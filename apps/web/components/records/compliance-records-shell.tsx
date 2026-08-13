import Link from 'next/link'

import { ConsoleShell } from '@/components/console/console-shell'

/**
 * The "Compliance records" console (ENT-70 / epic ENT-35).
 *
 * A thin wrapper over the shared {@link ConsoleShell} (extracted in ENT-62 so
 * the Agent feed can share the same frame): it pins the rail to Records, sets
 * the header title, and renders the record-type tab bar as the shell's sub-nav.
 * Tabs with a route are links; the rest render disabled until their issues land
 * (Vendors / AI literacy).
 */

export type RecordsTab = 'ropa' | 'ai-systems' | 'vendors' | 'dsar-log' | 'ai-literacy'

const TABS: { key: RecordsTab; label: string; href?: string }[] = [
  { key: 'ropa', label: 'ROPA', href: '/records/ropa' },
  { key: 'ai-systems', label: 'AI systems', href: '/records/ai-systems' },
  { key: 'vendors', label: 'Vendors' },
  { key: 'dsar-log', label: 'DSAR log', href: '/records/dsar' },
  { key: 'ai-literacy', label: 'AI literacy' },
]

function RecordTabs({ activeTab }: { activeTab: RecordsTab }) {
  return (
    <div role="tablist" aria-label="Record types" className="flex flex-wrap gap-2">
      {TABS.map(({ key, label, href }) => {
        const isActive = key === activeTab
        if (href && !isActive) {
          return (
            <Link
              key={key}
              href={href}
              role="tab"
              aria-selected={false}
              className="rounded-xl border border-white/10 px-4 py-2 text-sm font-medium text-zinc-300 transition-colors hover:bg-white/5"
            >
              {label}
            </Link>
          )
        }
        return (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={isActive}
            disabled={!isActive}
            title={isActive || href ? undefined : 'Coming soon'}
            className={`rounded-xl px-4 py-2 text-sm font-medium transition-colors ${
              isActive ? 'bg-white text-zinc-900' : 'border border-white/10 text-zinc-500'
            } ${isActive ? '' : 'cursor-not-allowed opacity-60'}`}
          >
            {label}
          </button>
        )
      })}
    </div>
  )
}

export function ComplianceRecordsShell({
  activeTab,
  children,
}: {
  activeTab: RecordsTab
  children: React.ReactNode
}) {
  return (
    <ConsoleShell
      activeRail="records"
      title="Compliance records"
      subnav={<RecordTabs activeTab={activeTab} />}
    >
      {children}
    </ConsoleShell>
  )
}
