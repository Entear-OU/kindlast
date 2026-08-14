import Link from 'next/link'

import { SignOutForm } from '@/components/auth/sign-out-form'
import { orgPath } from '@/lib/auth/org'

/**
 * The header every authenticated page sits under (ENT-91, re-homed by
 * ENT-198).
 *
 * A plain synchronous component, taking the organisation as props rather than
 * resolving it. That split is deliberate: the layout above it is an async
 * server component that reads a session and calls core-api, and folding the
 * chrome into it would mean rendering React in order to test a tenancy
 * decision, or rendering core-api in order to test a header.
 *
 * The brand link is organisation-scoped, so "home" means this organisation's
 * home rather than a resolver that has to look one up. It is also the seam
 * §22.4's organisation switcher grows into.
 */
function KindlastMark() {
  // Same mark as `app/(public)/layout.tsx` but inlined and slightly smaller.
  // Deliberately not shared with the public header: the public page lives in a
  // different visual key (eggshell background, larger logo).
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
      <line x1="21" y1="28" x2="44" y2="9" stroke="white" strokeWidth="9" strokeLinecap="round" />
      <line x1="21" y1="28" x2="44" y2="47" stroke="white" strokeWidth="9" strokeLinecap="round" />
      <circle cx="21" cy="28" r="5.5" fill="#00C9A7" />
    </svg>
  )
}

export function ConsoleChrome({
  orgSlug,
  orgName,
  children,
}: {
  orgSlug: string
  orgName?: string
  children: React.ReactNode
}) {
  return (
    <div className="flex h-[100dvh] flex-col">
      <header className="border-b border-border/60 bg-background">
        <div className="mx-auto flex w-full max-w-3xl items-center justify-between px-4 py-3">
          <div className="flex items-center gap-3">
            <Link
              href={orgPath(orgSlug)}
              className="flex items-center gap-2 text-foreground transition-opacity hover:opacity-80"
            >
              <span className="text-foreground">
                <KindlastMark />
              </span>
              <span className="text-[15px] font-extrabold tracking-[-0.02em]">kindlast</span>
            </Link>
            {orgName ? (
              <>
                <span aria-hidden="true" className="text-border">
                  /
                </span>
                <span
                  data-testid="chrome-org"
                  className="text-[15px] font-medium text-muted-foreground"
                >
                  {orgName}
                </span>
              </>
            ) : null}
          </div>
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
