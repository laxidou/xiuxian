// Property-based tests (Task 4 of web-stale-derived-state bugfix) for the PURE
// module web/src/deriveDisplayState.ts.
//
// These tests exercise `deriveDisplayState` DIRECTLY (no React render). They
// generalize the two Correctness Properties over the input domain using
// fast-check:
//   Property 1 (Bug Condition / Expected Behavior): for alive snapshots and
//     elapsed time > 0, derived state advances to the rule-derived value —
//     修为 at 1/60 点/sec, 本世年龄 1:1, 实时位置 along the trajectory, and the
//     realm-derived fields recomputed from the rule table; a fresh push
//     re-bases the display to authority exactly.
//   Property 2 (Preservation): zero elapsed, not-alive, and idle-position
//     inputs are returned unchanged; target movement never overshoots.
//
// Each assertion compares `deriveDisplayState` against an INDEPENDENT reference
// derivation implemented locally in this file, with tolerance `epsilon` for
// floating comparisons.
import fc from 'fast-check'
import { describe, expect, it } from 'vitest'
import type { GameRules, Position, RoleState } from './api'
import { deriveDisplayState } from './deriveDisplayState'

// ---------------------------------------------------------------------------
// Tolerance helpers
// ---------------------------------------------------------------------------
const EPSILON = 1e-6

/** Scaled floating comparison: absolute tolerance that grows with magnitude. */
function approxEqual(actual: number, expected: number): boolean {
  return Math.abs(actual - expected) <= EPSILON * (1 + Math.abs(expected))
}

function distance(a: Position, b: Position): number {
  return Math.hypot(a.x - b.x, a.y - b.y)
}

// ---------------------------------------------------------------------------
// Independent reference derivation (mirrors the world-authority rules, written
// separately from the module under test so a defect in either surfaces).
// ---------------------------------------------------------------------------
type RealmFields = Pick<RoleState, 'realm_level' | 'realm' | 'speed' | 'sense_radius'>

function referenceRealm(cultivation: number, rules: GameRules, state: RoleState): RealmFields {
  let best: GameRules['realms'][number] | undefined
  for (const realm of rules.realms) {
    if (realm.cultivation_threshold <= cultivation) {
      if (best === undefined || realm.cultivation_threshold > best.cultivation_threshold) {
        best = realm
      }
    }
  }
  if (best === undefined) {
    return { realm_level: state.realm_level, realm: state.realm, speed: state.speed, sense_radius: state.sense_radius }
  }
  return { realm_level: best.level, realm: best.name, speed: best.speed, sense_radius: best.sense_radius }
}

function referencePosition(state: RoleState, elapsedSeconds: number, target?: Position): Position {
  const travelled = state.actual_movement_speed * elapsedSeconds

  if (state.movement_mode === 'direction' && state.movement_direction) {
    const { x, y } = state.position
    switch (state.movement_direction) {
      case 'up':
        return { x, y: y + travelled }
      case 'down':
        return { x, y: y - travelled }
      case 'left':
        return { x: x - travelled, y }
      case 'right':
        return { x: x + travelled, y }
    }
  }

  if (state.movement_mode === 'target' && target) {
    const dx = target.x - state.position.x
    const dy = target.y - state.position.y
    const remaining = Math.hypot(dx, dy)
    if (remaining === 0 || travelled >= remaining) {
      return { x: target.x, y: target.y }
    }
    const ratio = travelled / remaining
    return { x: state.position.x + dx * ratio, y: state.position.y + dy * ratio }
  }

  return state.position
}

// ---------------------------------------------------------------------------
// Arbitraries
// ---------------------------------------------------------------------------
// A rule table with ascending cultivation_threshold values and a first realm at
// threshold 0 (so a realm always matches any cultivation >= 0). Multiple realms
// let elapsed times cross thresholds.
function realmsArb(): fc.Arbitrary<GameRules['realms']> {
  return fc
    .integer({ min: 1, max: 5 })
    .chain((count) =>
      fc
        .array(fc.integer({ min: 1, max: 200 }), { minLength: Math.max(0, count - 1), maxLength: Math.max(0, count - 1) })
        .map((deltas) => {
          const realms: GameRules['realms'] = []
          let threshold = 0
          for (let i = 0; i < count; i++) {
            realms.push({
              level: i + 1,
              name: `境界${i + 1}`,
              cultivation_threshold: threshold,
              lifespan_seconds: 36000 * (i + 1),
              speed: 10 * (i + 1),
              sense_radius: 50 * (i + 1),
            })
            threshold += deltas[i] ?? 0
          }
          return realms
        }),
    )
}

