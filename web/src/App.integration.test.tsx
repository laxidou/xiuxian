// Integration tests for the client-side interpolation ("tick") fix
// (Task 5 of web-stale-derived-state bugfix). These mount the REAL App with a
// mocked ./api and drive a controlled clock to exercise the wired behavior:
//
//   - Tick lifecycle: continuous readouts advance ONLY when status === 'alive'
//     AND the page is visible; frozen while hidden and for non-alive roles;
//     resume from the current baseline on re-visibility.
//   - Full render flow / reconciliation (Req 2.5): advancing without an SSE push
//     advances 修为/年龄/位置/境界 on screen; an authoritative push snaps the
//     display EXACTLY to the new authoritative values (drift discarded).
//   - Context transitions (Req 3.4, 3.5): a push that flips status away from
//     'alive' stops advancement and holds values constant.
//   - Command safety (Req 3.1): a move issued during interpolation calls the api
//     command with AUTHORITATIVE inputs (never interpolated values); the
//     returned snapshot re-bases the display.
//   - Non-continuous data (Req 3.8): a tick-only window never re-fetches
//     events/conversations/bounds.
//
// The mock/harness mirrors deriveDisplayState.preservation.test.tsx: a hoisted
// api mock, a mutable holder for the seeded snapshot/rules, a MockEventSource,
// the useControlledClock helper, and the statValue reader.
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { GameRules, RoleState } from './api'
import { useControlledClock } from './test-utils'

// ---------------------------------------------------------------------------
// Mocked `./api` — App renders a controlled snapshot with no real network.
// ---------------------------------------------------------------------------
const emptyScan = {
  roles: [],
  opportunities: [],
  has_more: false,
  truncated_roles: 0,
  truncated_opportunities: 0,
}

const h = vi.hoisted(() => ({
  state: null as RoleState | null,
  rules: null as GameRules | null,
}))

const apiMock = vi.hoisted(() => ({
  health: vi.fn(async () => ({ status: 'ok', service: 'world', version: 'test' })),
  state: vi.fn(),
  gameRules: vi.fn(),
  events: vi.fn(async () => ({ events: [] as unknown[] })),
  conversations: vi.fn(async () => ({ conversations: [] as unknown[] })),
  bounds: vi.fn(async () => ({ min_x: 0, max_x: 1000, min_y: 0, max_y: 1000 })),
  scan: vi.fn(),
  logout: vi.fn(async () => {}),
  move: vi.fn(),
  moveDirection: vi.fn(),
  stop: vi.fn(),
  transfer: vi.fn(),
  seize: vi.fn(),
  requestConversation: vi.fn(async () => ({})),
  respondConversation: vi.fn(async () => ({})),
  sendMessage: vi.fn(async () => ({})),
  closeConversation: vi.fn(async () => ({})),
  rotateMCPKey: vi.fn(async () => ({ api_key: '' })),
  revokeMCPKey: vi.fn(async () => true),
}))

vi.mock('./api', () => ({ api: apiMock }))

// Import App AFTER the mock so it picks up the mocked api.
import App from './App'

// ---------------------------------------------------------------------------
// EventSource stand-in (jsdom has none). Records the registered event-type
// listeners so tests can deliver an authoritative push on demand.
// ---------------------------------------------------------------------------
class MockEventSource {
  static instances: MockEventSource[] = []
  url: string
  onmessage: ((event: MessageEvent) => void) | null = null
  listeners = new Map<string, Set<(event: MessageEvent) => void>>()
  closed = false
  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
  }
  addEventListener(type: string, listener: (event: MessageEvent) => void) {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set())
    this.listeners.get(type)!.add(listener)
  }
  removeEventListener(type: string, listener: (event: MessageEvent) => void) {
    this.listeners.get(type)?.delete(listener)
  }
  close() {
    this.closed = true
  }
  emit(type: string) {
    const event = new MessageEvent(type)
    if (type === 'message') this.onmessage?.(event)
    for (const listener of this.listeners.get(type) ?? []) listener(event)
  }
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
function statValue(label: string): string {
  const dt = screen.getByText(label)
  const dd = dt.nextElementSibling
  return (dd?.textContent ?? '').trim()
}

type Continuous = {
  cultivation: string
  realm: string
  age: string
  position: string
  speed: string
  sense: string
}

function readContinuous(): Continuous {
  return {
    cultivation: statValue('修为'),
    realm: statValue('境界'),
    age: statValue('年龄 / 寿元'),
    position: statValue('位置'),
    speed: statValue('速度上限'),
    sense: statValue('神识'),
  }
}

