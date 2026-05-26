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

// jsdom doesn't implement `Element.prototype.scrollTo`, which `OnboardingChat`
// calls in a passive effect to keep the latest turn in view. Stub it as a no-op
// so component tests don't crash on first commit.
if (typeof Element !== 'undefined' && typeof Element.prototype.scrollTo !== 'function') {
  Element.prototype.scrollTo = function scrollToStub() {}
}
