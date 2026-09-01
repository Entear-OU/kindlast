import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * What Kindy is answering about (ENT-284).
 *
 * Kindy's one conversational path is AskAboutFinding, because a finding names
 * exactly one obligation and that is what lets a citation to anything else be
 * refused. So every exchange has a subject, and the only question is who
 * chooses it.
 *
 * Until this test existed the action chose, by recency: it read the newest
 * pending finding and answered about that whatever the reader had open. Open
 * the DPO finding, ask about it, get an answer about the ROPA gap because the
 * ROPA gap was raised later. The answer is correct about a finding nobody
 * asked about, which is the worst shape a wrong answer can take here: it
 * carries a real citation, so it survives being checked.
 *
 * These tests pin the two halves of the fix. The subject comes from where the
 * question was asked, and where there is none the action asks rather than
 * guesses. `nothing-open` is untouched: an organisation with nothing pending
 * is still told so from code rather than from a model.
 */
const currentSession = vi.fn()
const resolveOrg = vi.fn()
const listFindings = vi.fn()
const getFinding = vi.fn()
const askAboutFinding = vi.fn()

vi.mock('@/lib/auth/session', () => ({
  currentSession: () => currentSession(),
}))
vi.mock('@/lib/auth/org', () => ({
  resolveOrg: (...args: unknown[]) => resolveOrg(...args),
}))
vi.mock('@/lib/findings/client', () => ({
  listFindings: (...args: unknown[]) => listFindings(...args),
  getFinding: (...args: unknown[]) => getFinding(...args),
}))
vi.mock('@/lib/agents/conversation', () => ({
  askAboutFinding: (...args: unknown[]) => askAboutFinding(...args),
  MAX_QUESTION_CHARS: 1000,
}))

const { askKindy } = await import('@/app/(authed)/o/[org]/kindy-actions')
const { KINDY_IDLE } = await import('@/components/console/kindy-state')

/** The form the composer posts: the words, the slug, and maybe a subject. */
function form(fields: Record<string, string>) {
  const data = new FormData()
  for (const [name, value] of Object.entries(fields)) data.append(name, value)
  return data
}

function finding(findingId: string, detected: string) {
  return { findingId, detected, status: 'pending', severity: 'high' }
}

const OPEN = [
  finding('f-ropa', 'Profile gap: ROPA'),
  finding('f-dpo', 'Profile gap: no DPO named'),
  finding('f-dpia', 'Profile gap: no DPIA for the scoring model'),
]

const ANSWERED = {
  ok: true as const,
  value: {
    intelligenceAvailable: true,
    outcome: 'ANSWER_OUTCOME_SUCCEEDED' as const,
    answer: 'You still owe a written record of processing.',
  },
}

beforeEach(() => {
  vi.clearAllMocks()
  currentSession.mockResolvedValue({ accessToken: 'token' })
  resolveOrg.mockResolvedValue({
    status: 'ok',
    membership: { orgId: 'org-1', orgSlug: 'acme-ltd', orgName: 'Acme Ltd' },
  })
  // Newest first, which is how the feed reads and how the old action chose.
  listFindings.mockResolvedValue({ ok: true, value: { findings: OPEN } })
  getFinding.mockImplementation(async (_t: string, _o: string, id: string) => {
    const found = OPEN.find((f) => f.findingId === id)
    return found
      ? { ok: true, value: { finding: found } }
      : { ok: false, error: { kind: 'missing', message: 'no such finding' } }
  })
  askAboutFinding.mockResolvedValue(ANSWERED)
})

describe('a question asked on a finding page', () => {
  it('is answered about that finding, not the newest one', async () => {
    const state = await askKindy(
      KINDY_IDLE,
      form({ slug: 'acme-ltd', ask: 'why us?', findingId: 'f-dpo' }),
    )

    expect(askAboutFinding).toHaveBeenCalledTimes(1)
    expect(askAboutFinding).toHaveBeenCalledWith(
      'token',
      'org-1',
      'f-dpo',
      'why us?',
    )
    expect(state).toMatchObject({
      status: 'answered',
      findingId: 'f-dpo',
      findingTitle: 'Profile gap: no DPO named',
    })
  })

  it('names the subject from the record rather than from the form', async () => {
    // The title is what the panel shows above the answer, so it has to come
    // from the organisation's own rows. A title posted by the browser is a
    // title anybody can edit, and a mislabelled subject is exactly the bug
    // this issue is about.
    const state = await askKindy(
      KINDY_IDLE,
      form({
        slug: 'acme-ltd',
        ask: 'why us?',
        findingId: 'f-dpo',
        findingTitle: 'Something else entirely',
      }),
    )

    expect(state).toMatchObject({ findingTitle: 'Profile gap: no DPO named' })
  })

  it('asks nothing about a finding this organisation does not have', async () => {
    const state = await askKindy(
      KINDY_IDLE,
      form({ slug: 'acme-ltd', ask: 'why us?', findingId: 'f-someone-elses' }),
    )

    expect(askAboutFinding).not.toHaveBeenCalled()
    expect(state).toMatchObject({ status: 'error' })
  })
})

describe('a question asked anywhere else', () => {
  it('picks no finding at all, and offers the open ones instead', async () => {
    const state = await askKindy(
      KINDY_IDLE,
      form({ slug: 'acme-ltd', ask: 'where are we?' }),
    )

    // The whole of the bug, in one assertion: with no subject, nothing is
    // asked. A guessed subject produces an answer that reads as authoritative
    // about a finding nobody named.
    expect(askAboutFinding).not.toHaveBeenCalled()
    expect(state).toMatchObject({
      status: 'choose',
      question: 'where are we?',
      choices: [
        { findingId: 'f-ropa', findingTitle: 'Profile gap: ROPA' },
        { findingId: 'f-dpo', findingTitle: 'Profile gap: no DPO named' },
        {
          findingId: 'f-dpia',
          findingTitle: 'Profile gap: no DPIA for the scoring model',
        },
      ],
    })
  })

  it('keeps the question, so choosing does not mean typing it again', async () => {
    const state = await askKindy(
      KINDY_IDLE,
      form({ slug: 'acme-ltd', ask: 'where are we?' }),
    )

    expect(state).toMatchObject({ question: 'where are we?' })
  })

  it('still says nothing is open, from code, when nothing is pending', async () => {
    listFindings.mockResolvedValue({ ok: true, value: { findings: [] } })

    const state = await askKindy(
      KINDY_IDLE,
      form({ slug: 'acme-ltd', ask: 'where are we?' }),
    )

    expect(askAboutFinding).not.toHaveBeenCalled()
    expect(state).toEqual({ status: 'nothing-open', question: 'where are we?' })
  })

  it('reads only the findings still needing a decision', async () => {
    await askKindy(KINDY_IDLE, form({ slug: 'acme-ltd', ask: 'where are we?' }))

    expect(listFindings).toHaveBeenCalledWith(
      'token',
      'org-1',
      expect.objectContaining({ status: 'pending' }),
    )
  })
})
