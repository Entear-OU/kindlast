'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useEffect, useState } from 'react'
import { GitHubMark } from '@/components/icons/github-mark'
import { GITHUB_REPO_URL } from '@/lib/links'

/**
 * The public header.
 *
 * Two routes now open on a full-bleed dark plate, so a solid warm bar sitting
 * on top of them reads as a mistake. Over a dark hero the header is
 * transparent with white type; once the reader scrolls past that hero it
 * resolves to the solid warm bar with dark type. On light-topped routes it is
 * simply always solid.
 *
 * The route list is explicit rather than inferred: a page either opens on a
 * dark plate or it does not, and guessing from scroll position alone would
 * flash the wrong colour on first paint.
 */
const DARK_HERO_ROUTES = new Set(['/', '/how-it-works'])

/** Roughly the height of a hero, past which the bar always solidifies. */
const SOLIDIFY_AFTER_PX = 120

function KindlastIcon({ size = 32 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 56 56" fill="none" xmlns="http://www.w3.org/2000/svg">
      <rect width="56" height="56" rx="11" fill="#0D1B2A" />
      <rect x="12" y="8" width="9" height="40" rx="2" fill="white" />
      <line x1="21" y1="28" x2="44" y2="9" stroke="white" strokeWidth="9" strokeLinecap="round" />
      <line x1="21" y1="28" x2="44" y2="47" stroke="white" strokeWidth="9" strokeLinecap="round" />
      <circle cx="21" cy="28" r="5.5" fill="#00C9A7" />
    </svg>
  )
}

export function SiteHeader() {
  const pathname = usePathname()
  const [scrolled, setScrolled] = useState(false)

  const hasDarkHero = DARK_HERO_ROUTES.has(pathname ?? '')
  const overDarkHero = hasDarkHero && !scrolled

  // `pathname` is in the deps deliberately. A client-side route change keeps
  // this component mounted, so re-running the effect is what resyncs the bar
  // to the new page's scroll position instead of leaking the old one.
  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > SOLIDIFY_AFTER_PX)
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [pathname])

  const link = overDarkHero
    ? 'text-white/60 hover:text-white'
    : 'text-[#0D1B2A]/45 hover:text-[#0D1B2A]'

  return (
    <>
    {/* Fixed rather than sticky. A transparent bar has to have the hero
        running underneath it, and a sticky header occupies its own strip of
        layout, so the page background showed through instead of the plate.
        Light-topped routes get the spacer below to compensate. */}
    <header
      data-over-hero={overDarkHero ? 'true' : 'false'}
      className={[
        'fixed inset-x-0 top-0 z-50 transition-colors duration-300',
        overDarkHero
          ? 'border-b border-transparent bg-transparent'
          : 'border-b border-black/[0.05] bg-[#F5F4F0]/92 backdrop-blur-2xl',
      ].join(' ')}
    >
      <div className="mx-auto flex h-[70px] max-w-5xl items-center justify-between px-6 lg:px-8">

        <Link href="/" className="group flex items-center gap-2.5">
          <KindlastIcon size={32} />
          <span
            className={[
              'text-[18px] font-extrabold tracking-[-0.03em] transition-colors duration-300',
              overDarkHero ? 'text-white' : 'text-[#0D1B2A]',
            ].join(' ')}
          >
            kindlast
          </span>
        </Link>

        {/* Real routes, not in-page anchors: an anchor only resolved on `/`. */}
        <nav className="hidden items-center gap-8 md:flex">
          <Link
            href="/how-it-works"
            className={`text-[15px] font-medium tracking-[-0.01em] transition-colors duration-150 ${link}`}
          >
            How it works
          </Link>
          <Link
            href="/features"
            className={`text-[15px] font-medium tracking-[-0.01em] transition-colors duration-150 ${link}`}
          >
            Features
          </Link>
        </nav>

        <div className="flex items-center gap-4">
          {/* Icon-only, so it reads as a developer affordance rather than
              repeating the worded call to action beside it. */}
          <a
            href={GITHUB_REPO_URL}
            target="_blank"
            rel="noopener noreferrer"
            aria-label="Kindlast on GitHub"
            className={`rounded-full p-2 transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[#00C9A7] ${link}`}
          >
            <GitHubMark size={20} />
          </a>

          {/* Hidden at the narrowest widths, where the wordmark, the icon and a
              worded pill cannot share 390px without wrapping. The hero repeats
              this call to action anyway. */}
          <a
            href={GITHUB_REPO_URL}
            target="_blank"
            rel="noopener noreferrer"
            className={[
              'hidden whitespace-nowrap rounded-full px-6 py-2.5 text-[15px] font-semibold tracking-[-0.01em] transition-all duration-150 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7] sm:inline-flex',
              overDarkHero
                ? 'bg-white text-[#0D1B2A] hover:bg-white/90'
                : 'bg-[#0D1B2A] text-white hover:bg-[#162537]',
            ].join(' ')}
          >
            Read the source
          </a>
        </div>

      </div>
    </header>

    {/* Routes that do not open on a dark plate would otherwise start beneath
        the fixed bar. */}
    {!hasDarkHero ? <div className="h-[70px]" aria-hidden="true" /> : null}
    </>
  )
}
