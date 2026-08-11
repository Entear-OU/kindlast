import { Hero } from '@/components/landing/hero'
import { Features } from '@/components/landing/features'
import { HowItWorks } from '@/components/landing/how-it-works'
import { OpenSource } from '@/components/landing/open-source'
import { Footer } from '@/components/landing/footer'
import { WaitlistForm } from '@/components/landing/waitlist-form'

export default function LandingPage() {
  return (
    <>
      <Hero />

      {/* ── Problem — stats section ── */}
      <section className="py-24 sm:py-32" style={{ backgroundColor: '#F5F4F0' }}>
        <div className="mx-auto max-w-5xl px-6 lg:px-8">

          {/* Stats row */}
          <div
            className="grid grid-cols-2 gap-x-8 gap-y-10 sm:grid-cols-3 pb-16 mb-16"
            style={{ borderBottom: '1px solid rgba(13,27,42,0.07)' }}
          >
            {[
              { value: '4%', label: 'Max GDPR fine of global annual turnover' },
              { value: '€20M', label: 'Minimum fine threshold, whichever is higher' },
              { value: "Aug '26", label: 'EU AI Act high-risk obligations deadline' },
            ].map((stat) => (
              <div key={stat.value}>
                <p className="text-[3.5rem] font-black tracking-[-0.04em] leading-none text-[#0D1B2A] sm:text-[4.5rem]">
                  {stat.value}
                </p>
                <p className="mt-3 text-[1rem] font-medium leading-[1.6] tracking-[-0.005em] max-w-[200px]" style={{ color: 'rgba(13,27,42,0.42)' }}>
                  {stat.label}
                </p>
              </div>
            ))}
          </div>

          {/* Split */}
          <div className="grid lg:grid-cols-2 gap-12 items-start">
            <div>
              <p className="mb-4 text-[13px] font-bold uppercase tracking-[0.18em]" style={{ color: 'rgba(13,27,42,0.3)' }}>
                The reality
              </p>
              <h2 className="text-[3rem] font-black tracking-[-0.035em] leading-none text-[#0D1B2A] sm:text-[3.75rem] text-balance">
                Why SMEs struggle
                <br />
                with compliance
              </h2>
            </div>
            <div className="lg:pt-2 space-y-5">
              <p className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]" style={{ color: 'rgba(13,27,42,0.5)' }}>
                Most SMEs lack the legal budget, in-house expertise, or time to figure out
                where they stand. GDPR has been in force since 2018, and fines are
                accelerating. The EU AI Act now adds a second wave of obligations.
              </p>
              <p className="text-[1.0625rem] font-medium leading-[1.82] tracking-[-0.01em]" style={{ color: 'rgba(13,27,42,0.5)' }}>
                Kindlast turns regulatory complexity into a plain-English action plan your
                team can act on immediately, without hiring a DPO.
              </p>
            </div>
          </div>

        </div>
      </section>

      <HowItWorks />

      <Features />

      <OpenSource />

      {/* ── Waitlist CTA ── */}
      <section
        id="waitlist"
        className="relative overflow-hidden py-28 sm:py-36"
        style={{ backgroundColor: '#0D1B2A' }}
      >
        {/* Grain */}
        <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />

        {/* Teal glow */}
        <div
          className="pointer-events-none absolute inset-0"
          aria-hidden="true"
          style={{
            background: 'radial-gradient(ellipse 65% 55% at 50% 100%, rgba(0,201,167,0.12) 0%, transparent 65%)',
          }}
        />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <div className="flex flex-col items-center text-center">

            <p className="mb-5 text-[13px] font-bold uppercase tracking-[0.2em]" style={{ color: 'rgba(0,201,167,0.7)' }}>
              Early access
            </p>

            <h2 className="text-[3rem] font-black tracking-[-0.035em] leading-none text-white sm:text-[4.5rem] text-balance">
              Be first in line.
              <br />
              Join the waitlist.
            </h2>

            <p className="mx-auto mt-6 max-w-[460px] text-[1.0625rem] font-medium leading-[1.78] tracking-[-0.01em] text-white/60">
              We&apos;re opening early access to a limited number of EU companies.
              Get notified the moment your spot is ready, and help decide what we
              build next.
            </p>

            <WaitlistForm
              className="mt-10"
              size="large"
              variant="inverted"
            />

            <p className="mt-5 text-[14px] font-medium text-white/35">
              No spam, ever. Unsubscribe any time.
            </p>

            <div className="mt-12 flex flex-wrap justify-center gap-x-8 gap-y-3">
              {['Priority access guaranteed', 'Open source from day one', 'Help shape the roadmap'].map((item) => (
                <span key={item} className="text-[14px] font-semibold tracking-[-0.005em] text-white/45">
                  – {item}
                </span>
              ))}
            </div>

          </div>
        </div>
      </section>

      <Footer />
    </>
  )
}
