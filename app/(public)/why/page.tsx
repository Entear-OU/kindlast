import type { Metadata } from 'next'
import Image from 'next/image'
import Link from 'next/link'
import { Footer } from '@/components/landing/footer'
import { GitHubMark } from '@/components/icons/github-mark'
import { TechnicalGrid } from '@/components/landing/technical-grid'
import { PRINCIPLES } from '@/components/landing/principles'
import { GITHUB_REPO_URL } from '@/lib/links'

export const metadata: Metadata = {
  title: 'Why Kindlast exists',
  description:
    'Europe should be able to build quickly without trading away the rights the rules exist to protect. The principles this product is held to, and the mechanism behind each one.',
}

/**
 * The why.
 *
 * The rest of the site says what Kindlast is and how it works. Neither answers
 * the question a founder actually asks first, which is why anyone should build
 * this at all. The argument is a refusal of the usual trade-off: compliance is
 * treated as a brake on shipping, and the two are assumed to be in tension.
 * They are not, and treating them as though they were is what produces both
 * slow companies and bad data practice.
 *
 * The principles are here rather than on a policy page nobody opens, because
 * they are the load-bearing claim: a compliance product that could not meet the
 * standard it measures you against would not be worth running. Each one is
 * paired with a mechanism that exists in the repository, so the section is
 * checkable rather than aspirational.
 */
