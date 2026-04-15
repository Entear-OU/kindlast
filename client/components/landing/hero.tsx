import { CheckCircle2 } from 'lucide-react'
import { WaitlistForm } from './waitlist-form'

export function Hero() {
  return (
    <section className="relative overflow-hidden bg-[#F5F4F0] min-h-[92dvh] flex items-center">

      {/* Grain texture */}
      <div className="noise pointer-events-none absolute inset-0 opacity-[0.03]" aria-hidden="true" />

      {/* Teal radial glow — top */}
      <div
        className="pointer-events-none absolute inset-0"
        aria-hidden="true"
        style={{
          background: [
            'radial-gradient(ellipse 75% 45% at 50% -5%, rgba(0,201,167,0.14) 0%, transparent 65%)',
            'radial-gradient(ellipse 40% 35% at 88% 88%, rgba(0,201,167,0.07) 0%, transparent 55%)',
          ].join(', '),
        }}
      />

      <div className="relative mx-auto w-full max-w-5xl px-6 py-28 lg:px-8">
        <div className="flex flex-col items-center text-center">

          {/* Eyebrow */}
          <div className="mb-10 inline-flex items-center gap-2.5 rounded-full border border-[#00C9A7]/30 bg-white/70 px-4 py-2 shadow-[0_2px_12px_-4px_rgba(0,0,0,0.07)]">
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-[#00C9A7] opacity-70" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-[#00C9A7]" />
            </span>
            <span className="text-[13px] font-bold uppercase tracking-[0.14em] text-[#0D1B2A]/70">
              GDPR &amp; EU AI Act · Now in Early Access
            </span>
          </div>

          {/* Headline */}
          <h1 className="text-[4.5rem] font-black tracking-[-0.04em] leading-[0.88] text-[#0D1B2A] sm:text-[6rem] lg:text-[8rem] text-balance">
            EU compliance,
            <br />
            <span style={{ color: '#00C9A7' }}>
              finally simple.
            </span>
          </h1>

          {/* Sub */}
          <p className="mt-9 max-w-[540px] text-[1.1875rem] font-medium leading-[1.78] tracking-[-0.01em] text-[#0D1B2A]/50">
            AI-powered GDPR and EU AI Act assessment built for European SMEs.
            Know exactly where you stand — and what to fix — in under 10 minutes.
          </p>

          {/* Waitlist form */}
          <WaitlistForm className="mt-10 w-full max-w-[520px]" />

          {/* Trust row */}
          <div className="mt-8 flex flex-wrap justify-center gap-x-8 gap-y-3">
            {[
              'No legal background needed',
              'Results in under 10 minutes',
              'GDPR + EU AI Act in one scan',
            ].map((item) => (
              <span
                key={item}
                className="flex items-center gap-2 text-[15px] font-medium tracking-[-0.005em] text-[#0D1B2A]/38"
              >
                <CheckCircle2 className="h-4 w-4 shrink-0" style={{ color: '#00C9A7' }} strokeWidth={2.5} />
                {item}
              </span>
            ))}
          </div>

        </div>
      </div>
    </section>
  )
}
