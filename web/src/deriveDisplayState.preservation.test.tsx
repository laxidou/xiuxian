// Preservation property tests (Task 2 of web-stale-derived-state bugfix).
//
// GOAL: lock in the behavior that MUST NOT change when the client-side
// interpolation ("tick") fix is added. These are the non-bug-condition cases:
//   isBugCondition(X) = X.S.status === 'alive' AND local elapsed time > 0
// so NOT a bug condition means EITHER local elapsed time is 0 OR the role is
// not alive — plus the always-preserved paths (idle position, the SSE
// transport, and non-continuous data).
//
// Observation-first methodology: every assertion here is observed to PASS on
// the CURRENT (unfixed) code (which never interpolates), and must continue to
// pass after the fix. The fix is display-only, gated to alive + visible, so:
//   - elapsed 0 still equals the authoritative snapshot exactly
//   - not-alive roles never advance 修为 / 年龄 / 位置
//   - idle roles never move
//   - SSE keeps arriving via EventSource('/events/stream') and triggers refresh
//   - non-continuous data (events/conversations/scan/bounds) only updates on
//     refresh, never on a tick
//
// The pure module web/src/deriveDisplayState.ts does not exist yet (task 3.1),
// so the render-path / SSE / non-continuous preservation properties are
// verified against the CURRENT App.tsx directly.
//
// Time is controlled with the Task 0 helpers so performance.now(), timers and
// requestAnimationFrame advance together.
import { act, cleanup, render, screen } from '@testing-library/react'
import fc from 'fast-check'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { GameRules, RoleState } from './api'
import { useControlledClock } from './test-utils'

// ---------------------------------------------------------------------------
// Mocked `./api` — App renders a controlled snapshot with no real network. A
// mutable holder lets each test/property seed the snapshot + rule table BEFORE
// rendering, and lets us count calls to the non-continuous data loaders.
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
  reincarnate: vi.fn(),
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
// EventSource stand-in (jsdom has none). Records the URL and every registered
// event-type listener so we can assert the SSE transport is preserved and can
// deliver events on demand.
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

// The exact SSE event-type set App subscribes to today. Preservation (Req 3.2)
// requires this set to remain unchanged.
const EXPECTED_SSE_EVENT_TYPES = [
  'scanned', 'movement_arrived', 'conversation_requested', 'conversation_incoming',
  'conversation_responded', 'conversation_message', 'conversation_closed', 'transfer',
  'transfer_received', 'seizure', 'opportunity_claimed', 'opportunity_converting',
  'opportunity_converted', 'death', 'reincarnation',
]

// ---------------------------------------------------------------------------
// Rendering helpers (mirror the display formatting in App.tsx exactly so the
// "equals the authoritative snapshot" assertions are precise).
// ---------------------------------------------------------------------------
const formatDuration = (seconds: number) => {
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return `${hours}时 ${minutes}分`
}

type DisplayedDerived = {
  cultivation: string
  realm: string
  age: string
  position: string
  speed: string
  sense: string
}

function displayedFromSnapshot(s: RoleState): DisplayedDerived {
  return {
    cultivation: s.cultivation.toFixed(3),
    realm: `${s.realm_level} · ${s.realm}`,
    age: `${formatDuration(s.age_seconds)} / ${formatDuration(s.lifespan_seconds)}`,
    position: `(${s.position.x}, ${s.position.y})`,
    speed: `${s.speed} / 秒`,
    sense: `${s.sense_radius}`,
  }
}

function statValue(label: string): string {
  const dt = screen.getByText(label)
  const dd = dt.nextElementSibling
  return (dd?.textContent ?? '').trim()
}

function readDisplayedDerived(): DisplayedDerived {
  return {
    cultivation: statValue('修为'),
    realm: statValue('境界'),
    age: statValue('年龄 / 寿元'),
    position: statValue('位置'),
    speed: statValue('速度上限'),
    sense: statValue('神识'),
  }
}

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

async function mountApp() {
  render(<App />)
  await flush()
}

// ---------------------------------------------------------------------------
// Fixtures / arbitraries
// ---------------------------------------------------------------------------
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