export default function WhyPage() {
  return (
    <>
      {/* Thesis */}
      <section className="relative overflow-hidden bg-[#0A141F] pt-32 pb-24 sm:pt-40 sm:pb-32">
        <Image
          src="/imagery/colonnade.webp"
          alt=""
          aria-hidden="true"
          fill
          priority
          sizes="100vw"
          className="object-cover"
        />
        <div
          className="pointer-events-none absolute inset-0"
          aria-hidden="true"
          style={{
            background: [
              'linear-gradient(180deg, rgba(10,20,31,0.82) 0%, rgba(10,20,31,0.66) 40%, rgba(10,20,31,0.95) 100%)',
              'linear-gradient(90deg, rgba(10,20,31,0.90) 0%, rgba(10,20,31,0.38) 72%, transparent 100%)',
            ].join(', '),
          }}
        />
        <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <p className="mb-5 text-[13px] font-bold uppercase tracking-[0.18em] text-white/40">
            Why we build this
          </p>

          <h1 className="max-w-[17ch] text-[2.75rem] font-black leading-[0.94] tracking-[-0.04em] text-white sm:text-[4.25rem] text-balance">
            Move fast.
            <br />
            <span style={{ color: '#00C9A7' }}>Respect people anyway.</span>
          </h1>

          <div className="mt-9 max-w-[620px] space-y-5">
            <p className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/55">
              The received wisdom is that these pull against each other: that
              European rules are a tax on building, and that moving quickly means
              cutting corners on the rights the rules exist to protect. Founders
              are told to pick one.
            </p>
            <p className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/55">
              We think that framing is the actual problem. It produces companies
              that ship slowly because they are afraid, and companies that ship
              quickly because they never looked. Neither is good for the people
              whose data is in the system.
            </p>
            <p className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/55">
              What is missing is not more regulation or less of it. It is
              infrastructure: something that holds the regulatory surface for you,
              continuously, so that respecting people costs a founder attention
              rather than months. That is the whole reason this exists.
            </p>
          </div>
        </div>
      </section>

      {/* What that means in practice */}
      <section className="relative overflow-hidden py-24 sm:py-32" style={{ backgroundColor: '#F5F4F0' }}>
        <TechnicalGrid
          labels={[
            { text: '[ VELOCITY ]', top: '12%', right: '2.5%', drift: -70 },
            { text: '[ RIGHTS ]', top: '58%', left: '2%', drift: -110 },
          ]}
        />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <div className="grid gap-12 lg:grid-cols-2">
            <div>
              <p
                className="mb-4 text-[13px] font-bold uppercase tracking-[0.18em]"
                style={{ color: 'rgba(13,27,42,0.3)' }}
              >
                The bet
              </p>
              <h2 className="max-w-[16ch] text-[2.5rem] font-black leading-[0.95] tracking-[-0.035em] text-[#0D1B2A] sm:text-[3.25rem] text-balance">
                Compliance as infrastructure, not as a brake.
              </h2>
            </div>
            <div className="space-y-5 lg:pt-2">
              <p
                className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                Nobody rebuilds TLS to ship a web app. It is handled, it is
                shared, and the work happens on top of it. The regulatory surface
                should sit in the same place: solved once, in the open, and
                maintained by everyone who depends on it.
              </p>
              <p
                className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                That is why the whole engine is public under AGPL-3.0 rather than
                sold as a black box. A moat around compliance plumbing would slow
                down exactly the companies we want moving faster.
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* Principles */}
      <section className="relative overflow-hidden py-24 sm:py-32" style={{ backgroundColor: '#0D1B2A' }}>
        <TechnicalGrid
          tone="light"
          labels={[
            { text: '[ UNESCO · OECD · EU AI ACT ]', top: '7%', right: '2.5%', drift: -60 },
            { text: '[ HELD TO, NOT ASPIRED TO ]', top: '92%', left: '2%', drift: -40 },
          ]}
        />
        <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <p className="mb-5 text-[13px] font-bold uppercase tracking-[0.18em] text-white/35">
            Responsible AI
          </p>
          <h2 className="max-w-[20ch] text-[2.5rem] font-black leading-[0.95] tracking-[-0.035em] text-white sm:text-[3.25rem] text-balance">
            The standard we measure you against, applied to us.
          </h2>
          <p className="mt-7 max-w-[620px] text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em] text-white/45">
            These are the widely agreed principles for responsible AI. Each one
            here is paired with the mechanism behind it, because a principle with
            nothing enforcing it is a poster. Every mechanism named is in the
            repository and you can go and check it.
          </p>

          <dl className="mt-16 grid gap-x-12 gap-y-12 sm:grid-cols-2">
            {PRINCIPLES.map((p) => (
              <div key={p.name}>
                {/* Sized properly and given room: at 34px these read as
                    smudges rather than as drawings. The generated SVGs also
                    had the navy plate baked in, which made each one a dark
                    box on the section rather than a glyph on it. */}
                <Image
                  src={`/icons/${p.icon}.svg`}
                  alt=""
                  aria-hidden="true"
                  width={64}
                  height={64}
                  className="mb-6 opacity-90"
                />
                <dt className="text-[1.0625rem] font-extrabold tracking-[-0.02em] text-white">
                  {p.name}
                </dt>
                <dd className="mt-2.5 text-[15px] font-medium leading-[1.7] tracking-[-0.005em] text-white/45">
                  {p.statement}
                </dd>
                <dd
                  className="mt-4 border-l pl-4 text-[14px] font-medium leading-[1.65] tracking-[-0.005em] text-white/38"
                  style={{ borderColor: 'rgba(0,201,167,0.4)' }}
                >
                  {p.mechanism}
                </dd>
              </div>
            ))}
          </dl>

          <div className="mt-16 flex flex-wrap items-center gap-x-7 gap-y-4">
            <a
              href={GITHUB_REPO_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2.5 rounded-full bg-white px-6 py-3 text-[15px] font-semibold tracking-[-0.01em] text-[#0D1B2A] transition-all duration-150 hover:bg-white/90 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
            >
              <GitHubMark size={17} />
              Check the mechanisms
            </a>
            <Link
              href="/how-it-works"
              className="text-[15px] font-semibold tracking-[-0.01em] text-white/45 underline underline-offset-4 transition-colors duration-150 hover:text-white focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-[#00C9A7]"
            >
              See the pipeline that enforces them
            </Link>
          </div>
        </div>
      </section>

      <Footer />
    </>
  )
}
