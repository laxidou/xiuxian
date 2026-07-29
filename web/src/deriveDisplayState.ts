// Pure, side-effect-free client-side interpolation ("tick") for the web
// frontend's continuous 派生状态 (derived state): 修为 (cultivation), 本世年龄
// (age), 实时位置 (position), and the 境界 / 移动速度上限 / 神识范围 that are pure
// functions of 修为.
//
// This module mirrors the world authority's derivation rules
// (internal/biz/core.go `cultivationLocked`/`stateLocked` and
// internal/rules/rules.go `RealmFor` / trajectory advancement) so the browser
// can advance the displayed snapshot between authoritative pushes. It is
// DISPLAY-ONLY: it never calls api.*, never touches timers/DOM, and returns a
// NEW RoleState while copying every authoritative field (ids, state_version,
// life_number, movement settings, rule_version) through unchanged so those
// remain authoritative for commands (Req 3.1).
//
// Design: .kiro/specs/web-stale-derived-state/design.md (Option (b): local
// baseline + inferred rates). See Fix Implementation for the exact rules.
import type { GameRules, Position, RoleState } from './api'

/**
 * Advance an authoritative `RoleState` snapshot by `elapsedSeconds` of world
 * time using the same rules as the world authority, recomputing realm-derived
 * values from the `gameRules` rule table.
 *
 * Preservation guard: if the role is not `alive` OR `elapsedSeconds <= 0`, the
 * snapshot is returned UNCHANGED (the same object reference), making the
 * not-alive and just-synced cases byte-for-byte identical to today
 * (Req 3.4, Correctness Property 2).
 *
 * @param state          the last authoritative snapshot (source of truth)
 * @param elapsedSeconds local elapsed seconds since the snapshot was received
 * @param gameRules      the rule table from `GetGameRules` (realm thresholds)
 * @param target         optional client-known destination for `target`
 *                       movement; supplied by App.tsx only when THIS client
 *                       issued the move. When absent, target-mode position is
 *                       held at the authoritative coordinate (Req 3.7 scope).
 * @returns a new advanced `RoleState`, or `state` unchanged when the guard hits
 */
export function deriveDisplayState(
  state: RoleState,
  elapsedSeconds: number,
  gameRules: GameRules,
  target?: Position,
): RoleState {
  // Guard (preservation): not-alive or no elapsed time => unchanged snapshot.
  if (state.status !== 'alive' || elapsedSeconds <= 0) {
    return state
  }

  // Advance 修为: 1 cultivation-unit per world-millisecond => 1/60 点 per second
  // (mirrors `cultivationLocked`: points = units / 60000).
  const cultivation = state.cultivation + elapsedSeconds / 60

  // Recompute realm-derived values from the rule table: highest realm whose
  // cultivation_threshold <= advanced cultivation (mirrors `rules.RealmFor`).
  const realmDerived = realmForCultivation(cultivation, gameRules, state)

  // Advance 本世年龄 1:1 with world time; lifespan stays authoritative.
  const ageSeconds = state.age_seconds + elapsedSeconds

  // Advance 实时位置 along the active 移动轨迹.
  const position = advancePosition(state, elapsedSeconds, target)

  return {
    ...state,
    cultivation,
    realm_level: realmDerived.realm_level,
    realm: realmDerived.realm,
    speed: realmDerived.speed,
    sense_radius: realmDerived.sense_radius,
    age_seconds: ageSeconds,
    position,
  }
}

type RealmDerived = Pick<RoleState, 'realm_level' | 'realm' | 'speed' | 'sense_radius'>

/**
 * Select the highest `gameRules.realms` entry whose `cultivation_threshold` is
 * `<= cultivation` (mirrors `rules.RealmFor`). Falls back to the current
 * authoritative realm fields when no realm qualifies (e.g. empty rule table),
 * so the display never regresses below authority.
 */
function realmForCultivation(cultivation: number, gameRules: GameRules, state: RoleState): RealmDerived {
  let best: GameRules['realms'][number] | undefined
  for (const realm of gameRules.realms) {
    if (realm.cultivation_threshold <= cultivation) {
      if (!best || realm.cultivation_threshold > best.cultivation_threshold) {
        best = realm
      }
    }
  }
  if (!best) {
    return {
      realm_level: state.realm_level,
      realm: state.realm,
      speed: state.speed,
      sense_radius: state.sense_radius,
    }
  }
  return {
    realm_level: best.level,
    realm: best.name,
    speed: best.speed,
    sense_radius: best.sense_radius,
  }
}

/**
 * Advance 实时位置 by `movement_mode`:
 * - `idle`: unchanged (Req 3.6).
 * - `direction`: add `actual_movement_speed * elapsedSeconds` along
 *   `movement_direction` (up:+Y, down:-Y, left:-X, right:+X — mirrors
 *   `PositionAfterDistance`).
 * - `target`: move from the authoritative position toward `target` by
 *   `actual_movement_speed * elapsedSeconds`, clamping at the destination (no
 *   overshoot, Req 3.7); hold position when no destination is known.
 */
function advancePosition(state: RoleState, elapsedSeconds: number, target?: Position): Position {
  const distance = state.actual_movement_speed * elapsedSeconds

  if (state.movement_mode === 'direction' && state.movement_direction) {
    const { x, y } = state.position
    switch (state.movement_direction) {
      case 'up':
        return { x, y: y + distance }
      case 'down':
        return { x, y: y - distance }
      case 'left':
        return { x: x - distance, y }
      case 'right':
        return { x: x + distance, y }
    }
  }

  if (state.movement_mode === 'target' && target) {
    const dx = target.x - state.position.x
    const dy = target.y - state.position.y
    const remaining = Math.hypot(dx, dy)
    // Clamp at the destination when travelled distance meets/exceeds remaining
    // (no overshoot); also guards the zero-remaining case.
    if (remaining === 0 || distance >= remaining) {
      return { x: target.x, y: target.y }
    }
    const ratio = distance / remaining
    return { x: state.position.x + dx * ratio, y: state.position.y + dy * ratio }
  }

  // idle, or target with no known destination => hold position.
  return state.position
}
