import {
  Bell,
  Copy,
  LayoutGrid,
  MessageSquare,
  Settings,
  Shield,
} from 'lucide-react'
import Link from 'next/link'

/**
 * The "Compliance records" console shell (ENT-70 / epic ENT-35).
 *
 * The dark, agentic frame the register tabs live in: a left icon rail, a header
 * with the agent-status pill, and the tab bar across the record types. Tabs with
 * a route are links; the rest render disabled until their issues land (Vendors /
 * AI literacy are later).
 *
 * Deliberately a dark surface independent of the global theme — this is the
 * product's "console", a distinct visual key from the eggshell marketing pages.
 */

export type RecordsTab = 'ropa' | 'ai-systems' | 'vendors' | 'dsar-log' | 'ai-literacy'

const TABS: { key: RecordsTab; label: string; href?: string }[] = [
  { key: 'ropa', label: 'ROPA', href: '/records/ropa' },
  { key: 'ai-systems', label: 'AI systems', href: '/records/ai-systems' },
  { key: 'vendors', label: 'Vendors' },
  { key: 'dsar-log', label: 'DSAR log', href: '/records/dsar' },
  { key: 'ai-literacy', label: 'AI literacy' },
]

const RAIL_ICONS = [
  { Icon: Shield, label: 'Records', active: true, badge: false },
  { Icon: LayoutGrid, label: 'Dashboard', active: false, badge: false },
  { Icon: Bell, label: 'Alerts', active: false, badge: true },
  { Icon: Copy, label: 'Documents', active: false, badge: false },
  { Icon: MessageSquare, label: 'Assistant', active: false, badge: false },
  { Icon: Settings, label: 'Settings', active: false, badge: false },
]

export function ComplianceRecordsShell({
  activeTab,
  lastScanLabel = 'last scan 4 min ago',
  children,
}: {
  activeTab: RecordsTab
  lastScanLabel?: string
  children: React.ReactNode
}) {
  return (
    <div className="flex min-h-0 flex-1 bg-zinc-950 text-zinc-100">
      {/* Left icon rail */}
      <nav
        aria-label="Console"
        className="flex w-16 flex-col items-center gap-2 border-r border-white/5 py-4"
      >
        {RAIL_ICONS.map(({ Icon, label, active, badge }) => (
          <span
            key={label}
            title={label}
            aria-label={label}
            aria-current={active ? 'page' : undefined}
            className={`relative flex h-10 w-10 items-center justify-center rounded-xl transition-colors ${
              active
                ? 'bg-[#00C9A7] text-zinc-950'
                : 'text-zinc-500 hover:bg-white/5 hover:text-zinc-200'
            }`}
          >
            <Icon size={18} aria-hidden="true" />
            {badge && (
              <span className="absolute right-2 top-2 h-1.5 w-1.5 rounded-full bg-rose-500" />
            )}
          </span>
        ))}
      </nav>

      {/* Main column */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center justify-between px-6 py-4">
          <h1 className="text-base font-semibold tracking-tight">Compliance records</h1>
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-2 rounded-full border border-white/10 px-3 py-1 text-xs text-zinc-300">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" aria-hidden="true" />
              Agent running · {lastScanLabel}
            </span>
          </div>
        </header>

        <div className="px-6">
          {/* Tab bar */}
          <div role="tablist" aria-label="Record types" className="flex flex-wrap gap-2">
            {TABS.map(({ key, label, href }) => {
              const isActive = key === activeTab
              const activeClass = isActive
                ? 'bg-white text-zinc-900'
                : 'border border-white/10 text-zinc-300 hover:bg-white/5'
              if (href && !isActive) {
                return (
                  <Link
                    key={key}
                    href={href}
                    role="tab"
                    aria-selected={false}
                    className={`rounded-xl px-4 py-2 text-sm font-medium transition-colors ${activeClass}`}
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
        </div>

        <div className="min-h-0 flex-1 overflow-auto px-6 py-5">{children}</div>
      </div>
    </div>
  )
}
