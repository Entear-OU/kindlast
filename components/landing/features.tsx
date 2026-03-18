import {
  Shield,
  Brain,
  FileText,
  BarChart3,
  Lightbulb,
  Lock,
} from 'lucide-react'

const features = [
  {
    title: 'GDPR Gap Analysis',
    description:
      'Our AI engine evaluates your business against the full scope of the General Data Protection Regulation — from lawful bases and consent mechanisms to data subject rights and breach notification procedures.',
    detail:
      'Receive article-level findings tied to specific GDPR provisions, so you know exactly where your gaps are and what to fix first.',
    icon: Shield,
    premium: false,
  },
  {
    title: 'Compliance Score & Dashboard',
    description:
      'Get a clear 0–100 compliance score with color-coded risk levels (critical, high, medium, low). Your dashboard gives you a single view of where you stand.',
    detail:
      'Track your progress over time as you resolve findings. See at a glance which areas need attention and which are already covered.',
    icon: BarChart3,
    premium: false,
  },
  {
    title: 'Actionable Recommendations',
    description:
      'Every finding comes with a prioritized, step-by-step recommendation — not generic advice, but specific actions mapped to your business context.',
    detail:
      'Each recommendation references the relevant GDPR article and severity level, so your team or DPO can triage and act immediately.',
    icon: Lightbulb,
    premium: false,
  },
  {
    title: 'EU AI Act Classification',
    description:
      'Classify your AI systems by EU AI Act risk tier — unacceptable, high, limited, or minimal. Understand your obligations before enforcement begins.',
    detail:
      'Get clear guidance on documentation requirements, conformity assessments, and compliance deadlines specific to your risk tier.',
    icon: Brain,
    premium: true,
  },
  {
    title: 'Audit-Ready PDF Reports',
    description:
      'Generate professional compliance reports with your score, all findings, and recommendations in a clean, structured PDF format.',
    detail:
      'Share with auditors, board members, or investors. Each report includes timestamps, methodology notes, and GDPR article references.',
    icon: FileText,
    premium: true,
  },
  {
    title: 'Privacy-First Architecture',
    description:
      'Your data never leaves the secure pipeline. All processing happens server-side with row-level security, and we never train on your inputs.',
    detail:
      'Built on Supabase with PostgreSQL RLS policies, so each user\'s data is isolated by design — not just by application logic.',
    icon: Lock,
    premium: false,
  },
]

export function Features() {
  return (
    <section className="bg-muted/50 py-24 sm:py-32">
      <div className="mx-auto max-w-7xl px-6 lg:px-8">
        <div className="mx-auto max-w-3xl text-center">
          <p className="text-sm font-semibold uppercase tracking-widest text-primary">
            Platform Capabilities
          </p>
          <h2 className="mt-2 text-3xl font-bold tracking-tight text-foreground sm:text-4xl">
            Everything you need for EU compliance
          </h2>
          <p className="mt-4 text-lg leading-8 text-muted-foreground">
            From initial assessment to audit-ready reports, Kindlast gives you a
            complete toolkit for GDPR and EU AI Act compliance — purpose-built
            for SMEs that need clarity without the consulting price tag.
          </p>
        </div>

        <div className="mx-auto mt-20 grid max-w-6xl grid-cols-1 gap-10 sm:grid-cols-2 lg:grid-cols-3">
          {features.map((feature) => (
            <div
              key={feature.title}
              className="relative flex flex-col rounded-xl border bg-card p-8 shadow-sm transition-shadow hover:shadow-md"
            >
              {feature.premium && (
                <span className="absolute right-4 top-4 rounded-full bg-primary/10 px-2.5 py-0.5 text-xs font-medium text-primary">
                  Premium
                </span>
              )}
              <div className="mb-5 flex h-11 w-11 items-center justify-center rounded-lg bg-primary/10">
                <feature.icon className="h-5 w-5 text-primary" />
              </div>
              <h3 className="text-lg font-semibold text-card-foreground">
                {feature.title}
              </h3>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">
                {feature.description}
              </p>
              <p className="mt-3 text-sm leading-6 text-muted-foreground/80">
                {feature.detail}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
