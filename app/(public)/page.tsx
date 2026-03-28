import { Hero } from '@/components/landing/hero'
import { Features } from '@/components/landing/features'
import { HowItWorks } from '@/components/landing/how-it-works'
import { Footer } from '@/components/landing/footer'
import Link from 'next/link'
import { ArrowRight } from 'lucide-react'

export default function LandingPage() {
  return (
    <>
      <Hero />

      {/* ── Problem — dark stats section ── */}
      <section className="bg-foreground py-24 sm:py-32">
        <div className="mx-auto max-w-6xl px-6 lg:px-8">

          {/* Stats row */}
          <div className="mb-16 grid grid-cols-2 gap-8 sm:grid-cols-3 border-b border-white/[0.07] pb-16">
            {[
              { value: '4%', label: 'Max GDPR fine of global annual turnover' },
              { value: '€20M', label: 'Minimum fine threshold, whichever is higher' },
              { value: 'Aug \'26', label: 'EU AI Act high-risk obligations deadline' },
            ].map((stat) => (
              <div key={stat.value}>
                <p className="text-[3rem] font-black tracking-[-0.04em] leading-none text-primary sm:text-[3.75rem]">
                  {stat.value}
                </p>
                <p className="mt-3 text-[0.9375rem] font-medium leading-[1.55] tracking-[-0.005em] text-white/40 max-w-[180px]">
                  {stat.label}
                </p>
              </div>
            ))}
          </div>

          {/* Split — heading + body */}
          <div className="grid lg:grid-cols-2 gap-12 items-start">
            <div>
              <p className="mb-3 text-[13px] font-bold uppercase tracking-[0.16em] text-white/30">
                The reality
              </p>
              <h2 className="text-[2.75rem] font-black tracking-[-0.03em] leading-[1.0] text-white sm:text-[3.5rem] text-balance">
                Why SMEs struggle
                <br />
                with compliance
              </h2>
            </div>
            <div className="lg:pt-3">
              <p className="text-[1.125rem] font-medium leading-[1.8] tracking-[-0.01em] text-white/50">
                Most SMEs lack the legal budget, in-house expertise, or time to
                figure out where they stand. GDPR has been in force since 2018 —
                and fines are accelerating. The EU AI Act now adds a second wave
                of obligations.
              </p>
              <p className="mt-5 text-[1.125rem] font-medium leading-[1.8] tracking-[-0.01em] text-white/50">
                Kindlast turns regulatory complexity into a plain-English action
                plan your team can act on immediately — without hiring a DPO.
              </p>
              <Link
                href="/login"
                className="mt-9 inline-flex items-center gap-2 rounded-full bg-primary px-8 py-4 text-[15px] font-bold tracking-[-0.01em] text-white shadow-[0_4px_20px_-4px_rgba(49,181,77,0.4)] transition-all duration-150 hover:bg-primary/90 active:scale-[0.97]"
              >
                Start your free assessment
                <ArrowRight className="h-4 w-4" />
              </Link>
            </div>
          </div>
        </div>
      </section>

      <HowItWorks />

      <Features />

      {/* ── Pricing preview ── */}
      <section className="bg-[#FAFAF8] py-24 sm:py-32">
        <div className="mx-auto max-w-6xl px-6 lg:px-8">
          <div className="overflow-hidden rounded-[2rem] border border-black/[0.06] bg-white">
            <div className="grid lg:grid-cols-2">
              {/* Left */}
              <div className="p-10 sm:p-14">
                <p className="mb-3 text-[13px] font-bold uppercase tracking-[0.16em] text-primary">
                  Pricing
                </p>
                <h2 className="text-[2.75rem] font-black tracking-[-0.03em] leading-[1.0] text-foreground sm:text-[3.25rem] text-balance">
                  Simple,
                  <br />
                  transparent
                  <br />
                  pricing
                </h2>
              </div>

              {/* Right */}
              <div className="flex flex-col justify-center border-t border-black/[0.05] p-10 sm:border-t-0 sm:border-l sm:p-14">
                <p className="text-[1.125rem] font-medium leading-[1.75] tracking-[-0.01em] text-foreground/55 mb-9">
                  Start for free with full GDPR gap analysis and your compliance
                  score. Upgrade when you need full findings, EU AI Act
                  classification, and audit-ready PDF exports.
                </p>
                <div className="flex flex-wrap items-center gap-4">
                  <Link
                    href="/pricing"
                    className="inline-flex items-center gap-2 rounded-full bg-primary px-8 py-4 text-[15px] font-bold tracking-[-0.01em] text-white shadow-[0_4px_20px_-4px_rgba(49,181,77,0.4)] transition-all duration-150 hover:bg-primary/90 active:scale-[0.97]"
                  >
                    View pricing
                    <ArrowRight className="h-4 w-4" />
                  </Link>
                  <Link
                    href="/login"
                    className="text-[15px] font-semibold tracking-[-0.01em] text-foreground/40 hover:text-foreground transition-colors duration-150"
                  >
                    Start free →
                  </Link>
                </div>
              </div>
            </div>
          </div>
        </div>
      </section>

      {/* ── Final CTA ── */}
      <section className="bg-primary py-24 sm:py-32">
        <div className="mx-auto max-w-6xl px-6 text-center lg:px-8">
          <h2 className="text-[2.75rem] font-black tracking-[-0.035em] leading-[1.0] text-white sm:text-[4rem] text-balance">
            Get your AI copilot
            <br />
            for EU compliance.
          </h2>
          <p className="mx-auto mt-7 max-w-[480px] text-[1.125rem] font-medium leading-[1.72] tracking-[-0.01em] text-white/65">
            Stop navigating GDPR and the EU AI Act alone. Get a clear action
            plan in under 10 minutes — no legal background required.
          </p>
          <div className="mt-10 flex flex-wrap items-center justify-center gap-4">
            <Link
              href="/login"
              className="rounded-full bg-white px-9 py-4 text-[15px] font-bold tracking-[-0.01em] text-primary shadow-lg transition-all duration-150 hover:bg-white/90 active:scale-[0.97]"
            >
              Get started free — it&apos;s free
            </Link>
            <Link
              href="/pricing"
              className="rounded-full border border-white/25 px-9 py-4 text-[15px] font-bold tracking-[-0.01em] text-white transition-all duration-150 hover:border-white/50 hover:bg-white/10 active:scale-[0.97]"
            >
              View pricing
            </Link>
          </div>
        </div>
      </section>

      <Footer />
    </>
  )
}
