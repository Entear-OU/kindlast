import '@testing-library/jest-dom/vitest'

/**
 * jsdom doesn't ship `ResizeObserver`, which `use-stick-to-bottom` (used by
 * `Conversation` in the AI Elements) requires at mount time. Stub it with a
 * no-op so component tests can render the chat without crashing on layout.
 */
if (typeof globalThis.ResizeObserver === 'undefined') {
  class ResizeObserverStub {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  globalThis.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver
}
