import { ClipboardList, Cpu, FileCheck } from 'lucide-react'

const steps = [
  {
    number: '01',
    title: 'Answer questions',
    description:
      'Complete a short onboarding wizard about your business, data processing activities, and current compliance measures.',
    icon: ClipboardList,
  },
  {
    number: '02',
    title: 'AI analyzes',
    description:
      'Our AI engine evaluates your responses against GDPR requirements and EU AI Act risk tiers, identifying every gap.',
    icon: Cpu,
  },
  {
    number: '03',
    title: 'Get your action plan',
    description:
      'Receive a scored report with specific findings, GDPR article references, and step-by-step recommendations to fix them.',
    icon: FileCheck,
  },
]

export function HowItWorks() {
  return (
    <section id="how-it-works" className="relative overflow-hidden bg-foreground py-24 sm:py-32">

      {/* Grain */}
      <div className="noise pointer-events-none absolute inset-0 opacity-[0.04]" aria-hidden="true" />

      {/* Subtle glow */}
      <div
        className="pointer-events-none absolute inset-0"
        aria-hidden="true"
        style={{
          background: 'radial-gradient(ellipse 60% 50% at 50% 100%, oklch(0.683 0.185 147 / 0.08) 0%, transparent 65%)',
        }}
      />

      <div className="relative mx-auto max-w-5xl px-6 lg:px-8">

        {/* Header */}
        <div className="mb-16 text-center">
          <p className="mb-4 text-[12px] font-bold uppercase tracking-[0.18em] text-primary/70">
            The process
          </p>
          <h2 className="text-[2.5rem] font-black tracking-[-0.035em] leading-[1.0] text-white sm:text-[3.25rem] text-balance">
            From zero to action plan
            <br />
            in under 10 minutes
          </h2>
          <p className="mx-auto mt-5 max-w-[340px] text-[0.9375rem] font-medium leading-[1.72] tracking-[-0.01em] text-white/38">
            No legal expertise required. Just answer honestly.
          </p>
        </div>

        {/* Steps */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {steps.map((step, i) => (
            <div
              key={step.number}
              className="group relative flex flex-col rounded-[1.5rem] border border-white/[0.07] bg-white/[0.04] p-8 transition-all duration-200 hover:bg-white/[0.07] hover:border-white/[0.12]"
            >
              {/* Connector line (between cards on desktop) */}
              {i < steps.length - 1 && (
                <span className="hidden md:block absolute -right-[9px] top-[4.5rem] h-px w-4 bg-white/10 z-10" />
              )}

              {/* Ghost number */}
              <span className="mb-4 block text-[3.5rem] font-black tracking-[-0.04em] leading-none text-white/[0.07] select-none">
                {step.number}
              </span>

              {/* Icon */}
              <div className="mb-5 flex h-11 w-11 items-center justify-center rounded-xl bg-primary/12 border border-primary/20">
                <step.icon className="h-5 w-5 text-primary" strokeWidth={2} />
              </div>

              <h3 className="text-[1.0625rem] font-extrabold tracking-[-0.02em] text-white">
                {step.title}
              </h3>
              <p className="mt-3 text-[0.9375rem] font-medium leading-[1.72] tracking-[-0.005em] text-white/40">
                {step.description}
              </p>
            </div>
          ))}
        </div>

      </div>
    </section>
  )
}
