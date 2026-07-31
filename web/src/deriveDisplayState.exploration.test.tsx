// Bug condition exploration test (Task 1 of web-stale-derived-state bugfix).
//
// GOAL: surface counterexamples proving that derived state FREEZES between
// authoritative pushes on the UNFIXED UI. The bug condition (design):
//   isBugCondition(X) = X.S.status === 'alive' AND local elapsed time > 0
//
// These assertions encode the EXPECTED (fixed) behavior — i.e. that 修为 /
// 本世年龄 / 实时位置 / 境界 ADVANCE as world time elapses without an SSE push.
// Therefore they MUST FAIL on the current unfixed code (the display is frozen),
// which is the SUCCESS outcome for an exploration test. The SAME test later
// verifies the fix (task 3.3) once it passes.
//
// Time is controlled with the Task 0 helpers (useControlledClock / advanceSeconds)
// so `performance.now()`, timers, and requestAnimationFrame advance together.
import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { GameRules, RoleState } from './api'
import { useControlledClock } from './test-utils'

// ---------------------------------------------------------------------------
// Mocked `./api` — the component renders a controlled alive snapshot with no
// real network. A mutable holder lets each test seed the snapshot / rule table
// BEFORE rendering.
// ---------------------------------------------------------------------------
const h = vi.hoisted(() => ({
  state: null as RoleState | null,
  rules: null as GameRules | null,
}))

vi.mock('./api', () => {
  const emptyScan = {
    roles: [],
    opportunities: [],
    has_more: false,
    truncated_roles: 0,
    truncated_opportunities: 0,
  }
  return {
    api: {
      health: vi.fn(async () => ({ status: 'ok', service: 'world', version: 'test' })),
      state: vi.fn(async () => h.state),
      gameRules: vi.fn(async () => h.rules),
      events: vi.fn(async () => ({ events: [] })),
      conversations: vi.fn(async () => ({ conversations: [] })),
      bounds: vi.fn(async () => ({ min_x: 0, max_x: 1000, min_y: 0, max_y: 1000 })),
      scan: vi.fn(async () => emptyScan),
      logout: vi.fn(async () => {}),
      move: vi.fn(async () => h.state),
      moveDirection: vi.fn(async () => h.state),
      stop: vi.fn(async () => h.state),
      transfer: vi.fn(async () => h.state),
      seize: vi.fn(async () => h.state),
      requestConversation: vi.fn(async () => ({})),
      respondConversation: vi.fn(async () => ({})),
      sendMessage: vi.fn(async () => ({})),
      closeConversation: vi.fn(async () => ({})),
      rotateMCPKey: vi.fn(async () => ({ api_key: '' })),
      revokeMCPKey: vi.fn(async () => true),
    },
  }
})

// Import AFTER the mock so App picks up the mocked api.
import App from './App'

// ---------------------------------------------------------------------------
// Minimal EventSource stand-in (jsdom has none). We intentionally NEVER deliver
// a message so no authoritative push occurs during a test window.
// ---------------------------------------------------------------------------
class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  onmessage: ((event: MessageEvent) => void) | null = null
  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }
  addEventListener() {}
  removeEventListener() {}
  close() {}
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------
function aliveState(overrides: Partial<RoleState> = {}): RoleState {
  return {
    id: 'role-1',
    name: '测试道友',
    life_number: 1,
    status: 'alive',
    cultivation: 12,
    realm_level: 1,
    realm: '炼气',
    age_seconds: 3600,
    lifespan_seconds: 36000,
    speed: 10,
    sense_radius: 50,
    position: { x: 100, y: 200 },
    movement_state: 'idle',
    movement_mode: 'idle',
    movement_direction: undefined,
    movement_speed_setting: 0,
    actual_movement_speed: 0,
    state_version: 1,
    rule_version: 1,
    ...overrides,
  }
}

function gameRules(): GameRules {
  return {
    rule_version: 1,
    title: '游戏说明',
    summary: '',
    canonical_url: '/rules',
    ai_rules: '',
    sections: [],
    realms: [
      { level: 1, name: '炼气', cultivation_threshold: 0, lifespan_seconds: 36000, speed: 10, sense_radius: 50 },
      { level: 2, name: '筑基', cultivation_threshold: 13, lifespan_seconds: 72000, speed: 20, sense_radius: 100 },
    ],
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
// Flush promise-based effects (initial refresh) without advancing the clock.
async function flush() {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve()
  })
}

