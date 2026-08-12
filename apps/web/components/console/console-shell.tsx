import {
  Bell,
  Copy,
  CreditCard,
  LayoutGrid,
  MessageSquare,
  Settings,
  Shield,
  type LucideIcon,
} from 'lucide-react'
import Link from 'next/link'

import { loadConsoleAgentStatus } from '@/lib/console/agent-status'
import { agentStatusLabel } from '@/lib/dashboard/activity'
import { createClient } from '@/lib/supabase/server'

/**
 * The shared "console" frame (epic ENT-35 / ENT-39).
 *
 * The dark, agentic shell every authed surface lives in: a left icon rail, a
 * header with the agent-status pill, an optional sub-nav slot, and a scrollable
 * body. Extracted from the records console (ENT-70) so the Agent feed (ENT-62)
 * can share the exact frame — the rail's Dashboard, Records, Alerts and Billing
 * destinations are real links, so the founder can move between the posture
 * dashboard (`/dashboard`), the registers (`/records/ropa`), the feed (`/feed`)
 * and the upgrade page (`/billing`). The rest of the rail is disabled until its
 * surfaces land (Documents, Assistant).
 *
 * ENT-155: the header pill and the Alerts unread dot are derived from real
 * state (last Watcher run + pending findings) rather than hard-coded — so a
 * fresh account no longer falsely reads "Agent running · last scan 4 min ago".
 *
 * Deliberately a dark surface independent of the global theme — distinct from
 * the eggshell marketing pages.
 */

export type ConsoleRail = 'dashboard' | 'records' | 'alerts' | 'billing' | 'settings'

type RailItem = {
  key: ConsoleRail | 'documents' | 'assistant'
  Icon: LucideIcon
  label: string
  href?: string
}

const RAIL: RailItem[] = [
  { key: 'records', Icon: Shield, label: 'Records', href: '/records/ropa' },
  { key: 'dashboard', Icon: LayoutGrid, label: 'Dashboard', href: '/dashboard' },
  { key: 'alerts', Icon: Bell, label: 'Alerts', href: '/feed' },
  { key: 'documents', Icon: Copy, label: 'Documents' },
  { key: 'assistant', Icon: MessageSquare, label: 'Assistant' },
  { key: 'billing', Icon: CreditCard, label: 'Billing', href: '/billing' },
  { key: 'settings', Icon: Settings, label: 'Settings', href: '/settings' },
]

export async function ConsoleShell({
  activeRail,
  title,
  subnav,
  children,
}: {
  activeRail: ConsoleRail
  title: string
  subnav?: React.ReactNode
  children: React.ReactNode
}) {
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  // Every consumer is an authed page (each redirects on no user), but stay
  // defensive: with no user, show the honest "hasn't run yet" pill and no dot.
  const { agentStatus, hasPendingFindings } = user
    ? await loadConsoleAgentStatus(supabase, user.id)
    : { agentStatus: agentStatusLabel(null), hasPendingFindings: false }

  return (
    <div className="flex min-h-0 flex-1 bg-zinc-950 text-zinc-100">
      {/* Left icon rail */}
      <nav
        aria-label="Console"
        className="flex w-16 flex-col items-center gap-2 border-r border-white/5 py-4"
      >
        {RAIL.map(({ key, Icon, label, href }) => {
          const active = key === activeRail
          const base = `relative flex h-10 w-10 items-center justify-center rounded-xl transition-colors ${
            active
              ? 'bg-[#00C9A7] text-zinc-950'
              : 'text-zinc-500 hover:bg-white/5 hover:text-zinc-200'
          }`
          // The Alerts dot reflects real pending findings, not a hard-coded flag.
          const dot =
            key === 'alerts' && hasPendingFindings ? (
              <span className="absolute right-2 top-2 h-1.5 w-1.5 rounded-full bg-rose-500" />
            ) : null

          if (href && !active) {
            return (
              <Link key={key} href={href} title={label} aria-label={label} className={base}>
                <Icon size={18} aria-hidden="true" />
                {dot}
              </Link>
            )
          }
          return (
            <span
              key={key}
              title={href ? label : 'Coming soon'}
              aria-label={label}
              aria-current={active ? 'page' : undefined}
              className={`${base}${!href && !active ? ' cursor-not-allowed opacity-60' : ''}`}
            >
              <Icon size={18} aria-hidden="true" />
              {dot}
            </span>
          )
        })}
      </nav>

      {/* Main column */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center justify-between px-6 py-4">
          <h1 className="text-base font-semibold tracking-tight">{title}</h1>
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-2 rounded-full border border-white/10 px-3 py-1 text-xs text-zinc-300">
              <span
                className={`h-1.5 w-1.5 rounded-full ${
                  agentStatus.running ? 'bg-emerald-400' : 'bg-amber-400'
                }`}
                aria-hidden="true"
              />
              {agentStatus.text}
            </span>
          </div>
        </header>

        {subnav ? <div className="px-6">{subnav}</div> : null}

        <div className="min-h-0 flex-1 overflow-auto px-6 py-5">{children}</div>
      </div>
    </div>
  )
}
