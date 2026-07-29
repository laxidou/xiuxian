// Shared test helpers for controlling the passage of time.
//
// The stale-derived-state fix relies on `performance.now()` as the local
// interpolation baseline and on rAF / setInterval ticks. These helpers give
// tests deterministic control over both so they can advance "world time"
// without wall-clock flakiness.
import { vi } from 'vitest'

/**
 * Install Vitest fake timers with an explicit fake `performance` clock so that
 * `performance.now()`, `Date`, `setTimeout`/`setInterval`, and
 * `requestAnimationFrame` all advance together under `vi.advanceTimersByTime`.
 *
 * Call in a test (or beforeEach); teardown is handled globally in setupTests.ts
 * via `vi.useRealTimers()`.
 */
export function useControlledClock(nowMs = 0): void {
  vi.useFakeTimers({
    now: nowMs,
    toFake: [
      'setTimeout',
      'clearTimeout',
      'setInterval',
      'clearInterval',
      'requestAnimationFrame',
      'cancelAnimationFrame',
      'Date',
      'performance',
    ],
  })
}

/**
 * Advance the controlled clock (and fire due timers / rAF callbacks) by the
 * given number of seconds. Mirrors how the tick loop measures elapsed time.
 */
export function advanceSeconds(seconds: number): void {
  vi.advanceTimersByTime(Math.round(seconds * 1000))
}

/**
 * Lightweight alternative when a test only needs to control `performance.now()`
 * (no timers). Returns a setter to move the mocked clock; restore via the
 * global `vi.restoreAllMocks()` in setupTests.ts.
 */
export function mockPerformanceNow(initialMs = 0): (ms: number) => void {
  let current = initialMs
  vi.spyOn(performance, 'now').mockImplementation(() => current)
  return (ms: number) => {
    current = ms
  }
}
