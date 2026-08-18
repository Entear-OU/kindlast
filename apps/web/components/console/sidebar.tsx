import Link from 'next/link'
import {
  Gauge,
  ListChecks,
  FolderOpen,
  ScrollText,
  Scale,
  Settings,
  BookUser,
} from 'lucide-react'

import { SignOutForm } from '@/components/auth/sign-out-form'
import { KindlastMark } from '@/components/console/mark'
import { NavLink } from '@/components/console/nav-link'
import { orgPath } from '@/lib/auth/org'

/**
 * The console's navigation (ENT-222).
 *
 * A server component. Only the individual links are client components, because
 * only they need `usePathname` to mark the active surface. Making the whole
 * sidebar client would drag SignOutForm across the boundary with it, and that
 * form reads a cookie through `next/headers`, which the client cannot do.
 *
 * WHAT IS AND IS NOT LINKED
 *
 * Only surfaces that exist are links. The rest are listed, plainly, as not
 * built yet.
 *
 * That is the ENT-202 rule rather than a stylistic choice: a control that
 * silently does nothing is worse than one visibly absent, and a nav item
 * leading to a 404 is the worst version of it, because the person concludes
 * the product is broken rather than unfinished. Listing them without linking
 * them says what the console will be without pretending it already is.
 *
 * They leave this list by becoming links, which is a one-line change per
 * surface as each lands.
 */

/**
 * Named in the rebuild, not yet built. Listed, never linked.
 *
 * Empty as of ENT-200, which took Records out of it. The list and its heading
 * are rendered only when something is in it: a "Coming next" heading over
 * nothing reads as a surface that failed to load rather than as a promise
 * already kept.
 */
const COMING: readonly { label: string; icon: typeof FolderOpen }[] = []

export function ConsoleSidebar({
  orgSlug,
  orgName,
}: {
  orgSlug: string
  orgName?: string
}) {
  return (
    <nav
      aria-label="Console"
      className="flex h-full flex-col gap-6 border-r border-border/60 bg-background px-4 py-6"
    >
      <Link
        href={orgPath(orgSlug)}
        className="flex items-center gap-2 px-2 text-foreground transition-opacity hover:opacity-80"
      >
        <KindlastMark />
        <span className="text-[15px] font-extrabold tracking-[-0.02em]">
          kindlast
        </span>
      </Link>

      <ul className="space-y-1">
        <li>
          <NavLink href={orgPath(orgSlug)} label="Overview" exact>
            <Gauge aria-hidden="true" className="size-4" />
          </NavLink>
        </li>
        <li>
          {/* Left COMING and became a link when ENT-203 landed it, which is the
              one-line change per surface this list was built for. */}
          <NavLink href={orgPath(orgSlug, '/feed')} label="Feed">
            <ListChecks aria-hidden="true" className="size-4" />
          </NavLink>
        </li>
        <li>
          {/* Left COMING when ENT-200 landed the read surface, the same
              one-line change per surface the list was built for. */}
          <NavLink href={orgPath(orgSlug, '/records')} label="Records">
            <FolderOpen aria-hidden="true" className="size-4" />
          </NavLink>
        </li>
        <li>
          {/* The corpus surface (ENT-207). Sits next to Records because the two
              answer neighbouring questions: what the law says, and what this
              organisation has on file about it. */}
          <NavLink href={orgPath(orgSlug, '/regulation')} label="Regulation">
            <Scale aria-hidden="true" className="size-4" />
          </NavLink>
        </li>
        <li>
          {/* Left COMING and became a link when ENT-223 landed the audit
              surface, the same one-line change per surface this list was built
              for. */}
          <NavLink href={orgPath(orgSlug, '/logs')} label="Logs">
            <ScrollText aria-hidden="true" className="size-4" />
          </NavLink>
        </li>
        <li>
          {/* What Kindlast knows about the organisation (ENT-228). Its own
              entry rather than a tab inside Settings, because a customer
              looking for what a product holds about them should not have to
              guess that it is filed under configuration. Rectification and
              erasure are the thing being sold here, not a preference. */}
          <NavLink
            href={orgPath(orgSlug, '/settings/memory')}
            label="What we know"
          >
            <BookUser aria-hidden="true" className="size-4" />
          </NavLink>
        </li>
        <li>
          <NavLink href={orgPath(orgSlug, '/settings')} label="Settings">
            <Settings aria-hidden="true" className="size-4" />
          </NavLink>
        </li>
      </ul>

      {COMING.length > 0 ? (
        <div>
          <p className="px-2 text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
            Coming next
          </p>
          <ul className="mt-2 space-y-1">
            {COMING.map(({ label, icon: Icon }) => (
              <li
                key={label}
                className="flex items-center gap-2.5 px-2 py-2 text-sm text-muted-foreground/60"
              >
                <Icon aria-hidden="true" className="size-4" />
                {label}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <div className="mt-auto space-y-3 border-t border-border/60 pt-4">
        {orgName ? (
          <p
            data-testid="chrome-org"
            className="truncate px-2 text-sm font-medium text-foreground"
            title={orgName}
          >
            {orgName}
          </p>
        ) : null}
        <SignOutForm>
          <button
            type="submit"
            className="w-full cursor-pointer rounded-lg px-2 py-2 text-left text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            Sign out
          </button>
        </SignOutForm>
      </div>
    </nav>
  )
}