function gameRulesArb(): fc.Arbitrary<GameRules> {
  return realmsArb().map((realms) => ({
    rule_version: 1,
    title: '游戏说明',
    summary: '',
    canonical_url: '/rules',
    ai_rules: '',
    sections: [],
    realms,
  }))
}

type SnapshotOpts = {
  status?: RoleState['status']
  movement?: 'idle' | 'direction' | 'target'
}

// A RoleState arbitrary. Numeric ranges are bounded to keep floating tolerances
// meaningful. `status`/`movement` can be pinned to target specific properties.
function snapshotArb(opts: SnapshotOpts = {}): fc.Arbitrary<RoleState> {
  const statusArb: fc.Arbitrary<RoleState['status']> = opts.status
    ? fc.constant(opts.status)
    : fc.constantFrom('alive', 'pending_reincarnation')
  const movementArb: fc.Arbitrary<'idle' | 'direction' | 'target'> = opts.movement
    ? fc.constant(opts.movement)
    : fc.constantFrom('idle', 'direction', 'target')

  return fc
    .record({
      status: statusArb,
      movement: movementArb,
      cultivation: fc.double({ min: 0, max: 10000, noNaN: true }),
      realm_level: fc.integer({ min: 1, max: 9 }),
      realm: fc.constantFrom('炼气', '筑基', '金丹', '元婴'),
      age_seconds: fc.double({ min: 0, max: 200000, noNaN: true }),
      lifespan_seconds: fc.double({ min: 0, max: 400000, noNaN: true }),
      speed: fc.integer({ min: 0, max: 100 }),
      sense_radius: fc.integer({ min: 0, max: 500 }),
      x: fc.double({ min: -1000, max: 1000, noNaN: true }),
      y: fc.double({ min: -1000, max: 1000, noNaN: true }),
      actualSpeed: fc.double({ min: 0, max: 50, noNaN: true }),
      direction: fc.constantFrom('up', 'down', 'left', 'right') as fc.Arbitrary<
        NonNullable<RoleState['movement_direction']>
      >,
    })
    .map((r): RoleState => ({
      id: 'role-1',
      name: '测试道友',
      life_number: 1,
      status: r.status,
      cultivation: r.cultivation,
      realm_level: r.realm_level,
      realm: r.realm,
      age_seconds: r.age_seconds,
      lifespan_seconds: r.lifespan_seconds,
      speed: r.speed,
      sense_radius: r.sense_radius,
      position: { x: r.x, y: r.y },
      movement_state: r.movement === 'idle' ? 'idle' : 'moving',
      movement_mode: r.movement,
      movement_direction: r.movement === 'direction' ? r.direction : undefined,
      movement_speed_setting: r.movement === 'direction' ? r.actualSpeed : 0,
      actual_movement_speed: r.movement === 'idle' ? 0 : r.actualSpeed,
      state_version: 1,
      rule_version: 1,
    }))
}

const positiveElapsedArb = fc.double({ min: 1e-3, max: 600, noNaN: true })
const anyElapsedArb = fc.double({ min: 0, max: 600, noNaN: true })
const targetArb: fc.Arbitrary<Position> = fc.record({
  x: fc.double({ min: -1000, max: 1000, noNaN: true }),
  y: fc.double({ min: -1000, max: 1000, noNaN: true }),
})

// ===========================================================================
// Property 1 — rate accuracy (Req 2.1, 2.2, 2.3)
// ===========================================================================
describe('P1 rate accuracy: deriveDisplayState matches an independent reference derivation', () => {
  // **Validates: Requirements 2.1, 2.2, 2.3**
  it('advances 修为 (1/60 点/sec), 本世年龄 (1:1), and 实时位置 along the trajectory within epsilon', () => {
    fc.assert(
      fc.property(
        snapshotArb({ status: 'alive' }),
        positiveElapsedArb,
        gameRulesArb(),
        targetArb,
        (state, elapsedSeconds, rules, target) => {
          const result = deriveDisplayState(state, elapsedSeconds, rules, target)

          // 修为: cultivation + elapsedSeconds / 60
          const expectedCultivation = state.cultivation + elapsedSeconds / 60
          expect(approxEqual(result.cultivation, expectedCultivation)).toBe(true)

          // 本世年龄: age_seconds + elapsedSeconds (lifespan stays authoritative)
          const expectedAge = state.age_seconds + elapsedSeconds
          expect(approxEqual(result.age_seconds, expectedAge)).toBe(true)
          expect(result.lifespan_seconds).toBe(state.lifespan_seconds)

          // 实时位置: along the active trajectory
          const expectedPosition = referencePosition(state, elapsedSeconds, target)
          expect(approxEqual(result.position.x, expectedPosition.x)).toBe(true)
          expect(approxEqual(result.position.y, expectedPosition.y)).toBe(true)
        },
      ),
      { numRuns: 300 },
    )
  })
})

