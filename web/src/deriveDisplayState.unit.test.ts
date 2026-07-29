// Example-based UNIT tests for the pure interpolation module
// web/src/deriveDisplayState.ts (Task 5 of web-stale-derived-state bugfix).
//
// These are deterministic, hand-picked examples that pin down the exact rules
// the module mirrors from the world authority (see design.md Fix
// Implementation): 修为 grows at 1/60 点/秒, 本世年龄 advances 1:1, realm-derived
// fields come from the rule table at the threshold boundary, directional motion
// is per-axis (up:+Y, down:-Y, left:-X, right:+X), and target motion clamps at
// the destination without overshoot. The guard (elapsed <= 0 / not-alive)
// returns the SAME snapshot reference unchanged.
//
// The property-based generalization of these rules lives in the sibling
// deriveDisplayState.property.test.ts (Task 4); this file covers concrete
// examples and edge cases that anchor the behavior.
import { describe, expect, it } from 'vitest'
import type { GameRules, RoleState } from './api'
import { deriveDisplayState } from './deriveDisplayState'

// Rule table with three realms whose 修为 thresholds let us probe the boundary
// selection exactly at / just below / just above a threshold.
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
      { level: 3, name: '金丹', cultivation_threshold: 30, lifespan_seconds: 108000, speed: 30, sense_radius: 150 },
    ],
  }
}

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

describe('deriveDisplayState: 修为 advance rate is exactly 1/60 点/秒', () => {
  it('advances by +1.0 over 60s', () => {
    const result = deriveDisplayState(aliveState({ cultivation: 10 }), 60, gameRules())
    expect(result.cultivation).toBeCloseTo(11, 10)
  })

  it('advances by +1/60 over 1s', () => {
    const result = deriveDisplayState(aliveState({ cultivation: 10 }), 1, gameRules())
    expect(result.cultivation).toBeCloseTo(10 + 1 / 60, 10)
  })

  it('advances by +8/60 over 8s (the design counterexample)', () => {
    const result = deriveDisplayState(aliveState({ cultivation: 12 }), 8, gameRules())
    expect(result.cultivation).toBeCloseTo(12 + 8 / 60, 10)
  })
})

describe('deriveDisplayState: 本世年龄 advances 1:1 with elapsed world time', () => {
  it('adds elapsedSeconds to age_seconds and leaves lifespan authoritative', () => {
    const state = aliveState({ age_seconds: 100, lifespan_seconds: 36000 })
    const result = deriveDisplayState(state, 42, gameRules())
    expect(result.age_seconds).toBe(142)
    expect(result.lifespan_seconds).toBe(36000)
  })
})

describe('deriveDisplayState: realm-derived fields come from the rule table at the threshold', () => {
  // 筑基 threshold is 13. cultivation' = base + elapsed/60.
  it('selects 筑基 exactly at the threshold (advanced cultivation === 13)', () => {
    // 12 + 60/60 = 13 exactly.
    const result = deriveDisplayState(aliveState({ cultivation: 12 }), 60, gameRules())
    expect(result.cultivation).toBeCloseTo(13, 10)
    expect(result.realm_level).toBe(2)
    expect(result.realm).toBe('筑基')
    expect(result.speed).toBe(20)
    expect(result.sense_radius).toBe(100)
  })

  it('stays 炼气 just below the threshold (advanced cultivation < 13)', () => {
    // 12 + 59/60 = 12.983... < 13.
    const result = deriveDisplayState(aliveState({ cultivation: 12 }), 59, gameRules())
    expect(result.cultivation).toBeLessThan(13)
    expect(result.realm_level).toBe(1)
    expect(result.realm).toBe('炼气')
    expect(result.speed).toBe(10)
    expect(result.sense_radius).toBe(50)
  })

  it('advances to 筑基 just above the threshold (advanced cultivation > 13)', () => {
    // 12 + 61/60 = 13.016... > 13.
    const result = deriveDisplayState(aliveState({ cultivation: 12 }), 61, gameRules())
    expect(result.cultivation).toBeGreaterThan(13)
    expect(result.realm_level).toBe(2)
    expect(result.realm).toBe('筑基')
    expect(result.speed).toBe(20)
    expect(result.sense_radius).toBe(100)
  })
})

describe('deriveDisplayState: directional 实时位置 advances per axis', () => {
  const base = aliveState({
    movement_state: 'moving',
    movement_mode: 'direction',
    movement_speed_setting: 5,
    actual_movement_speed: 5,
    position: { x: 100, y: 200 },
  })
  // speed 5 * elapsed 8 = 40 units of travel.

  it('up moves +Y', () => {
    const result = deriveDisplayState({ ...base, movement_direction: 'up' }, 8, gameRules())
    expect(result.position).toEqual({ x: 100, y: 240 })
  })

  it('down moves -Y', () => {
    const result = deriveDisplayState({ ...base, movement_direction: 'down' }, 8, gameRules())
    expect(result.position).toEqual({ x: 100, y: 160 })
  })

  it('left moves -X', () => {
    const result = deriveDisplayState({ ...base, movement_direction: 'left' }, 8, gameRules())
    expect(result.position).toEqual({ x: 60, y: 200 })
  })

  it('right moves +X', () => {
    const result = deriveDisplayState({ ...base, movement_direction: 'right' }, 8, gameRules())
    expect(result.position).toEqual({ x: 140, y: 200 })
  })
})

describe('deriveDisplayState: target 实时位置 with clamping', () => {
  const base = aliveState({
    movement_state: 'moving',
    movement_mode: 'target',
    movement_speed_setting: 10,
    actual_movement_speed: 10,
    position: { x: 0, y: 0 },
  })
  const target = { x: 100, y: 0 }

  it('makes partial progress toward the target when travel < remaining', () => {
    // distance 10*5 = 50, remaining 100 => halfway.
    const result = deriveDisplayState(base, 5, gameRules(), target)
    expect(result.position).toEqual({ x: 50, y: 0 })
  })

  it('clamps exactly at the target when travel > remaining (no overshoot)', () => {
    // distance 10*20 = 200 >= remaining 100 => clamp at target, not (200, 0).
    const result = deriveDisplayState(base, 20, gameRules(), target)
    expect(result.position).toEqual({ x: 100, y: 0 })
  })

  it('clamps exactly at the target when travel === remaining', () => {
    // distance 10*10 = 100 === remaining 100 => exactly the target.
    const result = deriveDisplayState(base, 10, gameRules(), target)
    expect(result.position).toEqual({ x: 100, y: 0 })
  })

  it('holds position when target movement has no known destination', () => {
    const result = deriveDisplayState(base, 20, gameRules())
    expect(result.position).toEqual({ x: 0, y: 0 })
  })
})

describe('deriveDisplayState: guard cases return the SAME snapshot reference unchanged', () => {
  it('returns the same reference when elapsedSeconds === 0', () => {
    const state = aliveState()
    expect(deriveDisplayState(state, 0, gameRules())).toBe(state)
  })

  it('returns the same reference when elapsedSeconds < 0', () => {
    const state = aliveState()
    expect(deriveDisplayState(state, -5, gameRules())).toBe(state)
  })

  it('returns the same reference for a non-alive role even with elapsed > 0', () => {
    const state = aliveState({ status: 'pending_reincarnation' })
    expect(deriveDisplayState(state, 10, gameRules())).toBe(state)
  })
})