const realmArb = fc.constantFrom(
  { realm_level: 1, realm: '炼气' },
  { realm_level: 2, realm: '筑基' },
  { realm_level: 3, realm: '金丹' },
)

// A snapshot arbitrary parameterized by status + movement so we can target the
// specific preservation cases. Numeric fields use integers/bounded values so
// the expected display formatting is deterministic.
function snapshotArb(opts: {
  status: RoleState['status']
  movement?: 'idle' | 'direction' | 'target'
}): fc.Arbitrary<RoleState> {
  const movement = opts.movement ?? 'idle'
  return fc.record({
    cultivation: fc.integer({ min: 0, max: 500000 }).map((n) => n / 1000),
    age_seconds: fc.integer({ min: 0, max: 200000 }),
    lifespan_seconds: fc.integer({ min: 0, max: 400000 }),
    speed: fc.integer({ min: 0, max: 100 }),
    sense_radius: fc.integer({ min: 0, max: 500 }),
    x: fc.integer({ min: -1000, max: 1000 }),
    y: fc.integer({ min: -1000, max: 1000 }),
    realm: realmArb,
    actualSpeed: fc.integer({ min: 0, max: 50 }),
    direction: fc.constantFrom('up', 'down', 'left', 'right') as fc.Arbitrary<RoleState['movement_direction']>,
  }).map((r): RoleState => ({
    id: 'role-1',
    name: '测试道友',
    life_number: 1,
    status: opts.status,
    cultivation: r.cultivation,
    realm_level: r.realm.realm_level,
    realm: r.realm.realm,
    age_seconds: r.age_seconds,
    lifespan_seconds: r.lifespan_seconds,
    speed: r.speed,
    sense_radius: r.sense_radius,
    position: { x: r.x, y: r.y },
    movement_state: movement === 'idle' ? 'idle' : 'moving',
    movement_mode: movement,
    movement_direction: movement === 'direction' ? r.direction : undefined,
    movement_speed_setting: movement === 'direction' ? r.actualSpeed : 0,
    actual_movement_speed: movement === 'idle' ? 0 : r.actualSpeed,
    state_version: 1,
    rule_version: 1,
  }))
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
})

afterEach(() => {
  h.state = null
  h.rules = null
})

describe('preservation: just-synced (elapsed 0) renders the authoritative snapshot exactly', () => {
  // Req 2.5 boundary / Property 2: with no elapsed time the display equals the
  // authoritative snapshot. Holds today (no interpolation) and after the fix
  // (the tick guard returns the snapshot unchanged at elapsedSeconds <= 0).
  it('displayed 修为/境界/年龄/位置/速度上限/神识 equal the snapshot for random alive states', async () => {
    await fc.assert(
      fc.asyncProperty(snapshotArb({ status: 'alive', movement: 'idle' }), async (snapshot) => {
        useControlledClock(0)
        h.state = snapshot
        try {
          await mountApp()
          expect(readDisplayedDerived()).toEqual(displayedFromSnapshot(snapshot))
        } finally {
          cleanup()
          vi.useRealTimers()
        }
      }),
      { numRuns: 20 },
    )
  })
})

describe('preservation: not-alive roles never advance derived state (Req 3.4)', () => {
  // For a non-alive snapshot, 修为 / 本世年龄 / 实时位置 stay constant across ANY
  // elapsed time. The fix gates the tick on status === 'alive', so this is
  // preserved.
  it('修为 / 年龄 / 位置 are held constant for pending_reincarnation across any elapsed time', async () => {
    await fc.assert(
      fc.asyncProperty(
        snapshotArb({ status: 'pending_reincarnation', movement: 'direction' }),
        fc.integer({ min: 1, max: 600 }),
        async (snapshot, elapsedSeconds) => {
          useControlledClock(0)
          h.state = snapshot
          try {
            await mountApp()
            const before = readDisplayedDerived()
            await advance(elapsedSeconds)
            const after = readDisplayedDerived()
            expect(after.cultivation).toBe(before.cultivation)
            expect(after.age).toBe(before.age)
            expect(after.position).toBe(before.position)
            // And equal to the authoritative snapshot.
            const snap = displayedFromSnapshot(snapshot)
            expect(after.cultivation).toBe(snap.cultivation)
            expect(after.age).toBe(snap.age)
            expect(after.position).toBe(snap.position)
          } finally {
            cleanup()
            vi.useRealTimers()
          }
        },
      ),
      { numRuns: 20 },
    )
  })
})

