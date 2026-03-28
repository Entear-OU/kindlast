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
    id: 'gdpr',
    title: 'GDPR Gap Analysis',
    description:
      'AI evaluates your business against the full scope of GDPR — lawful bases, consent, data subject rights, and breach notification procedures.',
    detail:
      'Article-level findings tied to specific GDPR provisions, so you know exactly where the gaps are and what to fix first.',
    icon: Shield,
    premium: false,
    colSpan: 'md:col-span-2',
    accent: true,
  },
  {
    id: 'score',
    title: 'Compliance Score',
    description:
      'A clear 0–100 score with color-coded risk levels. Single view of where you stand, with progress over time.',
    detail: '',
    icon: BarChart3,
    premium: false,
    colSpan: 'md:col-span-1',
    accent: false,
  },
  {
    id: 'recs',
    title: 'Actionable Recommendations',
    description:
      'Every finding comes with a prioritized, step-by-step recommendation — specific actions mapped to your business context.',
    detail: '',
    icon: Lightbulb,
    premium: false,
    colSpan: 'md:col-span-1',
    accent: false,
  },
  {
    id: 'aiact',
    title: 'EU AI Act Classification',
    description:
      'Classify your AI systems by risk tier — unacceptable, high, limited, or minimal. Understand your obligations before enforcement.',
    detail:
      'Guidance on documentation requirements, conformity assessments, and compliance deadlines for your specific tier.',
    icon: Brain,
    premium: true,
    colSpan: 'md:col-span-2',
    accent: true,
  },
  {
    id: 'pdf',
    title: 'Audit-Ready PDF Reports',
    description:
      'Professional compliance reports with your score, all findings, and recommendations — ready to share with auditors or investors.',
    detail: '',
    icon: FileText,
    premium: true,
    colSpan: 'md:col-span-1',
    accent: false,
  },
  {
    id: 'privacy',
    title: 'Privacy-First Architecture',
    description:
      'Data never leaves the secure pipeline. Server-side processing, row-level security, no training on your inputs.',
    detail: '',
    icon: Lock,
    premium: false,
    colSpan: 'md:col-span-1',
    accent: false,
  },
]

function GdprMiniVisual() {
  const items = [
    { label: 'Lawful Basis', pct: 75, ok: true },
    { label: 'Data Mapping', pct: 91, ok: true },
    { label: 'DPA Agreements', pct: 40, ok: false },
    { label: 'Breach Procedure', pct: 22, ok: false },
  ]
  return (
    <div className="mt-6 rounded-xl border border-black/[0.06] bg-[#F7F7F5] p-4 space-y-3">
      {items.map((item) => (
        <div key={item.label} className="flex items-center gap-3">
          <span
            className={`h-2 w-2 shrink-0 rounded-full ${item.ok ? 'bg-primary' : 'bg-red-400'}`}
          />
          <div className="flex-1">
            <div className="mb-1.5 flex justify-between">
              <span className="text-[13px] font-semibold tracking-[-0.005em] text-foreground/50">
                {item.label}
              </span>
              <span className="text-[13px] font-bold text-foreground/40">{item.pct}%</span>
            </div>
            <div className="h-1.5 rounded-full bg-black/[0.07]">
              <div
                className="h-1.5 rounded-full transition-all"
                style={{
                  width: `${item.pct}%`,
                  backgroundColor: item.ok ? '#31b54d' : '#f87171',
                }}
              />
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}

function AiActMiniVisual() {
  const tiers = [
    { label: 'Unacceptable', color: '#dc2626', active: false },
    { label: 'High Risk', color: '#ea580c', active: false },
    { label: 'Limited Risk', color: '#d97706', active: true },
    { label: 'Minimal Risk', color: '#31b54d', active: false },
  ]
  return (
    <div className="mt-6 grid grid-cols-2 gap-2">
      {tiers.map((tier) => (
        <div
          key={tier.label}
          className={[
            'flex items-center gap-2.5 rounded-xl border px-3.5 py-3 transition-all',
            tier.active
              ? 'border-amber-300/50 bg-amber-50'
              : 'border-black/[0.06] bg-[#F7F7F5]',
          ].join(' ')}
        >
          <span
            className="h-2.5 w-2.5 shrink-0 rounded-full"
            style={{ backgroundColor: tier.color }}
          />
          <span
            className={[
              'text-[12px] font-bold tracking-[-0.005em]',
              tier.active ? 'text-amber-700' : 'text-foreground/40',
            ].join(' ')}
          >
            {tier.label}
          </span>
          {tier.active && (
            <span className="ml-auto text-[11px] font-bold text-amber-600 uppercase tracking-[0.06em]">
              You
            </span>
          )}
        </div>
      ))}
    </div>
  )
}

export function Features() {
  return (
    <section id="features" className="bg-white py-24 sm:py-32">
      <div className="mx-auto max-w-6xl px-6 lg:px-8">

        {/* Header */}
        <div className="mb-14">
          <p className="mb-3 text-[13px] font-bold uppercase tracking-[0.16em] text-primary">
            Platform capabilities
          </p>
          <div className="flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
            <h2 className="text-[2.75rem] font-black tracking-[-0.03em] leading-[1.0] text-foreground sm:text-[3.5rem] text-balance">
              Everything you need
              <br />
              for EU compliance
            </h2>
            <p className="max-w-[280px] text-[1.0625rem] font-medium leading-[1.65] tracking-[-0.01em] text-foreground/50 sm:text-right">
              GDPR &amp; AI Act in a single workflow. No consultants required.
            </p>
          </div>
        </div>

        {/* Bento grid */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {features.map((f) => (
            <div
              key={f.id}
              className={[
                'group relative flex flex-col rounded-[1.6rem] border border-black/[0.06] bg-[#FAFAF8] p-8',
                'transition-all duration-200 hover:shadow-[0_16px_40px_-12px_rgba(0,0,0,0.07)] hover:-translate-y-0.5',
                f.colSpan,
              ].join(' ')}
            >
              {/* Premium badge */}
              {f.premium && (
                <span className="absolute right-5 top-5 rounded-full border border-primary/20 bg-primary/8 px-3 py-1 text-[12px] font-bold uppercase tracking-[0.1em] text-primary">
                  Premium
                </span>
              )}

              {/* Icon */}
              <div className="mb-5 flex h-12 w-12 items-center justify-center rounded-xl border border-black/[0.07] bg-white shadow-sm">
                <f.icon className="h-6 w-6 text-foreground/70" strokeWidth={1.75} />
              </div>

              <h3 className="text-[1.125rem] font-extrabold tracking-[-0.02em] text-foreground">
                {f.title}
              </h3>
              <p className="mt-2.5 text-[1rem] font-medium leading-[1.65] tracking-[-0.005em] text-foreground/50">
                {f.description}
              </p>

              {f.accent && f.detail && (
                <p className="mt-3 text-[0.9375rem] font-medium leading-[1.6] tracking-[-0.005em] text-foreground/35 border-t border-black/[0.06] pt-3">
                  {f.detail}
                </p>
              )}

              {f.id === 'gdpr' && <GdprMiniVisual />}
              {f.id === 'aiact' && <AiActMiniVisual />}
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}
