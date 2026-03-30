import { CheckCircle2 } from 'lucide-react'
import { WaitlistForm } from './waitlist-form'

export function Hero() {
  return (
    <section className="relative overflow-hidden bg-[#FAFAF8] min-h-[92dvh] flex items-center">

      {/* Grain texture */}
      <div
        className="noise pointer-events-none absolute inset-0 opacity-[0.028]"
        aria-hidden="true"
      />

      {/* Top radial glow */}
      <div
        className="pointer-events-none absolute inset-0"
        aria-hidden="true"
        style={{
          background: [
            'radial-gradient(ellipse 80% 50% at 50% -8%, oklch(0.90 0.04 143 / 0.6) 0%, transparent 65%)',
            'radial-gradient(ellipse 50% 40% at 85% 85%, oklch(0.93 0.025 143 / 0.28) 0%, transparent 55%)',
          ].join(', '),
        }}
      />

      <div className="relative mx-auto w-full max-w-5xl px-6 py-28 lg:px-8">
        <div className="flex flex-col items-center text-center">

          {/* Eyebrow */}
          <div className="mb-10 inline-flex items-center gap-2.5 rounded-full border border-primary/20 bg-white/80 px-4 py-1.5 shadow-[0_2px_12px_-4px_rgba(0,0,0,0.08)]">
            <span className="relative flex h-2 w-2">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-primary opacity-60" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-primary" />
            </span>
            <span className="text-[12px] font-bold uppercase tracking-[0.16em] text-primary">
              GDPR &amp; EU AI Act · Now in Early Access
            </span>
          </div>

          {/* Headline */}
          <h1 className="text-[4rem] font-black tracking-[-0.04em] leading-[0.88] text-foreground sm:text-[5.5rem] lg:text-[7.5rem] text-balance">
            EU compliance,
            <br />
            <span
              className="text-primary"
              style={{
                background: 'linear-gradient(135deg, oklch(0.655 0.130 143) 0%, oklch(0.60 0.115 148) 100%)',
                WebkitBackgroundClip: 'text',
                WebkitTextFillColor: 'transparent',
                backgroundClip: 'text',
              }}
            >
              finally simple.
            </span>
          </h1>

          {/* Sub */}
          <p className="mt-9 max-w-[500px] text-[1.0625rem] font-medium leading-[1.8] tracking-[-0.01em] text-foreground/50">
            AI-powered GDPR and EU AI Act assessment built for European SMEs.
            Know exactly where you stand — and what to fix — in under 10 minutes.
          </p>

          {/* Waitlist form */}
          <WaitlistForm className="mt-10 w-full max-w-[500px]" />

          {/* Trust row */}
          <div className="mt-8 flex flex-wrap justify-center gap-x-8 gap-y-3">
            {[
              'No legal background needed',
              'Results in under 10 minutes',
              'GDPR + EU AI Act in one scan',
            ].map((item) => (
              <span
                key={item}
                className="flex items-center gap-1.5 text-[13px] font-medium tracking-[-0.005em] text-foreground/38"
              >
                <CheckCircle2 className="h-3.5 w-3.5 text-primary/70 shrink-0" strokeWidth={2.5} />
                {item}
              </span>
            ))}
          </div>

          {/* Social proof blip */}
          <div className="mt-12 flex items-center gap-3 rounded-full border border-black/[0.06] bg-white px-5 py-2.5 shadow-[0_2px_16px_-4px_rgba(0,0,0,0.06)]">
            <div className="flex -space-x-2">
              {['#4ade80', '#60a5fa', '#f472b6', '#fb923c'].map((color, i) => (
                <span
                  key={i}
                  className="h-6 w-6 rounded-full border-2 border-white"
                  style={{ backgroundColor: color }}
                />
              ))}
            </div>
            <span className="text-[13px] font-semibold tracking-[-0.01em] text-foreground/55">
              Join <strong className="text-foreground font-extrabold">200+</strong> EU businesses already on the list
            </span>
          </div>

        </div>
      </div>
    </section>
  )
}
