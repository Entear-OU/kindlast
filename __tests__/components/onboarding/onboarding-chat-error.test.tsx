import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi, beforeEach } from 'vitest'

/**
 * ENT-88 — Inline error + retry on chat failure.
 *
 * The component drives a mocked `useChat`: we flip `status` to `'error'` and
 * assert the banner renders, that Retry calls `regenerate`, and that the
 * user's typed text is preserved through the failure.
 */

// `useChat` is the only thing from `@ai-sdk/react` that matters here.
const { useChatMock } = vi.hoisted(() => ({ useChatMock: vi.fn() }))
vi.mock('@ai-sdk/react', () => ({
  useChat: useChatMock,
}))

// `DefaultChatTransport` is constructed at module top in the component;
// stub it so the import doesn't pull the full AI SDK transport into jsdom.
vi.mock('ai', async () => {
  const actual = await vi.importActual<typeof import('ai')>('ai')
  return {
    ...actual,
    DefaultChatTransport: vi.fn(),
  }
})

import { OnboardingChat } from '@/components/onboarding/onboarding-chat'

type UseChatReturn = {
  messages: Array<{
    id: string
    role: 'user' | 'assistant'
    parts: Array<{ type: 'text'; text: string }>
  }>
  sendMessage: ReturnType<typeof vi.fn>
  regenerate: ReturnType<typeof vi.fn>
  status: 'submitted' | 'streaming' | 'ready' | 'error'
  error?: Error
}

function mockUseChat(overrides: Partial<UseChatReturn> = {}): UseChatReturn {
  const value: UseChatReturn = {
    messages: [],
    sendMessage: vi.fn(),
    regenerate: vi.fn(),
    status: 'ready',
    error: undefined,
    ...overrides,
  }
  useChatMock.mockReturnValue(value)
  return value
}

describe('OnboardingChat — error banner + retry (ENT-88)', () => {
  beforeEach(() => {
    useChatMock.mockReset()
  })

  it('does not render the error banner while status is ready', () => {
    mockUseChat({ status: 'ready' })
    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByRole('button', { name: /retry/i })).toBeNull()
  })

  it('renders an inline error banner with a Retry button when status is error', () => {
    mockUseChat({ status: 'error', error: new Error('boom') })
    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    const banner = screen.getByRole('alert')
    expect(banner).toBeInTheDocument()
    expect(banner.textContent?.toLowerCase()).toMatch(/something went wrong/)

    const retry = screen.getByRole('button', { name: /retry/i })
    expect(retry).toBeInTheDocument()
  })

  it('clicking Retry calls regenerate', async () => {
    const regenerate = vi.fn()
    mockUseChat({ status: 'error', error: new Error('boom'), regenerate })

    const user = userEvent.setup()
    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    await user.click(screen.getByRole('button', { name: /retry/i }))
    expect(regenerate).toHaveBeenCalledTimes(1)
  })

  it('preserves typed text in the textarea after a failure', async () => {
    // First render: status ready, user types something, hits submit. We
    // simulate the failed roundtrip by re-rendering with status='error'
    // and asserting the textarea still holds the user's text.
    const sendMessage = vi.fn()
    mockUseChat({ status: 'ready', sendMessage })

    const user = userEvent.setup()
    const { rerender } = render(
      <OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />,
    )

    const textarea = screen.getByRole('textbox')
    await user.type(textarea, 'we sell SaaS to EU SMEs')
    await user.click(screen.getByRole('button', { name: /submit/i }))

    expect(sendMessage).toHaveBeenCalledWith({ text: 'we sell SaaS to EU SMEs' })

    // The server rejected — re-render with the error state.
    mockUseChat({
      status: 'error',
      error: new Error('boom'),
      sendMessage,
      messages: [
        {
          id: 'u1',
          role: 'user',
          parts: [{ type: 'text', text: 'we sell SaaS to EU SMEs' }],
        },
      ],
    })
    rerender(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    // Textarea is still the same element after rerender; user can fix &
    // resubmit, or click Retry to replay the existing user turn.
    expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe(
      'we sell SaaS to EU SMEs',
    )
  })
})
