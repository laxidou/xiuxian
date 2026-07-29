import { describe, expect, it } from 'vitest'

// Trivial smoke test to verify the Vitest + jsdom runner is wired up.
// Real tests for deriveDisplayState / App live in dedicated task files.
describe('test runner smoke test', () => {
  it('runs a trivial assertion', () => {
    expect(true).toBe(true)
  })

  it('has a jsdom document available', () => {
    expect(typeof document).toBe('object')
    expect(document.createElement('div')).toBeTruthy()
  })
})
