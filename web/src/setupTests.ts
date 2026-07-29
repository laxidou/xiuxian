// Global test setup for Vitest (jsdom environment).
//
// - Registers @testing-library/jest-dom matchers (toBeInTheDocument, etc.)
// - Cleans up the React Testing Library DOM after every test
// - Resets timers/mocks between tests so fake-timer and performance.now()
//   control (see ./test-utils.ts) never leaks across test boundaries.
import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

afterEach(() => {
  cleanup()
  // Ensure any test that opted into fake timers / a controlled clock is torn
  // down, returning to real timers and a pristine mock state for the next test.
  vi.useRealTimers()
  vi.restoreAllMocks()
})