// ===========================================================================
// Property 1 — realm threshold boundaries (Req 2.4)
// ===========================================================================
describe('P1 realm threshold boundaries: realm-derived fields equal the rule-table lookup', () => {
  // **Validates: Requirements 2.4**
  it('realm_level/realm/speed/sense_radius match ruleTable(advanced cultivation) across and around thresholds', () => {
    fc.assert(
      fc.property(snapshotArb({ status: 'alive' }), positiveElapsedArb, gameRulesArb(), (state, elapsedSeconds, rules) => {
        const result = deriveDisplayState(state, elapsedSeconds, rules)
        const advancedCultivation = state.cultivation + elapsedSeconds / 60
        const expected = referenceRealm(advancedCultivation, rules, state)
        expect(result.realm_level).toBe(expected.realm_level)
        expect(result.realm).toBe(expected.realm)
        expect(result.speed).toBe(expected.speed)
        expect(result.sense_radius).toBe(expected.sense_radius)
      }),
      { numRuns: 300 },
    )
  })

  it('crossing a threshold boundary produces the higher realm exactly at/after the threshold', () => {
    // Seed cultivation just below each threshold, then pick an elapsed time that
    // advances 修为 to straddle that threshold, and assert the module selects the
    // rule-table realm for the advanced cultivation.
    fc.assert(
      fc.property(
        gameRulesArb(),
        fc.double({ min: 0, max: 20, noNaN: true }),
        fc.integer({ min: 0, max: 4 }),
        (rules, extraCultivation, realmIndex) => {
          const idx = Math.min(realmIndex, rules.realms.length - 1)
          const threshold = rules.realms[idx].cultivation_threshold
          // Start below the threshold, elapse enough to cross it.
          const startCultivation = Math.max(0, threshold - 0.05)
          const elapsedSeconds = (threshold - startCultivation + extraCultivation) * 60 + 1e-3
          const base = snapshotSeed(startCultivation)
          const result = deriveDisplayState(base, elapsedSeconds, rules)
          const advanced = startCultivation + elapsedSeconds / 60
          const expected = referenceRealm(advanced, rules, base)
          expect(result.realm_level).toBe(expected.realm_level)
          expect(result.speed).toBe(expected.speed)
          expect(result.sense_radius).toBe(expected.sense_radius)
        },
      ),
      { numRuns: 300 },
    )
  })
})

function snapshotSeed(cultivation: number): RoleState {
  return {
    id: 'role-1',
    name: '测试道友',
    life_number: 1,
    status: 'alive',
    cultivation,
    realm_level: 1,
    realm: '炼气',
    age_seconds: 100,
    lifespan_seconds: 36000,
    speed: 0,
    sense_radius: 0,
    position: { x: 0, y: 0 },
    movement_state: 'idle',
    movement_mode: 'idle',
    movement_direction: undefined,
    movement_speed_setting: 0,
    actual_movement_speed: 0,
    state_version: 1,
    rule_version: 1,
  }
}

// ===========================================================================
// Property 1 — drift/rebase reconciliation (Req 2.5)
// ===========================================================================
describe('P1 drift/rebase reconciliation: a fresh push re-bases the display to authority exactly', () => {
  // **Validates: Requirements 2.5**
  it('applying deriveDisplayState to a new authoritative snapshot with elapsedSeconds=0 returns that snapshot exactly', () => {
    fc.assert(
      fc.property(
        snapshotArb(),
        positiveElapsedArb,
        gameRulesArb(),
        targetArb,
        snapshotArb(),
        (baseline, elapsedSeconds, rules, target, pushed) => {
          // Model baseline -> advance (drift accumulates) -> new authoritative push.
          deriveDisplayState(baseline, elapsedSeconds, rules, target)
          // On the push, the display re-bases: elapsedSeconds resets to 0.
          const postPush = deriveDisplayState(pushed, 0, rules, target)
          // The post-push display equals the new authoritative snapshot exactly
          // (same reference, hence byte-for-byte identical).
          expect(postPush).toBe(pushed)
          expect(postPush).toEqual(pushed)
        },
      ),
      { numRuns: 300 },
    )
  })
})

