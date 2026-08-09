import Link from 'next/link'
import { redirect } from 'next/navigation'

import { hasComplianceProfile } from '@/lib/console/require-profile'
import { getOrCreateActiveSession, loadTranscript } from '@/lib/onboarding/persistence'
import { createClient } from '@/lib/supabase/server'

export default async function OnboardingPage() {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    redirect('/login')
  }

  // ENT-170: the inverse of the console's onboarding gate. `getOrCreateActiveSession`
  // only sees `in_progress` sessions, so for a founder who has already finished
  // it opens a brand-new empty one and this page invites them to start over.
  // Check the profile first, so the check itself has no side effect either.
  if (await hasComplianceProfile(supabase, user.id)) {
    redirect('/dashboard')
  }

  const sessionId = await getOrCreateActiveSession(supabase, user.id)
  const rows = await loadTranscript(supabase, sessionId)

  if (rows.length > 0) {
    redirect('/onboarding/chat')
  }

  return (
    <main className="flex flex-1 flex-col items-center justify-center px-4">
      <div className="flex max-w-md flex-col items-center text-center">
        <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-muted-foreground/70">
          Onboarding
        </p>
        <h1 className="mt-2 text-balance font-black text-3xl tracking-tight">
          Let&apos;s build your compliance posture.
        </h1>
        <p className="mt-3 text-muted-foreground text-[0.875rem] leading-relaxed">
          A short conversation to map your business to your initial GDPR and EU AI Act profile.
          Your answers stay private.
        </p>
        <Link
          href="/onboarding/chat"
          className="mt-8 inline-flex items-center rounded-full bg-foreground px-6 py-3 text-sm font-medium text-background transition-opacity hover:opacity-90"
        >
          Get started
        </Link>
      </div>
    </main>
  )
}