// Flush promise-based effects (initial refresh / gameRules) without advancing
// the clock.
async function flush() {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve()
  })
}

// Advance the controlled clock (fires timers + interval ticks) and flush.
async function advance(seconds: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(Math.round(seconds * 1000))
  })
}

function setVisibility(state: 'visible' | 'hidden') {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => state })
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'))
  })
}

async function mountApp() {
  render(<App />)
  await flush()
}

// ---------------------------------------------------------------------------
beforeEach(() => {
  MockEventSource.instances = []
  ;(globalThis as unknown as { EventSource: typeof MockEventSource }).EventSource = MockEventSource
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'visible' })
  h.rules = gameRules()
  apiMock.state.mockImplementation(async () => h.state)
  apiMock.gameRules.mockImplementation(async () => h.rules)
  apiMock.scan.mockImplementation(async () => emptyScan)
  apiMock.events.mockImplementation(async () => ({ events: [] }))
  apiMock.conversations.mockImplementation(async () => ({ conversations: [] }))
  apiMock.bounds.mockImplementation(async () => ({ min_x: 0, max_x: 1000, min_y: 0, max_y: 1000 }))
  useControlledClock(0)
})

afterEach(() => {
  h.state = null
  h.rules = null
  vi.useRealTimers()
})

// ---------------------------------------------------------------------------
describe('full render flow: derived state advances between pushes, then snaps to authority (Req 2.5)', () => {
  it('advances 修为/年龄/位置/境界 on screen, then reconciles exactly on an authoritative push', async () => {
    // Alive, moving right at 5/秒, 修为 seeded just below the 筑基 threshold (13).
    h.state = aliveState({
      cultivation: 12.5,
      age_seconds: 3600,
      movement_state: 'moving',
      movement_mode: 'direction',
      movement_direction: 'right',
      movement_speed_setting: 5,
      actual_movement_speed: 5,
      position: { x: 100, y: 200 },
    })
    await mountApp()

    // At elapsed 0 the display equals the authoritative snapshot.
    expect(statValue('修为')).toBe('12.500')
    expect(statValue('境界')).toBe('1 · 炼气')
    expect(statValue('位置')).toBe('(100, 200)')

    // Advance 60s WITHOUT an SSE push.
    await advance(60)

    // 修为: 12.5 + 60/60 = 13.5 => crosses the 筑基 threshold.
    expect(Number(statValue('修为'))).toBeCloseTo(13.5, 3)
    expect(statValue('境界')).toBe('2 · 筑基')
    expect(statValue('速度上限')).toBe('20 / 秒')
    expect(statValue('神识')).toBe('100')
    // 年龄: 3600 + 60 = 3660s => 1时 1分.
    expect(statValue('年龄 / 寿元')).toContain('1时 1分')
    // 位置: right 5/秒 * 60s => x + 300 => (400, 200).
    expect(statValue('位置')).toBe('(400, 200)')

    // Deliver an authoritative push (SSE event -> refresh() -> api.state()).
    h.state = aliveState({
      cultivation: 20,
      realm_level: 2,
      realm: '筑基',
      speed: 20,
      sense_radius: 100,
      age_seconds: 7200,
      position: { x: 555, y: 666 },
      state_version: 2,
    })
    const source = MockEventSource.instances.at(-1)!
    await act(async () => {
      source.emit('movement_arrived')
      for (let i = 0; i < 10; i++) await Promise.resolve()
    })

    // Display snaps EXACTLY to the new authoritative values; drift discarded.
    expect(statValue('修为')).toBe('20.000')
    expect(statValue('境界')).toBe('2 · 筑基')
    expect(statValue('位置')).toBe('(555, 666)')
    expect(statValue('年龄 / 寿元')).toContain('2时 0分')
  })
})

describe('tick lifecycle: advance only when alive AND visible', () => {
  it('does NOT advance while the page is hidden, and resumes from the baseline on re-visibility', async () => {
    h.state = aliveState({
      movement_state: 'moving',
      movement_mode: 'direction',
      movement_direction: 'right',
      movement_speed_setting: 5,
      actual_movement_speed: 5,
      position: { x: 100, y: 200 },
    })
    await mountApp()

    // Visible: advancing 4s moves x by 5*4 = 20 => (120, 200).
    await advance(4)
    expect(statValue('位置')).toBe('(120, 200)')

    // Hide the page: the tick must halt. Advancing does not change the display.
    setVisibility('hidden')
    const frozen = readContinuous()
    await advance(4)
    expect(readContinuous()).toEqual(frozen)

    // Restore visibility: the tick resumes from the current baseline (the mount
    // snapshot), so elapsed time keeps accumulating and the display advances.
    setVisibility('visible')
    await advance(0.2)
    // Total elapsed ~8.2s from baseline => x = 100 + 5*8.2 = 141 (> the 120 held
    // while hidden), proving advancement resumed.
    expect(Number(statValue('位置').match(/-?\d+/)![0])).toBeGreaterThan(120)
  })

  it('does NOT advance for a non-alive role', async () => {
    h.state = aliveState({
      status: 'pending_reincarnation',
      movement_state: 'moving',
      movement_mode: 'direction',
      movement_direction: 'right',
      movement_speed_setting: 5,
      actual_movement_speed: 5,
    })
    await mountApp()

    const before = readContinuous()
    await advance(60)
    expect(readContinuous()).toEqual(before)
  })
})

