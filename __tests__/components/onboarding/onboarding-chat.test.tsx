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

import { OnboardingChat, OPENING_TRIGGER } from '@/components/onboarding/onboarding-chat'

type MessagePart =
  | { type: 'text'; text: string }
  | {
      type: 'tool-complete_onboarding'
      toolCallId: string
      state: string
      output?: unknown
    }

type UseChatReturn = {
  messages: Array<{
    id: string
    role: 'user' | 'assistant'
    parts: MessagePart[]
  }>
  sendMessage: ReturnType<typeof vi.fn>
  setMessages: ReturnType<typeof vi.fn>
  status: 'submitted' | 'streaming' | 'ready' | 'error'
}

function mockUseChat(overrides: Partial<UseChatReturn> = {}): UseChatReturn {
  const value: UseChatReturn = {
    messages: [],
    sendMessage: vi.fn(),
    setMessages: vi.fn(),
    status: 'ready',
    ...overrides,
  }
  useChatMock.mockReturnValue(value)
  return value
}

const greeting = {
  id: 'a1',
  role: 'assistant' as const,
  parts: [{ type: 'text' as const, text: 'Hi — what does your company do?' }],
}

describe('OnboardingChat', () => {
  beforeEach(() => {
    useChatMock.mockReset()
  })

  it('disables the submit button while the input is empty', () => {
    mockUseChat()
    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    const submit = screen.getByRole('button', { name: /submit/i })
    expect(submit).toBeDisabled()
  })

  it('keeps the submit button disabled when the input is whitespace only', async () => {
    mockUseChat()
    const user = userEvent.setup()
    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    await user.type(screen.getByRole('textbox'), '   ')
    expect(screen.getByRole('button', { name: /submit/i })).toBeDisabled()
  })

  it('enables the submit button once the input has non-whitespace content', async () => {
    mockUseChat()
    const user = userEvent.setup()
    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    await user.type(screen.getByRole('textbox'), 'we sell SaaS')
    expect(screen.getByRole('button', { name: /submit/i })).toBeEnabled()
  })

  it('calls sendMessage with the trimmed input on submit', async () => {
    const sendMessage = vi.fn()
    mockUseChat({ sendMessage })
    const user = userEvent.setup()
    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    await user.type(screen.getByRole('textbox'), '  we sell SaaS  ')
    await user.click(screen.getByRole('button', { name: /submit/i }))

    expect(sendMessage).toHaveBeenCalledWith({ text: 'we sell SaaS' })
  })

  it('reflects the streaming status on the submit affordance via aria-label', () => {
    // `PromptInputSubmit` swaps its `aria-label` between Submit/Stop based on
    // status. Asserting via accessible name keeps the test resilient to the
    // icon swap (Spinner / SquareIcon / CornerDownLeftIcon).
    mockUseChat({ status: 'streaming' })
    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

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

    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    expect(
      screen.getByText('We build SME accounting tools.'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(/which countries do you serve\?/i),
    ).toBeInTheDocument()
  })

  // ENT-154: a fresh session renders instantly with an empty transcript and the
  // client streams the opening question in via a hidden trigger turn, rather
  // than the page blocking its render on server-side generation.
  it('streams the opening question on a fresh session (startOpening)', () => {
    const sendMessage = vi.fn()
    mockUseChat({ messages: [], sendMessage, status: 'ready' })

    render(
      <OnboardingChat
        sessionId="s1"
        initialMessages={[]}
        initialSummary={null}
        startOpening
      />,
    )

    expect(sendMessage).toHaveBeenCalledTimes(1)
    expect(sendMessage).toHaveBeenCalledWith({ text: OPENING_TRIGGER })
  })

  it('does not stream an opening when the transcript already has messages', () => {
    const sendMessage = vi.fn()
    mockUseChat({ messages: [greeting], sendMessage, status: 'ready' })

    render(
      <OnboardingChat
        sessionId="s1"
        initialMessages={[greeting]}
        initialSummary={null}
        startOpening
      />,
    )

    expect(sendMessage).not.toHaveBeenCalled()
  })

  it('does not stream an opening when startOpening is false', () => {
    const sendMessage = vi.fn()
    mockUseChat({ messages: [], sendMessage, status: 'ready' })

    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    expect(sendMessage).not.toHaveBeenCalled()
  })

  // ENT-167: the route answers a premature `complete_onboarding` call with
  // `too_early` and lets the model ask the missing question instead. The
  // rejected call stays in the transcript forever, so keying "drafting" off
  // its mere presence locked the composer for the rest of the interview.
  describe('complete_onboarding tool state', () => {
    const summary = {
      covered: [{ key: 'business_mapped', label: 'Business profile mapped' }],
      missing: [{ key: 'ropa', label: 'Record of Processing Activities' }],
      topAction: {
        id: 'ropa',
        key: 'ropa',
        title: 'Start a Record of Processing Activities',
        description: 'You process employee data but have no ROPA yet.',
        regulation: 'GDPR Article 30',
        severity: 'high' as const,
      },
    }

    function toolTurn(part: MessagePart) {
      return { id: 'a2', role: 'assistant' as const, parts: [part] }
    }

    it('re-enables the composer after a too_early finalisation attempt', () => {
      mockUseChat({
        status: 'ready',
        messages: [
          greeting,
          toolTurn({
            type: 'tool-complete_onboarding',
            toolCallId: 'call_1',
            state: 'output-available',
            output: { status: 'too_early', missing: ['ropaStatus'], message: 'Keep interviewing.' },
          }),
        ],
      })

      render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

      expect(screen.getByRole('textbox')).toBeEnabled()
      expect(screen.getByPlaceholderText(/type your answer/i)).toBeInTheDocument()
      expect(screen.queryByText(/drafting/i)).not.toBeInTheDocument()
    })

    it('re-enables the composer when the finalisation call errors', () => {
      mockUseChat({
        status: 'ready',
        messages: [
          greeting,
          toolTurn({
            type: 'tool-complete_onboarding',
            toolCallId: 'call_1',
            state: 'output-error',
          }),
        ],
      })

      render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

      expect(screen.getByRole('textbox')).toBeEnabled()
    })

    it('disables the composer and shows the indicator while finalisation is in flight', () => {
      mockUseChat({
        status: 'streaming',
        messages: [
          greeting,
          toolTurn({
            type: 'tool-complete_onboarding',
            toolCallId: 'call_1',
            state: 'input-available',
          }),
        ],
      })

      render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

      expect(screen.getByRole('textbox')).toBeDisabled()
      expect(screen.getByText(/drafting your compliance posture/i)).toBeInTheDocument()
    })

    it('swaps the composer for the posture summary once finalisation completes', () => {
      mockUseChat({
        status: 'ready',
        messages: [
          greeting,
          toolTurn({
            type: 'tool-complete_onboarding',
            toolCallId: 'call_1',
            state: 'output-available',
            output: { status: 'completed', summary },
          }),
        ],
      })

      render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

      expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
      expect(screen.getByText(/start a record of processing activities/i)).toBeInTheDocument()
    })

    it('re-enables the composer after a too_early call that precedes a later question', () => {
      // The model's second step asks the missing question in a new turn; the
      // stale tool part still sits earlier in the transcript.
      mockUseChat({
        status: 'ready',
        messages: [
          greeting,
          toolTurn({
            type: 'tool-complete_onboarding',
            toolCallId: 'call_1',
            state: 'output-available',
            output: { status: 'too_early', missing: ['ropaStatus'], message: 'Keep interviewing.' },
          }),
          {
            id: 'a3',
            role: 'assistant' as const,
            parts: [{ type: 'text' as const, text: 'Do you keep a ROPA today?' }],
          },
        ],
      })

      render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

      expect(screen.getByRole('textbox')).toBeEnabled()
    })
  })

  it('never renders the hidden opening trigger turn', () => {
    mockUseChat({
      messages: [
        { id: 'trigger', role: 'user', parts: [{ type: 'text', text: OPENING_TRIGGER }] },
        {
          id: 'a1',
          role: 'assistant',
          parts: [{ type: 'text', text: 'Hi — what does your company do?' }],
        },
      ],
    })

    render(<OnboardingChat sessionId="s1" initialMessages={[]} initialSummary={null} />)

    expect(screen.queryByText(OPENING_TRIGGER)).not.toBeInTheDocument()
    expect(screen.getByText(/what does your company do\?/i)).toBeInTheDocument()
  })
})
