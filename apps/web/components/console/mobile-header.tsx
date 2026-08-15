import Link from 'next/link'

import { SignOutForm } from '@/components/auth/sign-out-form'
import { KindlastMark } from '@/components/console/mark'
import { orgPath } from '@/lib/auth/org'

/**
 * The phone header (ENT-222).
 *
 * Everything the sidebar carries that is not navigation: which organisation
 * you are in, and the way out. Navigation itself moved to the bottom bar,
 * where a thumb reaches it.
 *
 * The organisation name is here rather than in the tab bar because it is not a
 * destination, it is context, and a person switching between three client
 * companies on a phone needs to see which one they are looking at without
 * opening anything.
 *
 * A server component, which is what lets it keep SignOutForm: that form reads
 * a cookie through `next/headers` and cannot cross a client boundary.
 */
export function MobileHeader({
  orgSlug,
  orgName,
}: {
  orgSlug: string
  orgName?: string
}) {
  return (
    <header className="sticky top-0 z-10 flex items-center justify-between gap-3 border-b border-border/60 bg-background px-4 py-3 md:hidden">
      <Link
        href={orgPath(orgSlug)}
        className="flex min-w-0 items-center gap-2 text-foreground transition-opacity hover:opacity-80"
      >
        <KindlastMark />
        {orgName ? (
          <span
            data-testid="mobile-org"
            className="truncate text-[15px] font-medium"
          >
            {orgName}
          </span>
        ) : (
          <span className="text-[15px] font-extrabold tracking-[-0.02em]">
            kindlast
          </span>
        )}
      </Link>

      <SignOutForm>
        <button
          type="submit"
          className="shrink-0 cursor-pointer rounded-lg px-2 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        >
          Sign out
        </button>
      </SignOutForm>
    </header>
  )
}
