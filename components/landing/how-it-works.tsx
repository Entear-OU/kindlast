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
    <section id="how-it-works" className="bg-[#FAFAF8] py-24 sm:py-32">
      <div className="mx-auto max-w-6xl px-6 lg:px-8">

        {/* Header */}
        <div className="mb-14 flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <p className="mb-3 text-[13px] font-bold uppercase tracking-[0.16em] text-primary">
              The process
            </p>
            <h2 className="text-[2.75rem] font-black tracking-[-0.03em] leading-[1.0] text-foreground sm:text-[3.5rem] text-balance">
              From zero to action plan
              <br />
              in under 10 minutes
            </h2>
          </div>
          <p className="max-w-[300px] text-[1.0625rem] font-medium leading-[1.65] tracking-[-0.01em] text-foreground/50 sm:text-right">
            No legal expertise required. Just answer honestly.
          </p>
        </div>

        {/* Steps — 3-col cards */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {steps.map((step) => (
            <div
              key={step.number}
              className="group relative flex flex-col rounded-[1.6rem] border border-black/[0.06] bg-white p-8 transition-all duration-200 hover:shadow-[0_16px_40px_-12px_rgba(0,0,0,0.08)] hover:-translate-y-0.5"
            >
              {/* Step number — large ghost */}
              <span className="mb-4 block text-[3.5rem] font-black tracking-[-0.04em] leading-none text-primary/10 select-none">
                {step.number}
              </span>

              {/* Icon */}
              <div className="mb-5 flex h-12 w-12 items-center justify-center rounded-xl bg-primary/8 border border-primary/15">
                <step.icon className="h-6 w-6 text-primary" strokeWidth={2} />
              </div>

              <h3 className="text-[1.125rem] font-extrabold tracking-[-0.02em] text-foreground">
                {step.title}
              </h3>
              <p className="mt-3 text-[1rem] font-medium leading-[1.65] tracking-[-0.005em] text-foreground/50">
                {step.description}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
