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
      'AI evaluates your business against the full scope of GDPR: lawful bases, consent, data subject rights, and breach notification procedures.',
    detail:
      'Article-level findings tied to specific GDPR provisions, so you know exactly where the gaps are and what to fix first.',
    icon: Shield,
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
    colSpan: 'md:col-span-1',
    accent: false,
  },
  {
    id: 'recs',
    title: 'Actionable Recommendations',
    description:
      'Every finding comes with a prioritized, step-by-step recommendation: specific actions mapped to your business context.',
    detail: '',
    icon: Lightbulb,
    colSpan: 'md:col-span-1',
    accent: false,
  },
  {
    id: 'aiact',
    title: 'EU AI Act Classification',
    description:
      'Classify your AI systems by risk tier: unacceptable, high, limited, or minimal. Understand your obligations before enforcement.',
    detail:
      'Guidance on documentation requirements, conformity assessments, and compliance deadlines for your specific tier.',
    icon: Brain,
    colSpan: 'md:col-span-2',
    accent: true,
  },
  {
    id: 'pdf',
    title: 'Audit-Ready PDF Reports',
    description:
      'Professional compliance reports with your score, all findings, and recommendations, ready to share with auditors or investors.',
    detail: '',
    icon: FileText,
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
    <div className="mt-6 rounded-xl p-4 space-y-3.5" style={{ border: '1px solid rgba(13,27,42,0.06)', backgroundColor: '#EDECEA' }}>
      {items.map((item) => (
        <div key={item.label} className="flex items-center gap-3">
          <span
            className="h-2 w-2 shrink-0 rounded-full"
            style={{ backgroundColor: item.ok ? '#00C9A7' : '#f87171' }}
          />
          <div className="flex-1">
            <div className="mb-1.5 flex justify-between">
              <span className="text-[14px] font-semibold tracking-[-0.005em]" style={{ color: 'rgba(13,27,42,0.5)' }}>
                {item.label}
              </span>
              <span className="text-[14px] font-bold tabular-nums" style={{ color: 'rgba(13,27,42,0.4)' }}>
                {item.pct}%
              </span>
            </div>
            <div className="h-1.5 rounded-full" style={{ backgroundColor: 'rgba(13,27,42,0.07)' }}>
              <div
                className="h-1.5 rounded-full"
                style={{
                  width: `${item.pct}%`,
                  backgroundColor: item.ok ? '#00C9A7' : '#f87171',
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
    { label: 'Minimal Risk', color: '#00C9A7', active: false },
  ]
  return (
    <div className="mt-6 grid grid-cols-2 gap-2">
      {tiers.map((tier) => (
        <div
          key={tier.label}
          className="flex items-center gap-2.5 rounded-xl px-3.5 py-3 transition-all"
          style={{
            border: tier.active ? '1px solid rgba(217,119,6,0.4)' : '1px solid rgba(13,27,42,0.06)',
            backgroundColor: tier.active ? '#FFFBEB' : '#EDECEA',
          }}
        >
          <span className="h-2 w-2 shrink-0 rounded-full" style={{ backgroundColor: tier.color }} />
          <span
            className="text-[13px] font-bold tracking-[-0.005em]"
            style={{ color: tier.active ? '#92400e' : 'rgba(13,27,42,0.42)' }}
          >
            {tier.label}
          </span>
          {tier.active && (
            <span className="ml-auto text-[12px] font-bold uppercase tracking-[0.06em]" style={{ color: '#b45309' }}>
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
    <section id="features" className="py-24 sm:py-32" style={{ backgroundColor: '#F5F4F0' }}>
      <div className="mx-auto max-w-5xl px-6 lg:px-8">

        {/* Header */}
        <div className="mb-16 text-center">
          <p className="mb-4 text-[13px] font-bold uppercase tracking-[0.18em]" style={{ color: '#00C9A7' }}>
            Platform capabilities
          </p>
          <h2 className="text-[3rem] font-black tracking-[-0.035em] leading-none text-[#0D1B2A] sm:text-[3.75rem] text-balance">
            Everything you need
            <br />
            for EU compliance
          </h2>
          <p className="mx-auto mt-6 max-w-[400px] text-[1.0625rem] font-medium leading-[1.72] tracking-[-0.01em]" style={{ color: 'rgba(13,27,42,0.45)' }}>
            GDPR &amp; AI Act in a single workflow. No consultants required.
          </p>
        </div>

        {/* Bento grid */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3.5">
          {features.map((f) => (
            <div
              key={f.id}
              className={[
                'group relative flex flex-col rounded-[1.5rem] bg-white p-8',
                'transition-all duration-300 hover:shadow-[0_20px_48px_-12px_rgba(13,27,42,0.1)] hover:-translate-y-1',
                f.colSpan,
              ].join(' ')}
              style={{ border: '1px solid rgba(13,27,42,0.06)' }}
            >
              {/* Icon */}
              <div
                className="mb-5 flex h-11 w-11 items-center justify-center rounded-xl transition-colors duration-200"
                style={{ border: '1px solid rgba(13,27,42,0.06)', backgroundColor: '#EDECEA' }}
              >
                <f.icon
                  className="h-5 w-5 transition-colors duration-200"
                  style={{ color: 'rgba(13,27,42,0.5)' }}
                  strokeWidth={1.75}
                />
              </div>

              <h3 className="text-[1.1875rem] font-extrabold tracking-[-0.02em] text-[#0D1B2A]">
                {f.title}
              </h3>
              <p className="mt-2.5 text-[1.0625rem] font-medium leading-[1.72] tracking-[-0.005em]" style={{ color: 'rgba(13,27,42,0.48)' }}>
                {f.description}
              </p>

              {f.accent && f.detail && (
                <p className="mt-3 text-[0.9375rem] font-medium leading-[1.65] tracking-[-0.005em] border-t pt-3" style={{ color: 'rgba(13,27,42,0.3)', borderColor: 'rgba(13,27,42,0.05)' }}>
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
