import { Hero } from '@/components/landing/hero'
import { CapabilitySummary } from '@/components/landing/capability-summary'
import { OpenSource } from '@/components/landing/open-source'
import { Footer } from '@/components/landing/footer'

/**
 * The home page after ENT-190.
 *
 * The site is three routes now. This one has a single job: say what Kindlast
 * is, why the problem is worth solving, and give the reader two honest exits
 * (the pipeline on `/how-it-works`, the surface area on `/features`). The
 * capability detail and the agent architecture both moved out, and the
 * waitlist is gone entirely.
 *
 * Open source stays a section here rather than becoming a fourth route: the
 * full story already lives in the repository, and a marketing page about
 * having a repository is worse than the repository.
 */
export default function LandingPage() {
  return (
    <>
      <Hero />

      {/* Problem, stated in numbers */}
      <section className="py-24 sm:py-32" style={{ backgroundColor: '#F5F4F0' }}>
        <div className="mx-auto max-w-5xl px-6 lg:px-8">

          <div
            className="mb-16 grid grid-cols-2 gap-x-8 gap-y-10 pb-16 sm:grid-cols-3"
            style={{ borderBottom: '1px solid rgba(13,27,42,0.07)' }}
          >
            {[
              { value: '4%', label: 'Max GDPR fine of global annual turnover' },
              { value: '€20M', label: 'Minimum fine threshold, whichever is higher' },
              { value: "Aug '26", label: 'EU AI Act high-risk obligations deadline' },
            ].map((stat) => (
              <div key={stat.value}>
                <p className="text-[3.5rem] font-black leading-none tracking-[-0.04em] text-[#0D1B2A] sm:text-[4.5rem]">
                  {stat.value}
                </p>
                <p
                  className="mt-3 max-w-[200px] text-[1rem] font-medium leading-[1.6] tracking-[-0.005em]"
                  style={{ color: 'rgba(13,27,42,0.42)' }}
                >
                  {stat.label}
                </p>
              </div>
            ))}
          </div>

          <div className="grid items-start gap-12 lg:grid-cols-2">
            <div>
              <p
                className="mb-4 text-[13px] font-bold uppercase tracking-[0.18em]"
                style={{ color: 'rgba(13,27,42,0.3)' }}
              >
                The reality
              </p>
              <h2 className="text-[3rem] font-black leading-none tracking-[-0.035em] text-[#0D1B2A] sm:text-[3.75rem] text-balance">
                Why SMEs struggle
                <br />
                with compliance
              </h2>
            </div>
            <div className="space-y-5 lg:pt-2">
              <p
                className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                Most SMEs lack the legal budget, the in-house expertise, or the
                time to work out where they stand. GDPR has been in force since
                2018 and fines are accelerating. The EU AI Act adds a second wave
                of obligations on top.
              </p>
              <p
                className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]"
                style={{ color: 'rgba(13,27,42,0.5)' }}
              >
                Compliance fails quietly, and it fails on the days nobody
                remembered to check. Kindlast turns that regulatory surface into
                a plain-language action plan your team can act on, without hiring
                a DPO to keep watch.
              </p>
            </div>
          </div>

        </div>
      </section>

      <CapabilitySummary />

      <OpenSource />

      <Footer />
    </>
  )
}
