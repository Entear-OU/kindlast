import { Shield, Brain, FileText, BarChart3 } from 'lucide-react'

const features = [
  {
    title: 'GDPR Assessment',
    description:
      'Comprehensive gap analysis against the full scope of the General Data Protection Regulation. Get specific findings tied to GDPR articles.',
    icon: Shield,
  },
  {
    title: 'AI Act Classification',
    description:
      'Classify your AI systems by EU AI Act risk tier — unacceptable, high, limited, or minimal — with obligations and deadlines.',
    icon: Brain,
  },
  {
    title: 'PDF Export',
    description:
      'Generate audit-ready PDF compliance reports with your score, findings, and recommendations. Share with stakeholders or auditors.',
    icon: FileText,
  },
  {
    title: 'Compliance Score',
    description:
      'Get a clear 0-100 compliance score with color-coded risk levels. Track your progress as you resolve findings.',
    icon: BarChart3,
  },
]

export function Features() {
  return (
    <section className="bg-muted/50 py-24 sm:py-32">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-2xl text-center">
          <h2 className="text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Everything you need for EU compliance
          </h2>
          <p className="mt-4 text-lg leading-8 text-muted-foreground">
            From assessment to action plan, Kindlast covers both GDPR and the
            EU AI Act in one platform.
          </p>
        </div>
        <div className="mx-auto mt-16 grid max-w-5xl grid-cols-1 gap-8 sm:grid-cols-2">
          {features.map((feature) => (
            <div
              key={feature.title}
              className="relative rounded-xl border bg-card p-8 shadow-sm"
            >
              <div className="mb-4 flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10">
                <feature.icon className="h-5 w-5 text-primary" />
              </div>
              <h3 className="text-lg font-semibold text-card-foreground">
                {feature.title}
              </h3>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">
                {feature.description}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
