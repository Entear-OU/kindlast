import { beforeEach, describe, expect, it, vi } from 'vitest'

/**
 * ENT-170 — the onboarding pages refuse to re-interview a founder who has
 * already onboarded.
 *
 * `getOrCreateActiveSession` only sees `in_progress` sessions, so once the
 * interview completes it inserts a *new* empty one. That made `/onboarding`
 * render its pre-onboarding splash again and `/onboarding/chat` open a fresh
 * question one, wiping the completed transcript from view. ENT-166 gated the
 * console on having a compliance profile; these are the inverse gate.
 */

const {
  getUserMock,
  hasComplianceProfileMock,
  getOrCreateActiveSessionMock,
  loadTranscriptMock,
  loadComplianceProfileMock,
  redirectMock,
} = vi.hoisted(() => ({
  getUserMock: vi.fn(),
  hasComplianceProfileMock: vi.fn(),
  getOrCreateActiveSessionMock: vi.fn(),
  loadTranscriptMock: vi.fn(),
  loadComplianceProfileMock: vi.fn(),
  redirectMock: vi.fn(),
}))

vi.mock('@/lib/supabase/server', () => ({
  createClient: async () => ({ auth: { getUser: getUserMock } }),
}))

vi.mock('@/lib/console/require-profile', () => ({
  hasComplianceProfile: hasComplianceProfileMock,
}))

vi.mock('@/lib/onboarding/persistence', () => ({
  getOrCreateActiveSession: getOrCreateActiveSessionMock,
  loadTranscript: loadTranscriptMock,
  loadComplianceProfile: loadComplianceProfileMock,
  profileFromRow: (row: unknown) => row,
  uiMessageFromRow: (row: unknown) => row,
}))

// Real `redirect` throws to unwind the render; mirror that so a page which
// keeps working after redirecting fails the test.
vi.mock('next/navigation', () => ({
  redirect: (url: string) => {
    redirectMock(url)
    throw new Error(`NEXT_REDIRECT:${url}`)
  },
}))

vi.mock('next/link', () => ({
  default: ({ href, children }: { href: string; children: React.ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}))

import OnboardingPage from '@/app/(authed)/onboarding/page'
import OnboardingChatPage from '@/app/(authed)/onboarding/chat/page'

beforeEach(() => {
  vi.clearAllMocks()
  getUserMock.mockResolvedValue({ data: { user: { id: 'u1' } } })
  getOrCreateActiveSessionMock.mockResolvedValue('s1')
  loadTranscriptMock.mockResolvedValue([])
  loadComplianceProfileMock.mockResolvedValue(null)
})

describe('/onboarding (ENT-170)', () => {
  it('sends an onboarded founder to the dashboard', async () => {
    hasComplianceProfileMock.mockResolvedValue(true)

    await expect(OnboardingPage()).rejects.toThrow('NEXT_REDIRECT:/dashboard')
    expect(redirectMock).toHaveBeenCalledWith('/dashboard')
  })

  it('does not open a session for an onboarded founder', async () => {
    hasComplianceProfileMock.mockResolvedValue(true)

    await expect(OnboardingPage()).rejects.toThrow()
    expect(getOrCreateActiveSessionMock).not.toHaveBeenCalled()
  })

  it('still shows the splash to a founder who has not onboarded', async () => {
    hasComplianceProfileMock.mockResolvedValue(false)

    await OnboardingPage()
    expect(redirectMock).not.toHaveBeenCalledWith('/dashboard')
    expect(getOrCreateActiveSessionMock).toHaveBeenCalled()
  })
})

describe('/onboarding/chat (ENT-170)', () => {
  it('sends an onboarded founder to the dashboard instead of a new interview', async () => {
    hasComplianceProfileMock.mockResolvedValue(true)

    await expect(OnboardingChatPage()).rejects.toThrow('NEXT_REDIRECT:/dashboard')
    expect(redirectMock).toHaveBeenCalledWith('/dashboard')
    expect(getOrCreateActiveSessionMock).not.toHaveBeenCalled()
  })

  it('still renders the interview for a founder who has not onboarded', async () => {
    hasComplianceProfileMock.mockResolvedValue(false)

    await OnboardingChatPage()
    expect(redirectMock).not.toHaveBeenCalledWith('/dashboard')
    expect(getOrCreateActiveSessionMock).toHaveBeenCalled()
  })
})
