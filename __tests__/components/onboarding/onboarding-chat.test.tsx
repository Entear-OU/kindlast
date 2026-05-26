import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * ENT-92 — RTL smoke coverage for `OnboardingChat`.
 *
 * The persistence layer is exercised end-to-end against the local Supabase
 * stack in `tests/integration/onboarding-persistence.test.ts`. Playwright
 * covers the full streaming round-trip — by hand, not in CI. This suite
 * fills the missing layer: the client component itself, with `useChat`
 * mocked so we can drive `messages`, `sendMessage`, and `status`
 * deterministically.
 */

const { useChatMock } = vi.hoisted(() => ({ useChatMock: vi.fn() }))
vi.mock('@ai-sdk/react', () => ({
  useChat: useChatMock,
}))

// The component constructs a `DefaultChatTransport` at module top; stubbing
// keeps the AI SDK's network plumbing out of jsdom.
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
  status: 'submitted' | 'streaming' | 'ready' | 'error'
}

function mockUseChat(overrides: Partial<UseChatReturn> = {}): UseChatReturn {
  const value: UseChatReturn = {
    messages: [],
    sendMessage: vi.fn(),
    status: 'ready',
    ...overrides,
  }
  useChatMock.mockReturnValue(value)
  return value
}

describe('OnboardingChat', () => {
  beforeEach(() => {
    useChatMock.mockReset()
  })

  it('renders the empty-state copy when initialMessages is empty', () => {
    mockUseChat({ messages: [] })
    render(<OnboardingChat sessionId="s1" initialMessages={[]} />)

    expect(screen.getByText(/ready when you are/i)).toBeInTheDocument()
    expect(
      screen.getByText(/type a quick hello to begin/i),
    ).toBeInTheDocument()
  })

  it('disables the submit button while the input is empty', () => {
    mockUseChat()
    render(<OnboardingChat sessionId="s1" initialMessages={[]} />)

    const submit = screen.getByRole('button', { name: /submit/i })
    expect(submit).toBeDisabled()
  })

  it('keeps the submit button disabled when the input is whitespace only', async () => {
    mockUseChat()
    const user = userEvent.setup()
    render(<OnboardingChat sessionId="s1" initialMessages={[]} />)

    await user.type(screen.getByRole('textbox'), '   ')
    expect(screen.getByRole('button', { name: /submit/i })).toBeDisabled()
  })

  it('enables the submit button once the input has non-whitespace content', async () => {
    mockUseChat()
    const user = userEvent.setup()
    render(<OnboardingChat sessionId="s1" initialMessages={[]} />)

    await user.type(screen.getByRole('textbox'), 'we sell SaaS')
    expect(screen.getByRole('button', { name: /submit/i })).toBeEnabled()
  })

  it('calls sendMessage with the trimmed input on submit', async () => {
    const sendMessage = vi.fn()
    mockUseChat({ sendMessage })
    const user = userEvent.setup()
    render(<OnboardingChat sessionId="s1" initialMessages={[]} />)

    await user.type(screen.getByRole('textbox'), '  we sell SaaS  ')
    await user.click(screen.getByRole('button', { name: /submit/i }))

    expect(sendMessage).toHaveBeenCalledWith({ text: 'we sell SaaS' })
  })

  it('reflects the streaming status on the submit affordance via aria-label', () => {
    // `PromptInputSubmit` swaps its `aria-label` between Submit/Stop based on
    // status. Asserting via accessible name keeps the test resilient to the
    // icon swap (Spinner / SquareIcon / CornerDownLeftIcon).
    mockUseChat({ status: 'streaming' })
    render(<OnboardingChat sessionId="s1" initialMessages={[]} />)

    expect(screen.getByRole('button', { name: /stop/i })).toBeInTheDocument()
  })

  it('renders prior user + assistant turns from messages', () => {
    mockUseChat({
      messages: [
        {
          id: 'u1',
          role: 'user',
          parts: [{ type: 'text', text: 'We build SME accounting tools.' }],
        },
        {
          id: 'a1',
          role: 'assistant',
          parts: [{ type: 'text', text: 'Got it — which countries do you serve?' }],
        },
      ],
    })

    render(<OnboardingChat sessionId="s1" initialMessages={[]} />)

    expect(
      screen.getByText('We build SME accounting tools.'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/which countries do you serve\?/i),
    ).toBeInTheDocument()
    // Empty-state copy should NOT show when messages are present.
    expect(screen.queryByText(/ready when you are/i)).toBeNull()
  })
})
