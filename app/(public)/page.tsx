import { Hero } from '@/components/landing/hero'
import { Features } from '@/components/landing/features'
import { HowItWorks } from '@/components/landing/how-it-works'
import { Footer } from '@/components/landing/footer'
import { WaitlistForm } from '@/components/landing/waitlist-form'

export default function LandingPage() {
  return (
    <>
      <Hero />

      {/* ── Problem — stats section ── */}
      <section className="relative overflow-hidden bg-[#FAFAF8] py-24 sm:py-32">
        <div className="mx-auto max-w-5xl px-6 lg:px-8">

          {/* Stats row */}
          <div className="grid grid-cols-2 gap-x-8 gap-y-10 sm:grid-cols-3 border-b border-black/[0.06] pb-16 mb-16">
            {[
              { value: '4%', label: 'Max GDPR fine of global annual turnover' },
              { value: '€20M', label: 'Minimum fine threshold, whichever is higher' },
              { value: "Aug '26", label: 'EU AI Act high-risk obligations deadline' },
            ].map((stat) => (
              <div key={stat.value}>
                <p className="text-[3rem] font-black tracking-[-0.04em] leading-none text-foreground sm:text-[3.75rem]">
                  {stat.value}
                </p>
                <p className="mt-3 text-[0.875rem] font-medium leading-[1.6] tracking-[-0.005em] text-foreground/40 max-w-[180px]">
                  {stat.label}
                </p>
              </div>
            ))}
          </div>

          {/* Split — heading + body */}
          <div className="grid lg:grid-cols-2 gap-12 items-start">
            <div>
              <p className="mb-4 text-[12px] font-bold uppercase tracking-[0.18em] text-foreground/28">
                The reality
              </p>
              <h2 className="text-[2.5rem] font-black tracking-[-0.035em] leading-[1.0] text-foreground sm:text-[3.25rem] text-balance">
                Why SMEs struggle
                <br />
                with compliance
              </h2>
            </div>
            <div className="lg:pt-2 space-y-5">
              <p className="text-[1rem] font-medium leading-[1.82] tracking-[-0.01em] text-foreground/50">
                Most SMEs lack the legal budget, in-house expertise, or time to figure out
                where they stand. GDPR has been in force since 2018 — and fines are
                accelerating. The EU AI Act now adds a second wave of obligations.
              </p>
              <p className="text-[1rem] font-medium leading-[1.82] tracking-[-0.01em] text-foreground/50">
                Kindlast turns regulatory complexity into a plain-English action plan your
                team can act on immediately — without hiring a DPO.
              </p>
            </div>
          </div>

        </div>
      </section>

      <HowItWorks />

      <Features />

      {/* ── Waitlist CTA ── */}
      <section
        id="waitlist"
        className="relative overflow-hidden py-28 sm:py-36"
        style={{ background: 'linear-gradient(135deg, oklch(0.655 0.130 143) 0%, oklch(0.60 0.115 148) 100%)' }}
      >
        {/* Grain */}
        <div className="noise pointer-events-none absolute inset-0 opacity-[0.05]" aria-hidden="true" />

        {/* Soft radial highlight */}
        <div
          className="pointer-events-none absolute inset-0"
          aria-hidden="true"
          style={{
            background: 'radial-gradient(ellipse 70% 60% at 50% 0%, rgba(255,255,255,0.12) 0%, transparent 65%)',
          }}
        />

        <div className="relative mx-auto max-w-5xl px-6 lg:px-8">
          <div className="flex flex-col items-center text-center">

            <p className="mb-5 text-[12px] font-bold uppercase tracking-[0.2em] text-white/60">
              Early access
            </p>

            <h2 className="text-[2.5rem] font-black tracking-[-0.035em] leading-[1.0] text-white sm:text-[4rem] text-balance">
              Be first in line.
              <br />
              Join the waitlist.
            </h2>

            <p className="mx-auto mt-6 max-w-[440px] text-[1rem] font-medium leading-[1.78] tracking-[-0.01em] text-white/65">
              We&apos;re opening early access to a limited number of EU SMEs. Get
              notified the moment your spot is ready — and lock in founding-member
              pricing.
            </p>

            <WaitlistForm
              className="mt-10 w-full max-w-[500px]"
              size="large"
              placeholder="Your work email address"
              variant="inverted"
            />

            <p className="mt-5 text-[13px] font-medium text-white/38">
              No spam, ever. Unsubscribe any time.
            </p>

            {/* Mini trust row */}
            <div className="mt-12 flex flex-wrap justify-center gap-x-8 gap-y-3">
              {[
                'Free to join',
                'Priority access guaranteed',
                'Founding-member pricing',
              ].map((item) => (
                <span
                  key={item}
                  className="text-[13px] font-semibold tracking-[-0.005em] text-white/55"
                >
                  — {item}
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
