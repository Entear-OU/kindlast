import Image from 'next/image'
import { GitHubMark } from '@/components/icons/github-mark'
import { GuillocheMark } from '@/components/landing/guilloche-mark'
import { TechnicalGrid } from '@/components/landing/technical-grid'
import { GITHUB_REPO_HANDLE, GITHUB_REPO_URL, LICENSE_SPDX } from '@/lib/links'

/**
 * The open-source section (ENT-175 relicensed the repo to AGPL-3.0).
 *
 * Framing is deliberate. The obvious pitch for an open-source compliance tool
 * is defensive ("audit the auditor"), but that sells compliance as a threat.
 * Our North Star is the opposite: more European companies shipping quickly
 * *because* the regulatory surface is handled. So the argument here is that
 * compliance plumbing is shared infrastructure, not a vendor moat, and every
 * startup re-answering the same GDPR questions is wasted European velocity.
 *
 * Visually this is the one dark object on the warm ground. ENT-190 made it the
 * last section on `/`, so the repo card is now what the home page steps down
 * into before the footer.
 */

const GUARANTEES = [
  {
    label: 'Auditable',
    body: 'Every detector, every citation, and every database access policy is in the repository. Nothing between you and the regulation is a black box.',
  },
  {
    label: 'Self-hostable',
    body: 'Run the whole thing on your own infrastructure. Your processing records never have to leave it, and there is no vendor to add to your ROPA.',
  },
  {
    label: 'Stays open',
    body: 'AGPL section 13: anyone running a modified Kindlast as a service owes their users the corresponding source. The commons stays a commons.',
  },
]

export function OpenSource() {
  return (
    <section
      id="open-source"
      className="relative overflow-hidden py-24 sm:py-32"
      style={{ backgroundColor: '#F5F4F0' }}
    >
      <TechnicalGrid
        labels={[
          { text: '[ AGPL-3.0 ]', top: '11%', right: '2.5%', drift: -60 },
          {
            text: '[ COPYLEFT · NETWORK USE ]',
            top: '38%',
            left: '2%',
            drift: -110,
          },
          { text: '[ SELF-HOSTABLE ]', top: '72%', right: '2%', drift: -80 },
        ]}
      />
      <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
        {/* Full-width intro. The two sections above both use a left/right
            split, so a third would read as a template; giving the headline the
            full measure also stops it fracturing into four ragged lines. */}
        <p
          className="mb-4 text-[13px] font-bold uppercase tracking-[0.18em]"
          style={{ color: 'rgba(13,27,42,0.3)' }}
        >
          Open source
        </p>
        <h2 className="max-w-[15ch] text-[3rem] font-black tracking-[-0.035em] leading-[0.92] text-[#0D1B2A] sm:text-[4.5rem] text-balance">
          Europe shouldn&rsquo;t build this twice.
        </h2>
        <div className="mt-8 max-w-[620px] space-y-5">
          <p
            className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
            style={{ color: 'rgba(13,27,42,0.5)' }}
          >
            Every company shipping software in the EU hits the same GDPR and AI
            Act questions, and almost every one of them answers from scratch.
            That is months of founder time spent re-deriving obligations someone
            already worked out.
          </p>
          <p
            className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
            style={{ color: 'rgba(13,27,42,0.5)' }}
          >
            Kindlast is {LICENSE_SPDX} licensed, so that groundwork is shared
            infrastructure rather than a moat. Read it, run it yourself, or
            improve it for everyone.
          </p>
        </div>

        {/* Repo card: the signature element */}
        <div
          className="relative mt-14 overflow-hidden rounded-3xl"
          style={{ backgroundColor: '#0D1B2A' }}
        >
          {/* Intaglio plate. A macro of engraved security printing, the raised
              ink on a share certificate caught in raking light. It is the same
              idea as the rosette above it but photographic, so the card reads
              as printed stock rather than as a flat panel. Kept very dark so
              the copy on top never has to fight it. */}
          <Image
            src="/imagery/engraving.webp"
            alt=""
            aria-hidden="true"
            fill
            sizes="(max-width: 1024px) 100vw, 1024px"
            className="object-cover opacity-[0.22] mix-blend-luminosity"
          />
          <div
            className="pointer-events-none absolute inset-0"
            aria-hidden="true"
            style={{
              background:
                'linear-gradient(105deg, rgba(13,27,42,0.97) 0%, rgba(13,27,42,0.86) 48%, rgba(13,27,42,0.62) 100%)',
            }}
          />

          {/* Grain, matching the hero treatment */}
          <div
            className="noise pointer-events-none absolute inset-0 opacity-[0.05]"
            aria-hidden="true"
          />

          {/* Guilloche rosette, bled off the top-right corner.
              This is the engraved seal stamped on share certificates and
              passports, which is the right reference for the object that
              carries the licence. It replaces a plain radial gradient: the
              gradient was generic, and this says something.
              A CSS background rather than an `<img>` because it is purely
              decorative, which also keeps it out of the accessibility tree
              without needing an empty alt. */}
          <GuillocheMark className="pointer-events-none absolute -right-24 -top-28 h-[420px] w-[420px] opacity-[0.09] sm:-right-16 sm:h-[520px] sm:w-[520px]" />

          <div className="relative p-8 sm:p-12">
            {/* Repo identity */}
            <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
              <GitHubMark size={26} className="text-white shrink-0" />
              <span className="text-[1.25rem] font-extrabold tracking-[-0.03em] text-white sm:text-[1.5rem]">
                {GITHUB_REPO_HANDLE}
              </span>
              <span
                className="rounded-full px-3 py-1 text-[12px] font-bold uppercase tracking-[0.1em]"
                style={{
                  color: '#00C9A7',
                  border: '1px solid rgba(0,201,167,0.35)',
                  backgroundColor: 'rgba(0,201,167,0.08)',
                }}
              >
                {LICENSE_SPDX}
              </span>
            </div>

            {/* Guarantees */}
            <dl
              className="mt-10 grid gap-x-10 gap-y-8 sm:grid-cols-3 pt-10"
              style={{ borderTop: '1px solid rgba(255,255,255,0.08)' }}
            >
              {GUARANTEES.map((item) => (
                <div key={item.label}>
                  <dt className="text-[15px] font-extrabold tracking-[-0.01em] text-white">
                    {item.label}
                  </dt>
                  <dd className="mt-2.5 text-[15px] font-medium leading-[1.7] tracking-[-0.005em] text-white/45">
                    {item.body}
                  </dd>
                </div>
              ))}
            </dl>

            {/* CTA */}
            <div className="mt-10">
              <a
                href={GITHUB_REPO_URL}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-2.5 rounded-full bg-white px-6 py-3 text-[15px] font-semibold tracking-[-0.01em] text-[#0D1B2A] transition-all duration-150 hover:bg-white/90 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
              >
                <GitHubMark size={17} />
                Read the source
              </a>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
