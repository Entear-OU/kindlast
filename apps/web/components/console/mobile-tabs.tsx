'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { Gauge, ListChecks, Settings, Bot } from 'lucide-react'

import { orgPath } from '@/lib/auth/org'
import { cn } from '@/lib/utils'

/**
 * The bottom tab bar (ENT-222).
 *
 * On a phone the sidebar is wrong twice over: it stacks above the content and
 * eats the first screen, and a vertical list of labels down the side of a
 * 390px viewport is a desktop pattern wearing a media query. So below `md` the
 * navigation moves to where a thumb is, as a tab bar.
 *
 * ICONS ONLY, WHICH IS A DECISION ABOUT REACH RATHER THAN TASTE
 *
 * Three tabs with labels would fit. The reason to drop the labels is that an
 * icon-only bar keeps each target wide enough to hit without looking, which is
 * what a thumb on a moving train actually needs. The names are still there for
 * anyone not looking at the screen: every tab carries an accessible name, so a
 * screen reader announces "Overview" rather than "link".
 *
 * ONLY WHAT EXISTS, SAME AS THE SIDEBAR
 *
 * Records is absent rather than present-and-dead. The sidebar lists it under
 * "Coming next" because it has the room to say so; a tab bar does not, and a
 * greyed tab is exactly the inert control ENT-202 argues against. Feed joined
 * the bar when ENT-203 built it, which is how a surface graduates.
 *
 * The third tab is the agent rail, which has nowhere else to go on a phone.
 * That is the point rather than a consolation: "has anything looked at my
 * compliance yet" is the question this product exists to answer, and it should
 * not be desktop-only.
 */

interface Tab {
  href: string
  label: string
  icon: typeof Gauge
  /** Exact match, for the tab that is the parent of the others. */
  exact?: boolean
}

export function MobileTabs({ orgSlug }: { orgSlug: string }) {
  const pathname = usePathname()

  const tabs: Tab[] = [
    { href: orgPath(orgSlug), label: 'Overview', icon: Gauge, exact: true },
    { href: orgPath(orgSlug, '/feed'), label: 'Feed', icon: ListChecks },
    { href: orgPath(orgSlug, '/settings'), label: 'Settings', icon: Settings },
    { href: '#agents', label: 'Your agents', icon: Bot },
  ]

  return (
    <nav
      aria-label="Console"
      // Sticky rather than fixed: fixed would sit over the last line of every
      // page and need padding added to each one to compensate. Sticky inside
      // the scrolling column keeps the bar on screen and off the content.
      //
      // pb-[env(safe-area-inset-bottom)] so the bar clears the home indicator
      // on a phone that has one, rather than putting a tap target under it.
      className="sticky bottom-0 z-10 border-t border-border/60 bg-background pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      <ul className="flex items-stretch">
        {tabs.map(({ href, label, icon: Icon, exact }) => {
          const active = href.startsWith('#')
            ? false
            : exact
              ? pathname === href
              : pathname.startsWith(href)

          return (
            <li key={href} className="flex-1">
              <Link
                href={href}
                aria-label={label}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  // min-h-14 rather than padding alone: the whole cell is the
                  // target, so a thumb landing anywhere in the column works.
                  'flex min-h-14 items-center justify-center transition-colors',
                  // Colour, not an underline. Every native tab bar marks the
                  // current tab by tinting it, and the first version's rule
                  // under the icon read as a stray line at the screen edge
                  // rather than as state.
                  active
                    ? 'text-primary'
                    : 'text-muted-foreground hover:text-foreground',
                )}
              >
                <Icon aria-hidden="true" className="size-5" />
                {/* The only name this tab has. Colour marks the current one
                    for anyone looking; aria-current says the same thing to
                    anyone not. */}
                <span className="sr-only">{label}</span>
              </Link>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}
