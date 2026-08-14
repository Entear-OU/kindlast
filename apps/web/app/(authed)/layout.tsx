import Link from 'next/link'

import { SignOutForm } from '@/components/auth/sign-out-form'

/**
 * Shared layout for authenticated routes (ENT-91).
 *
 * Currently wraps `/onboarding`; ENT-46 will plug the dashboard nav into the
 * same shell. The header keeps two affordances visible at all times:
 *
 *   - a Kindlast brand link back to `/onboarding` (the user's home until the
 *     dashboard exists), and
 *   - a sign-out button that submits to the `signOut` server action and
 *     bounces to the identity provider’s end-session endpoint.
 *
 * Markup is a single column flex so children — chiefly the `OnboardingChat`'s
 * scrollable conversation — can claim the remaining viewport via `flex-1`.
 */
function KindlastMark() {
  // Same mark as `app/(public)/layout.tsx` but inlined and slightly smaller —
  // we deliberately don't share with the public header because the public
  // page lives in a different visual key (eggshell bg, larger logo).
  return (
    <svg
      aria-hidden="true"
      width="26"
      height="26"
      viewBox="0 0 56 56"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <rect width="56" height="56" rx="11" fill="currentColor" />
      <rect x="12" y="8" width="9" height="40" rx="2" fill="white" />
      <line
        x1="21"
        y1="28"
        x2="44"
        y2="9"
        stroke="white"
        strokeWidth="9"
        strokeLinecap="round"
      />
      <line
        x1="21"
        y1="28"
        x2="44"
        y2="47"
        stroke="white"
        strokeWidth="9"
        strokeLinecap="round"
      />
      <circle cx="21" cy="28" r="5.5" fill="#00C9A7" />
    </svg>
  )
}

export default function AuthedLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <div className="flex h-[100dvh] flex-col">
      <header className="border-b border-border/60 bg-background">
        <div className="mx-auto flex w-full max-w-3xl items-center justify-between px-4 py-3">
          <Link
            href="/onboarding"
            className="flex items-center gap-2 text-foreground transition-opacity hover:opacity-80"
          >
            <span className="text-foreground">
              <KindlastMark />
            </span>
            <span className="text-[15px] font-extrabold tracking-[-0.02em]">
              kindlast
            </span>
          </Link>
          <SignOutForm>
            <button
              type="submit"
              className="rounded-md px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
              Sign out
            </button>
          </SignOutForm>
        </div>
      </header>
      <div className="flex min-h-0 flex-1 flex-col">{children}</div>
    </div>
  )
}