describe('context transitions: a non-alive push stops advancement (Req 3.4, 3.5)', () => {
  it('holds 修为/年龄/位置 constant after a death/寿尽 push flips status away from alive', async () => {
    h.state = aliveState({
      cultivation: 12,
      movement_state: 'moving',
      movement_mode: 'direction',
      movement_direction: 'right',
      movement_speed_setting: 5,
      actual_movement_speed: 5,
      position: { x: 100, y: 200 },
    })
    await mountApp()

    await advance(8)
    expect(statValue('位置')).toBe('(140, 200)')

    // A death push arrives: status flips to pending_reincarnation and the
    // authoritative snapshot is settled.
    h.state = aliveState({
      status: 'pending_reincarnation',
      cultivation: 15,
      age_seconds: 4000,
      position: { x: 140, y: 200 },
      movement_state: 'idle',
      movement_mode: 'idle',
      movement_direction: undefined,
      actual_movement_speed: 0,
      state_version: 2,
    })
    const source = MockEventSource.instances.at(-1)!
    await act(async () => {
      source.emit('death')
      for (let i = 0; i < 10; i++) await Promise.resolve()
    })

    // Reconciled to authority.
    expect(statValue('修为')).toBe('15.000')
    const held = readContinuous()

    // Further clock advancement does NOT change anything (interpolation stops
    // for a non-alive role).
    await advance(60)
    expect(readContinuous()).toEqual(held)
  })
})

describe('command safety: a move during interpolation uses authoritative inputs and re-bases (Req 3.1)', () => {
  it('calls api.move with the authoritative position (never the interpolated one) and snaps to the returned snapshot', async () => {
    h.state = aliveState({
      cultivation: 12,
      movement_state: 'moving',
      movement_mode: 'direction',
      movement_direction: 'right',
      movement_speed_setting: 5,
      actual_movement_speed: 5,
      position: { x: 100, y: 200 },
    })
    await mountApp()

    // Interpolation drifts the DISPLAYED position away from the authoritative
    // (100, 200).
    await advance(8)
    expect(statValue('位置')).toBe('(140, 200)')

    // The move command returns a fresh authoritative snapshot and updates the
    // holder so the subsequent refresh() reports it too.
    const newAuth = aliveState({
      cultivation: 30,
      age_seconds: 5000,
      position: { x: 700, y: 800 },
      movement_state: 'idle',
      movement_mode: 'idle',
      movement_direction: undefined,
      actual_movement_speed: 0,
      state_version: 2,
    })
    apiMock.move.mockImplementation(async () => {
      h.state = newAuth
      return newAuth
    })

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: '移动' }))
      for (let i = 0; i < 10; i++) await Promise.resolve()
    })

    // The command was issued with the AUTHORITATIVE position (100, 200) taken
    // from the form's authoritative state, NOT the interpolated (140, 200).
    expect(apiMock.move).toHaveBeenCalledWith(100, 200)

    // The returned authoritative snapshot re-bases the display.
    expect(statValue('位置')).toBe('(700, 800)')
    expect(statValue('修为')).toBe('30.000')
  })
})

describe('non-continuous data: a tick-only window never re-fetches events/conversations/bounds (Req 3.8)', () => {
  it('does not increase events/conversations/bounds api call counts while only the clock advances', async () => {
    h.state = aliveState()
    await mountApp()

    const eventsBefore = apiMock.events.mock.calls.length
    const conversationsBefore = apiMock.conversations.mock.calls.length
    const boundsBefore = apiMock.bounds.mock.calls.length

    await advance(60)

    expect(apiMock.events.mock.calls.length).toBe(eventsBefore)
    expect(apiMock.conversations.mock.calls.length).toBe(conversationsBefore)
    expect(apiMock.bounds.mock.calls.length).toBe(boundsBefore)
  })
})