describe('preservation: idle roles never drift in position (Req 3.6)', () => {
  // For movement_mode = 'idle', 实时位置 is constant across any elapsed time,
  // even for an alive role. The fix advances 修为/年龄 for alive roles but keeps
  // idle position constant, so this preservation continues to hold.
  it('位置 is held constant for an idle alive role across any elapsed time', async () => {
    await fc.assert(
      fc.asyncProperty(
        snapshotArb({ status: 'alive', movement: 'idle' }),
        fc.integer({ min: 1, max: 600 }),
        async (snapshot, elapsedSeconds) => {
          useControlledClock(0)
          h.state = snapshot
          try {
            await mountApp()
            const before = statValue('位置')
            await advance(elapsedSeconds)
            const after = statValue('位置')
            expect(after).toBe(before)
            expect(after).toBe(`(${snapshot.position.x}, ${snapshot.position.y})`)
          } finally {
            cleanup()
            vi.useRealTimers()
          }
        },
      ),
      { numRuns: 20 },
    )
  })
})

describe('preservation: SSE transport is unchanged (Req 3.2)', () => {
  beforeEach(() => useControlledClock(0))
  afterEach(() => vi.useRealTimers())

  it('subscribes via EventSource(/events/stream) with the existing event-type set', async () => {
    h.state = {
      id: 'role-1', name: '测试道友', life_number: 1, status: 'alive',
      cultivation: 12, realm_level: 1, realm: '炼气', age_seconds: 3600, lifespan_seconds: 36000,
      speed: 10, sense_radius: 50, position: { x: 100, y: 200 },
      movement_state: 'idle', movement_mode: 'idle', movement_direction: undefined,
      movement_speed_setting: 0, actual_movement_speed: 0, state_version: 1, rule_version: 1,
    }
    await mountApp()

    expect(MockEventSource.instances.length).toBeGreaterThan(0)
    const source = MockEventSource.instances.at(-1)!
    expect(source.url.startsWith('/events/stream')).toBe(true)
    expect([...source.listeners.keys()].sort()).toEqual([...EXPECTED_SSE_EVENT_TYPES].sort())
  })

  it('each SSE event still triggers refresh() (api.state re-fetched)', async () => {
    h.state = {
      id: 'role-1', name: '测试道友', life_number: 1, status: 'alive',
      cultivation: 12, realm_level: 1, realm: '炼气', age_seconds: 3600, lifespan_seconds: 36000,
      speed: 10, sense_radius: 50, position: { x: 100, y: 200 },
      movement_state: 'idle', movement_mode: 'idle', movement_direction: undefined,
      movement_speed_setting: 0, actual_movement_speed: 0, state_version: 1, rule_version: 1,
    }
    await mountApp()
    const source = MockEventSource.instances.at(-1)!
    const before = apiMock.state.mock.calls.length

    await act(async () => {
      source.emit('movement_arrived')
      for (let i = 0; i < 10; i++) await Promise.resolve()
    })

    expect(apiMock.state.mock.calls.length).toBeGreaterThan(before)
  })
})

describe('preservation: non-continuous data updates only on refresh, never on a tick (Req 3.8)', () => {
  beforeEach(() => useControlledClock(0))
  afterEach(() => vi.useRealTimers())

  it('advancing time without an SSE push does not re-fetch events/conversations/bounds', async () => {
    h.state = {
      id: 'role-1', name: '测试道友', life_number: 1, status: 'alive',
      cultivation: 12, realm_level: 1, realm: '炼气', age_seconds: 3600, lifespan_seconds: 36000,
      speed: 10, sense_radius: 50, position: { x: 100, y: 200 },
      movement_state: 'idle', movement_mode: 'idle', movement_direction: undefined,
      movement_speed_setting: 0, actual_movement_speed: 0, state_version: 1, rule_version: 1,
    }
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
