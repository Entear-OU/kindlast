import Image from 'next/image'
import Link from 'next/link'
import { GitHubMark } from '@/components/icons/github-mark'
import { HeroLattice } from '@/components/landing/hero-lattice'
import { GITHUB_REPO_URL, LICENSE_SPDX } from '@/lib/links'

/**
 * ENT-190 took the waitlist off the hero, and the sign-in link with it. There
 * is nothing to sign up for yet, and a form asking for an email in exchange for
 * a promise reads badly next to a public AGPL repository. The repository is the
 * claim, so reading it is the only ask.
 *
 * The hero is now a dark full-bleed plate rather than the flat warm ground it
 * used to be. The image is an aerial of a northern European city grid at blue
 * hour, abstracted to pure lattice: it is the North Star stated visually, many
 * companies building in the same regulatory space, and it gives the page the
 * depth that a flat colour section never had.
 *
 * Legibility is handled by a two-stop scrim rather than by dimming the whole
 * image, so the lattice stays readable at the edges while the type sits on a
 * genuinely dark ground.
 */
export function Hero() {
  return (
    // `100dvh`, not `100vh`: on mobile Safari and Chrome the static `vh` unit
    // measures against the viewport with the URL bar collapsed, so the plate
    // overflows and the page shifts as the bar hides. `dvh` tracks it.
    <section className="relative flex min-h-[100dvh] items-center overflow-hidden bg-[#0A141F]">
      {/* Plate. `priority` because this is the LCP element. */}
      <Image
        src="/imagery/hero-grid.webp"
        alt=""
        aria-hidden="true"
        fill
        priority
        sizes="100vw"
        className="object-cover"
      />

      {/* Scrim: heavier through the centre column where the type sits, so the
          lattice survives at the edges instead of being flattened everywhere. */}
      <div
        className="pointer-events-none absolute inset-0"
        aria-hidden="true"
        style={{
          background: [
            'linear-gradient(180deg, rgba(10,20,31,0.72) 0%, rgba(10,20,31,0.55) 45%, rgba(10,20,31,0.88) 100%)',
            'radial-gradient(ellipse 62% 55% at 50% 48%, rgba(10,20,31,0.78) 0%, transparent 75%)',
          ].join(', '),
        }}
      />

      {/* WebGL lattice, layered between the plate and the copy. Purely
          additive: it fades in only once a frame has drawn, so no WebGL, a
          lost context, or reduced motion all just leave the plate. */}
      <HeroLattice />

      {/* Teal lift, bottom right, picking up the accent from the mark */}
      <div
        className="pointer-events-none absolute inset-0"
        aria-hidden="true"
        style={{
          background:
            'radial-gradient(ellipse 45% 40% at 88% 92%, rgba(0,201,167,0.13) 0%, transparent 60%)',
        }}
      />

      <div
        className="noise pointer-events-none absolute inset-0 opacity-[0.05]"
        aria-hidden="true"
      />

      <div className="relative mx-auto w-full max-w-5xl px-6 py-28 lg:px-8">
        <div className="flex flex-col items-center text-center">
          {/* Eyebrow */}
          <div className="mb-10 inline-flex items-center gap-2.5 rounded-full border border-white/15 bg-white/[0.06] px-4 py-2 backdrop-blur-sm">
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-[#00C9A7] opacity-70" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-[#00C9A7]" />
            </span>
            <span className="text-[13px] font-bold uppercase tracking-[0.14em] text-white/70">
              GDPR &amp; EU AI Act &middot; Open source, {LICENSE_SPDX}
            </span>
          </div>

          {/* Headline */}
          <h1 className="text-[3.25rem] font-black leading-[0.88] tracking-[-0.04em] text-white sm:text-[5rem] lg:text-[7rem] text-balance">
            EU compliance,
            <br />
            <span style={{ color: '#00C9A7' }}>finally simple.</span>
          </h1>

          {/* Sub */}
          <p className="mt-9 max-w-[560px] text-[1.1875rem] font-medium leading-[1.78] tracking-[-0.01em] text-white/60">
            Four agents watch your GDPR and EU AI Act obligations, turn what
            they find into one specific action, and wait for your approval
            before anything changes.
          </p>

          {/* Action. ENT-189 gave the site something to DO for the first time
              since the waitlist went, so the readiness check takes the primary
              slot and reading the source moves beside it. Both are honest asks
              and neither is a form: the check needs no account and sends
              nothing, and the repository is still the product claim. */}
          <div className="mt-11 flex flex-col items-center gap-4 sm:flex-row">
            <Link
              href="/readiness"
              className="inline-flex items-center gap-2.5 rounded-full bg-[#00C9A7] px-7 py-3.5 text-[16px] font-semibold tracking-[-0.01em] text-[#052E28] transition-all duration-150 hover:bg-[#2AD6BA] active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-white"
            >
              Check where you stand
            </Link>
            <a
              href={GITHUB_REPO_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2.5 rounded-full border border-white/25 px-7 py-3.5 text-[16px] font-semibold tracking-[-0.01em] text-white transition-all duration-150 hover:bg-white/10 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
            >
              <GitHubMark size={18} />
              Read the source
            </a>
          </div>

          <p className="mt-5 text-[14px] font-medium tracking-[-0.005em] text-white/40">
            No account, and your answers never leave the page.
          </p>

          {/* What the shape of the product actually guarantees */}
          <div className="mt-14 flex flex-wrap justify-center gap-x-10 gap-y-3">
            {[
              'Runs on a schedule, not on a reminder',
              'Never acts without your approval',
              'Self-hostable, end to end',
            ].map((item) => (
              <span
                key={item}
                className="flex items-center gap-2.5 text-[15px] font-medium tracking-[-0.005em] text-white/45"
              >
                <span
                  aria-hidden="true"
                  className="h-1.5 w-1.5 shrink-0 rounded-full"
                  style={{ backgroundColor: '#00C9A7' }}
                />
                {item}
              </span>
            ))}
          </div>
        </div>
      </div>
    </section>
  )
}
