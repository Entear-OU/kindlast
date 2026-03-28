import Link from 'next/link'
import { ArrowRight, CheckCircle2 } from 'lucide-react'

export function Hero() {
  return (
    <section className="relative overflow-hidden bg-[#FAFAF8] min-h-[92dvh] flex items-center">
      {/* Background mesh */}
      <div
        className="pointer-events-none absolute inset-0"
        aria-hidden="true"
        style={{
          background: [
            'radial-gradient(ellipse 55% 50% at 75% 30%, oklch(0.93 0.04 147) 0%, transparent 65%)',
            'radial-gradient(ellipse 40% 35% at 20% 80%, oklch(0.95 0.02 147) 0%, transparent 60%)',
          ].join(', '),
        }}
      />

      <div className="relative mx-auto w-full max-w-6xl px-6 py-20 lg:px-8">
        <div className="grid lg:grid-cols-[1fr_440px] gap-10 xl:gap-16 items-center">

          {/* ── Left: copy ── */}
          <div>
            {/* Eyebrow */}
            <div className="mb-8 inline-flex items-center gap-2 rounded-full border border-primary/25 bg-primary/8 px-4 py-2">
              <span className="h-1.5 w-1.5 rounded-full bg-primary" />
              <span className="text-[13px] font-semibold uppercase tracking-[0.14em] text-primary">
                GDPR &amp; EU AI Act · For EU SMEs
              </span>
            </div>

            {/* Headline */}
            <h1 className="text-[3.75rem] font-black tracking-[-0.035em] leading-[0.93] text-foreground sm:text-[5rem] lg:text-[6rem]">
              Your AI copilot
              <br />
              for EU
              <br />
              <span className="text-primary">compliance.</span>
            </h1>

            <p className="mt-8 max-w-[460px] text-[1.1875rem] font-medium leading-[1.72] tracking-[-0.01em] text-foreground/55">
              Navigate GDPR and the EU AI Act with confidence. Answer a short
              questionnaire and get an instant gap analysis — with exactly what
              to fix, in plain English.
            </p>

            {/* CTAs */}
            <div className="mt-10 flex flex-wrap items-center gap-4">
              <Link
                href="/login"
                className="inline-flex items-center gap-2 rounded-full bg-primary px-8 py-4 text-[15px] font-bold tracking-[-0.01em] text-white shadow-[0_4px_20px_-4px_rgba(49,181,77,0.5)] transition-all duration-150 hover:bg-primary/90 active:scale-[0.97]"
              >
                Get started free
                <ArrowRight className="h-4 w-4" />
              </Link>
              <Link
                href="#how-it-works"
                className="text-[15px] font-semibold tracking-[-0.01em] text-foreground/50 hover:text-foreground transition-colors duration-150"
              >
                See how it works
              </Link>
            </div>

            {/* Trust bullets */}
            <div className="mt-10 flex flex-col gap-2.5 sm:flex-row sm:gap-7">
              {[
                'No credit card required',
                'Results in under 10 min',
                'GDPR + AI Act in one scan',
              ].map((item) => (
                <span
                  key={item}
                  className="flex items-center gap-2 text-[14px] font-semibold tracking-[-0.005em] text-foreground/40"
                >
                  <CheckCircle2 className="h-4 w-4 text-primary shrink-0" strokeWidth={2.5} />
                  {item}
                </span>
              ))}
            </div>
          </div>

          {/* ── Right: dashboard card ── */}
          <div className="relative mx-auto w-full max-w-[440px] lg:mx-0">
            {/* Floating EU AI Act badge */}
            <div className="absolute -top-5 -right-2 z-10 rounded-2xl border border-black/[0.07] bg-white px-4 py-3 shadow-[0_8px_28px_-6px_rgba(0,0,0,0.1)]">
              <p className="text-[12px] font-bold uppercase tracking-[0.14em] text-foreground/40">
                EU AI Act
              </p>
              <p className="mt-0.5 text-[17px] font-extrabold tracking-[-0.02em] text-foreground">
                Limited Risk ✓
              </p>
            </div>

            {/* Main card — double bezel */}
            <div className="rounded-[2.25rem] border border-black/[0.07] bg-white p-1.5 shadow-[0_24px_64px_-16px_rgba(0,0,0,0.14)]">
              <div className="rounded-[1.85rem] bg-[#F7F7F5] p-7">

                {/* Card header */}
                <div className="flex items-center justify-between mb-5">
                  <span className="text-[13px] font-bold uppercase tracking-[0.13em] text-foreground/40">
                    Compliance Score
                  </span>
                  <span className="rounded-full bg-amber-50 border border-amber-200/70 px-3 py-1 text-[12px] font-bold uppercase tracking-[0.08em] text-amber-600">
                    Needs Work
                  </span>
                </div>

                {/* Score number */}
                <div className="flex items-end gap-2 mb-6">
                  <span className="text-[4.5rem] font-black tracking-[-0.04em] leading-none text-foreground">
                    63
                  </span>
                  <span className="mb-2 text-[1.25rem] font-semibold text-foreground/30">
                    /100
                  </span>
                </div>

                {/* Progress bars */}
                <div className="space-y-4">
                  {[
                    { label: 'Data Mapping', pct: 82, color: '#31b54d' },
                    { label: 'Consent Mechanisms', pct: 41, color: '#f59e0b' },
                    { label: 'Breach Notification', pct: 28, color: '#ef4444' },
                  ].map((item) => (
                    <div key={item.label}>
                      <div className="mb-1.5 flex justify-between">
                        <span className="text-[13px] font-semibold tracking-[-0.005em] text-foreground/50">
                          {item.label}
                        </span>
                        <span className="text-[13px] font-bold text-foreground/50">
                          {item.pct}%
                        </span>
                      </div>
                      <div className="h-2 rounded-full bg-black/[0.07]">
                        <div
                          className="h-2 rounded-full"
                          style={{ width: `${item.pct}%`, backgroundColor: item.color }}
                        />
                      </div>
                    </div>
                  ))}
                </div>

                {/* Finding pills */}
                <div className="mt-6 flex flex-wrap gap-2">
                  {[
                    { label: '3 Critical', bg: '#fef2f2', border: '#fecaca', color: '#dc2626' },
                    { label: '7 High', bg: '#fffbeb', border: '#fde68a', color: '#d97706' },
                    { label: '12 Medium', bg: '#eff6ff', border: '#bfdbfe', color: '#2563eb' },
                  ].map((b) => (
                    <span
                      key={b.label}
                      className="rounded-full border px-3 py-1 text-[12px] font-bold uppercase tracking-[0.06em]"
                      style={{ background: b.bg, borderColor: b.border, color: b.color }}
                    >
                      {b.label}
                    </span>
                  ))}
                </div>

                {/* Footer row */}
                <div className="mt-6 border-t border-black/[0.06] pt-4 flex items-center justify-between">
                  <span className="text-[13px] font-semibold text-foreground/35">
                    Last scan: today
                  </span>
                  <span className="text-[13px] font-bold text-primary cursor-default">
                    View 22 findings →
                  </span>
                </div>
              </div>
            </div>
          </div>

        </div>
      </div>
    </section>
  )
}