// ===========================================================================
// Property 2 — target clamp / no overshoot (Req 3.7)
// ===========================================================================
describe('P2 target clamp / no overshoot: displayed position never overshoots the destination', () => {
  // **Validates: Requirements 3.7**
  it('distance travelled toward target <= straight-line distance to target (clamps at target)', () => {
    fc.assert(
      fc.property(
        snapshotArb({ status: 'alive', movement: 'target' }),
        positiveElapsedArb,
        gameRulesArb(),
        targetArb,
        (state, elapsedSeconds, rules, target) => {
          const result = deriveDisplayState(state, elapsedSeconds, rules, target)
          const straightLine = distance(state.position, target)
          const travelled = distance(state.position, result.position)
          // No overshoot: travelled cannot exceed the straight-line distance.
          expect(travelled).toBeLessThanOrEqual(straightLine + EPSILON * (1 + straightLine))
          // The result never lands beyond the target: distance from result to
          // target is <= straight-line distance (i.e. movement is toward target).
          const remainingAfter = distance(result.position, target)
          expect(remainingAfter).toBeLessThanOrEqual(straightLine + EPSILON * (1 + straightLine))
        },
      ),
      { numRuns: 300 },
    )
  })

  it('clamps exactly at the target when travel distance meets or exceeds the remaining distance', () => {
    fc.assert(
      fc.property(
        snapshotArb({ status: 'alive', movement: 'target' }),
        gameRulesArb(),
        targetArb,
        (state, rules, target) => {
          const remaining = distance(state.position, target)
          // Choose an elapsed time large enough to guarantee overshoot when speed > 0.
          const speed = state.actual_movement_speed
          fc.pre(speed > 0)
          const elapsedSeconds = (remaining / speed) + 10
          const result = deriveDisplayState(state, elapsedSeconds, rules, target)
          expect(approxEqual(result.position.x, target.x)).toBe(true)
          expect(approxEqual(result.position.y, target.y)).toBe(true)
        },
      ),
      { numRuns: 300 },
    )
  })
})

// ===========================================================================
// Property 2 — zero-elapsed preservation (Req 3.4 boundary)
// ===========================================================================
describe('P2 zero-elapsed preservation: deriveDisplayState(S, 0, rules, target) === S', () => {
  // **Validates: Requirements 3.4**
  it('returns the same snapshot reference for any input when elapsedSeconds = 0', () => {
    fc.assert(
      fc.property(snapshotArb(), gameRulesArb(), targetArb, (state, rules, target) => {
        const result = deriveDisplayState(state, 0, rules, target)
        expect(result).toBe(state)
      }),
      { numRuns: 300 },
    )
  })
})

// ===========================================================================
// Property 2 — not-alive preservation (Req 3.4)
// ===========================================================================
describe('P2 not-alive preservation: non-alive snapshots are returned unchanged', () => {
  // **Validates: Requirements 3.4**
  it('returns the same snapshot reference for pending_reincarnation across any elapsed time', () => {
    fc.assert(
      fc.property(
        snapshotArb({ status: 'pending_reincarnation' }),
        anyElapsedArb,
        gameRulesArb(),
        targetArb,
        (state, elapsedSeconds, rules, target) => {
          const result = deriveDisplayState(state, elapsedSeconds, rules, target)
          expect(result).toBe(state)
        },
      ),
      { numRuns: 300 },
    )
  })
})

// ===========================================================================
// Property 2 — idle-position preservation (Req 3.6)
// ===========================================================================
describe('P2 idle-position preservation: idle movement never drifts in position', () => {
  // **Validates: Requirements 3.6**
  it('position is unchanged for movement_mode = idle across any elapsed time', () => {
    fc.assert(
      fc.property(
        snapshotArb({ status: 'alive', movement: 'idle' }),
        positiveElapsedArb,
        gameRulesArb(),
        targetArb,
        (state, elapsedSeconds, rules, target) => {
          const result = deriveDisplayState(state, elapsedSeconds, rules, target)
          // Position value is preserved exactly (idle holds the coordinate).
          expect(result.position).toEqual(state.position)
          expect(result.position.x).toBe(state.position.x)
          expect(result.position.y).toBe(state.position.y)
        },
      ),
      { numRuns: 300 },
    )
  })
})
