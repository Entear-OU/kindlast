import { notFound, redirect } from 'next/navigation'

import { getPlan } from '@/lib/billing/plan'
import { ConsoleShell } from '@/components/console/console-shell'
import { FindingDetailView } from '@/components/feed/finding-detail'
import { loadFindingDetail, loadSupportingChunks } from '@/lib/feed/finding-detail'
import { createClient } from '@/lib/supabase/server'

/**
 * Finding detail (ENT-64) — the founder expands one finding to see the Analyst's
 * full reasoning and the supporting regulatory sources, so a decision can be
 * justified to an auditor or counsel later.
 *
 * This is the shareable permalink (`/feed/[id]`); it stays RLS-gated — the detail
 * loader scopes to the owner and `notFound()` covers both a missing row and one
 * owned by someone else, so a shared link leaks nothing across tenants.
 */
export default async function FindingDetailPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = await params
  const supabase = await createClient()
  const {
    data: { user },
  } = await supabase.auth.getUser()
  if (!user) {
    redirect('/login')
  }

  const finding = await loadFindingDetail(supabase, user.id, id)
  if (!finding) {
    notFound()
  }

  const [chunks, plan] = await Promise.all([
    loadSupportingChunks(supabase, id),
    getPlan(user.id),
  ])

  return (
    <ConsoleShell activeRail="alerts" title="Finding detail">
      <FindingDetailView finding={finding} chunks={chunks} plan={plan} />
    </ConsoleShell>
  )
}