// Advance the controlled clock (fires timers + rAF ticks) and flush microtasks.
async function advance(seconds: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(Math.round(seconds * 1000))
  })
}

// Read the rendered value (<dd>) for a status-grid label (<dt>).
function statValue(label: string): string {
  const dt = screen.getByText(label)
  const dd = dt.nextElementSibling
  return (dd?.textContent ?? '').trim()
}

async function mountApp() {
  render(<App />)
  await flush()
}

describe('bug condition exploration: derived state freezes between authoritative pushes', () => {
  beforeEach(() => {
    MockEventSource.instances = []
    ;(globalThis as unknown as { EventSource: typeof MockEventSource }).EventSource = MockEventSource
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
    h.rules = gameRules()
    useControlledClock(0)
  })

  afterEach(() => {
    h.state = null
    h.rules = null
  })

  // 1. 修为 freeze: advance 8s with no push; 修为 should have advanced ~0.133.
  it('修为 advances with world time (fails while frozen)', async () => {
    h.state = aliveState({ cultivation: 12 })
    await mountApp()

    const before = Number(statValue('修为'))
    expect(before).toBeCloseTo(12, 3)

    await advance(8)

    const after = Number(statValue('修为'))
    // Expected (fixed): 修为 grows at 1/60 点/秒 => +8/60 ≈ 0.133 over 8s.
    expect(after).toBeGreaterThan(before)
    expect(after).toBeCloseTo(12 + 8 / 60, 3)
  })

  // 2. 年龄 freeze: advance time; 本世年龄 should advance with world time.
  it('本世年龄 advances with world time (fails while frozen)', async () => {
    h.state = aliveState({ age_seconds: 3600 }) // "1时 0分"
    await mountApp()

    const before = statValue('年龄 / 寿元')
    expect(before).toContain('1时 0分')

    await advance(120) // +2 minutes of world time

    const after = statValue('年龄 / 寿元')
    // Expected (fixed): age advances 1:1 => "1时 2分".
    expect(after).not.toBe(before)
    expect(after).toContain('1时 2分')
  })

  // 3. 位置 freeze (directional): moving right at 5/秒 for 8s => x + 40.
  it('实时位置 advances along a directional trajectory (fails while frozen)', async () => {
    h.state = aliveState({
      movement_state: 'moving',
      movement_mode: 'direction',
      movement_direction: 'right',
      movement_speed_setting: 5,
      actual_movement_speed: 5,
      position: { x: 100, y: 200 },
    })
    await mountApp()

    const before = statValue('位置')
    expect(before).toBe('(100, 200)')

    await advance(8)

    const after = statValue('位置')
    // Expected (fixed): x advances by 5 * 8 = 40 => "(140, 200)".
    expect(after).not.toBe(before)
    expect(after).toBe('(140, 200)')
  })

  // 4. 境界 stale: 修为 seeded just below a threshold; advancing crosses it and
  //    should recompute 境界 / 移动速度上限 / 神识范围 from the rule table.
  it('境界 / 速度上限 / 神识 recompute when 修为 crosses a threshold (fails while stale)', async () => {
    // Threshold for 筑基 is 13 (see gameRules). Seed just below.
    h.state = aliveState({
      cultivation: 12.5,
      realm_level: 1,
      realm: '炼气',
      speed: 10,
      sense_radius: 50,
    })
    await mountApp()

    expect(statValue('境界')).toBe('1 · 炼气')
    expect(statValue('速度上限')).toBe('10 / 秒')
    expect(statValue('神识')).toBe('50')

    await advance(60) // 修为: 12.5 + 60/60 = 13.5 >= 13 => 筑基

    // Expected (fixed): realm and its derived caps advance to 筑基's row.
    expect(statValue('境界')).toBe('2 · 筑基')
    expect(statValue('速度上限')).toBe('20 / 秒')
    expect(statValue('神识')).toBe('100')
  })
})
