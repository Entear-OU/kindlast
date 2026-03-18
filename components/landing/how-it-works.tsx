import { ClipboardList, Cpu, FileCheck } from 'lucide-react'

const steps = [
  {
    number: '1',
    title: 'Answer questions',
    description:
      'Complete a short onboarding wizard about your business, data processing, and current compliance measures.',
    icon: ClipboardList,
  },
  {
    number: '2',
    title: 'AI analyzes',
    description:
      'Our AI engine evaluates your responses against GDPR requirements and EU AI Act risk tiers, identifying gaps and risks.',
    icon: Cpu,
  },
  {
    number: '3',
    title: 'Get actionable report',
    description:
      'Receive a scored compliance report with specific findings, GDPR article references, and step-by-step recommendations.',
    icon: FileCheck,
  },
]

export function HowItWorks() {
  return (
    <section id="how-it-works" className="bg-background py-24 sm:py-32">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            How it works
          </h2>
          <p className="mt-4 text-lg leading-8 text-muted-foreground">
            Get from zero to a compliance action plan in under 10 minutes.
          </p>
        </div>
        <div className="mx-auto mt-16 grid max-w-5xl grid-cols-1 gap-12 sm:grid-cols-3">
          {steps.map((step) => (
            <div key={step.number} className="text-center">
              <div className="mx-auto mb-6 flex h-14 w-14 items-center justify-center rounded-full bg-primary text-primary-foreground text-xl font-bold">
                {step.number}
              </div>
              <div className="mb-3 flex justify-center">
                <step.icon className="h-8 w-8 text-muted-foreground" />
              </div>
              <h3 className="text-lg font-semibold text-foreground">
                {step.title}
              </h3>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">
                {step.description}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
