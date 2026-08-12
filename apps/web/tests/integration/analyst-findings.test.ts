// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { applyFixtureSql, querySql } from './helpers/db-fixture'
import {
  createServiceRoleClient,
  createUserClient,
  isLocalSupabaseReachable,
} from './helpers/supabase'
import { deleteTestUser, signUpTestUser, type TestUser } from './helpers/test-user'

/**
 * ENT-58 — Convert a Watcher signal into a structured finding.
 *
 * The Analyst's foundational pass. The Watcher (ENT-53/55/56/57) emits
 * `watcher_findings` rows — the *signals*. The Analyst reads each open signal
 * and produces exactly one `findings` row (1:1), the user-facing actionable
 * item. ENT-58 is the deterministic structural conversion + traceability links;
 * the richer field *content* lands in later sub-issues:
 *
 *   * ENT-59 — precise obligation/article citation + corpus-cited context
 *   * ENT-60 — plain-language description + specific proposed action
 *   * ENT-61 — severity adjustment + effort-estimate logic
 *
 * Acceptance criteria exercised here:
 *   * One signal → one finding (1:1); re-running never duplicates.
 *   * Finding carries detected, severity, proposed_action,
 *     regulatory_obligation, supporting_context, effort_estimate,
 *     status='pending'.
 *   * Finding links back to the originating signal (watcher_finding_id) and the
 *     obligation (obligation_id / obligation_slug) for traceability.
 *   * Generation is deterministic given the same signal + retrieval context:
 *     converting the same signal twice yields the same finding id and identical
 *     field values.
 *
 * Signals are emitted directly through `emit_watcher_finding` (the same write
 * path the detectors use) rather than by running the Watcher loop. Integration
 * suites run in parallel against the shared local DB and the detectors scan
 * *all* obligations for a profile, so driving the loop would let other suites'
 * concurrently-seeded fixtures leak into this profile's signal set. Emitting
 * directly keeps this profile's signals fully controlled and counts exact.
 */

const supabaseRunning = await isLocalSupabaseReachable()
const PREFIX = '_test_ent58_'
// The DSAR anchor obligation. ENT-59 requires every finding to cite a real
// obligation (obligation_id is NOT NULL), so this is a seeded catalogue fixture.
const DSAR_SLUG = `${PREFIX}dsar`

const SUMMARY =
  'Fixture obligation used by the ENT-58 analyst conversion integration test. Long enough to satisfy the obligations_summary_length check constraint, which requires the summary to be between one hundred and two thousand characters in length.'

const EFFORT = ['minutes', 'hours', 'days']

async function seedObligation(opts: {
  slug: string
  appliesWhen: Record<string, unknown>
  severity?: string
}): Promise<void> {
  await applyFixtureSql(`
    insert into public.obligations
      (slug, title, summary, citation_celex, citation_kind, citation_article,
       applies_when, severity, effective_date)
    values
      ('${opts.slug}', 'Fixture ${opts.slug}', '${SUMMARY}',
       '32016R0679', 'article', 30,
       '${JSON.stringify(opts.appliesWhen)}'::jsonb,
       '${opts.severity ?? 'medium'}',
       null)
    on conflict (slug) do update set
      applies_when = excluded.applies_when,
      severity     = excluded.severity;
  `)
}

interface FindingRow {
  id: string
  profile_id: string
  user_id: string
  watcher_finding_id: string
  obligation_id: string | null
  obligation_slug: string | null
  detected: string
  severity: string
  proposed_action: string
  regulatory_obligation: string | null
  supporting_context: string | null
  effort_estimate: string
  status: string
  metadata: Record<string, unknown>
  [key: string]: unknown
}

const findingsForProfile = (profileId: string) =>
  querySql<FindingRow>(
    `select * from public.findings where profile_id = $1::uuid order by created_at, id`,
    [profileId],
  )

describe.skipIf(!supabaseRunning)('analyst signal→finding conversion (ENT-58)', () => {
  let user: TestUser
  let profileId: string

  // Signal ids captured after emission, keyed by kind.
  let deadlineSignalId: string
  let gapSignalId: string
  let dsarSignalId: string
  let deadlineObligationId: string
  let dsarObligationId: string

  beforeAll(async () => {
    const admin = createServiceRoleClient()
    user = await signUpTestUser(admin)

    const { data: session } = await admin
      .from('onboarding_sessions')
      .insert({ user_id: user.id, status: 'completed' })
      .select('id')
      .single()

    const { data: profile, error } = await admin
      .from('compliance_profiles')
      .insert({
        session_id: session!.id,
        user_id: user.id,
        industry: 'SaaS',
        has_dpo: 'no',
        has_ropa: 'yes',
        transfers_outside_eu: 'no',
      })
      .select('id')
      .single()
    expect(error).toBeNull()
    profileId = profile!.id as string

    // Catalogue obligations the conversion resolves — every signal must cite a
    // real obligation now that obligation_id is NOT NULL (ENT-59), including the
    // DSAR anchor.
    await seedObligation({ slug: `${PREFIX}deadline`, appliesWhen: { role: 'controller' }, severity: 'high' })
    await seedObligation({ slug: `${PREFIX}gap`, appliesWhen: { role: 'controller', requires: ['dpo'] } })
    await seedObligation({ slug: DSAR_SLUG, appliesWhen: { role: 'controller' }, severity: 'high' })

    const obligationId = async (slug: string) =>
      (await querySql<{ id: string }>(`select id from public.obligations where slug = $1`, [slug]))[0].id
    deadlineObligationId = await obligationId(`${PREFIX}deadline`)
    dsarObligationId = await obligationId(DSAR_SLUG)

    // Emit three signals — one per detector shape — through the real write path.
    const emit = async (
      kind: string,
      dedupKey: string,
      title: string,
      detail: string,
      severity: string,
      obligationSlug: string | null,
      metadata: Record<string, unknown>,
    ): Promise<string> => {
      const [{ id }] = await querySql<{ id: string }>(
        `select public.emit_watcher_finding(
           $1::uuid, $2::text, $3::text, $4::text, $5::text, $6::text,
           $7::text, $8::jsonb
         ) as id`,
        [profileId, kind, dedupKey, title, detail, severity, obligationSlug, JSON.stringify(metadata)],
      )
      return id
    }

    deadlineSignalId = await emit(
      'deadline',
      `deadline:obligation:${PREFIX}deadline`,
      `Fixture ${PREFIX}deadline takes effect in 15 days`,
      'This obligation effective date is within 30 days.',
      'high',
      `${PREFIX}deadline`,
      { days_remaining: 15 },
    )
    gapSignalId = await emit(
      'profile_gap',
      `gap:obligation:${PREFIX}gap`,
      `Profile gap: Fixture ${PREFIX}gap`,
      'The corresponding control does not appear to be in place yet.',
      'medium',
      `${PREFIX}gap`,
      { missing: ['dpo'] },
    )
    dsarSignalId = await emit(
      'dsar',
      'dsar:ent58-test',
      'URGENT: DSAR response due in 5 days',
      'A data-subject request from Jane Roe is within 10 days of its deadline.',
      'critical',
      DSAR_SLUG,
      { days_remaining: 5, escalated: true },
    )

    // Convert exactly our three signals. We call the single-signal conversion
    // (not run_analyst_for_profile) on purpose: integration suites run in
    // parallel and the global run_watcher() exercised by other suites can inject
    // extra deadline signals into this profile. Converting our known signals
    // keeps this profile's findings hermetic; the profile-wide loop is exercised
    // — inside an atomic cleanup — in its own test below.
    for (const id of [deadlineSignalId, gapSignalId, dsarSignalId]) {
      await querySql(`select public.analyst_convert_signal($1::uuid)`, [id])
    }
  })

  afterAll(async () => {
    const admin = createServiceRoleClient()
    // Findings are delete-protected (ON DELETE RESTRICT) while they cite an
    // obligation, so clear every finding that references our fixture obligations
    // — regardless of owner, to also sweep rows any interrupted prior run left —
    // before dropping the obligations, then the user.
    await applyFixtureSql(`
      delete from public.findings
      where obligation_id in (select id from public.obligations where slug like '${PREFIX}%');
      delete from public.obligations where slug like '${PREFIX}%';
    `)
    if (user?.id) await deleteTestUser(admin, user.id)
  })

  it('converts each signal into exactly one pending finding (1:1)', async () => {
    const findings = await findingsForProfile(profileId)
    expect(findings).toHaveLength(3) // our three converted signals
    expect(findings.every((f) => f.status === 'pending')).toBe(true)
    // 1:1 — each of our signals maps to exactly one finding.
    expect(findings.map((f) => f.watcher_finding_id).sort()).toEqual(
      [deadlineSignalId, gapSignalId, dsarSignalId].sort(),
    )
    for (const id of [deadlineSignalId, gapSignalId, dsarSignalId]) {
      const rows = await querySql(
        `select 1 from public.findings where watcher_finding_id = $1::uuid`,
        [id],
      )
      expect(rows).toHaveLength(1)
    }
  })

  it('populates the full finding payload and links back to signal + obligation', async () => {
    const [signal] = await querySql<{ title: string; severity: string }>(
      `select title, severity from public.watcher_findings where id = $1::uuid`,
      [deadlineSignalId],
    )

    const findings = await findingsForProfile(profileId)
    const finding = findings.find((f) => f.watcher_finding_id === deadlineSignalId)!

    // Traceability links.
    expect(finding.watcher_finding_id).toBe(deadlineSignalId)
    expect(finding.obligation_id).toBe(deadlineObligationId)
    expect(finding.obligation_slug).toBe(`${PREFIX}deadline`)

    // Carried-over fields.
    expect(finding.detected).toBe(signal.title)
    expect(finding.severity).toBe('high')

    // Every payload field is present and non-empty.
    expect(finding.proposed_action.trim().length).toBeGreaterThan(0)
    expect(finding.regulatory_obligation).toBeTruthy()
    expect(finding.supporting_context).toBe(SUMMARY) // obligation summary baseline
    expect(EFFORT).toContain(finding.effort_estimate)

    // Signal provenance retained in metadata for replay/audit.
    expect(finding.metadata.signal_kind).toBe('deadline')
  })

  it('converts the DSAR signal and links it to its catalogue obligation', async () => {
    const findings = await findingsForProfile(profileId)
    const finding = findings.find((f) => f.watcher_finding_id === dsarSignalId)!

    // ENT-59: obligation_id is non-null and points at the resolved obligation.
    expect(finding.obligation_id).toBe(dsarObligationId)
    expect(finding.obligation_slug).toBe(DSAR_SLUG)
    // regulatory_obligation is the precise citation label (GDPR Art. 30 fixture).
    expect(finding.regulatory_obligation).toBe('GDPR Art. 30')
    expect(finding.severity).toBe('critical') // carried from the escalated signal
    expect(finding.metadata.signal_kind).toBe('dsar')
  })

  it('is idempotent and deterministic — re-running yields the same findings', async () => {
    const snapshot = (rows: FindingRow[]) =>
      JSON.stringify(
        rows.map((r) => ({
          id: r.id,
          watcher_finding_id: r.watcher_finding_id,
          obligation_id: r.obligation_id,
          obligation_slug: r.obligation_slug,
          detected: r.detected,
          severity: r.severity,
          proposed_action: r.proposed_action,
          regulatory_obligation: r.regulatory_obligation,
          supporting_context: r.supporting_context,
          effort_estimate: r.effort_estimate,
          status: r.status,
        })),
      )

    const before = await findingsForProfile(profileId)
    // Re-convert our known signals (idempotent upsert on watcher_finding_id).
    for (const id of [deadlineSignalId, gapSignalId, dsarSignalId]) {
      await querySql(`select public.analyst_convert_signal($1::uuid)`, [id])
    }

    const after = await findingsForProfile(profileId)
    expect(after).toHaveLength(3) // no duplicates
    expect(snapshot(after)).toBe(snapshot(before)) // identical ids + field values
  })

  it('run_analyst_for_profile converts open signals only and skips dismissed ones', async () => {
    // A dismissed signal that maps to a real obligation — it must NOT convert,
    // proving the open-only filter (not merely that an unresolvable signal skips).
    const dismissedId = await querySql<{ id: string }>(
      `select public.emit_watcher_finding(
         $1::uuid, 'deadline', 'ent58:dismissed-only',
         'Dismissed-only signal', 'Should never become a finding', 'low', $2::text
       ) as id`,
      [profileId, `${PREFIX}deadline`],
    ).then((rows) => rows[0].id)
    await querySql(`update public.watcher_findings set status = 'dismissed' where id = $1::uuid`, [
      dismissedId,
    ])

    // Exercise the profile-wide loop, but inside one statement that also strips
    // any findings the concurrently-running global run_watcher() may have
    // injected for foreign obligations. Because both run in a single implicit
    // transaction, those injected rows are never committed — other suites'
    // delete-protected obligations stay clean.
    await querySql(
      `do $$
       begin
         perform public.run_analyst_for_profile('${profileId}'::uuid);
         delete from public.findings
         where profile_id = '${profileId}'::uuid and obligation_slug not like '${PREFIX}%';
       end $$;`,
    )

    const findings = await findingsForProfile(profileId)
    expect(findings.find((f) => f.watcher_finding_id === dismissedId)).toBeUndefined()
    expect(findings).toHaveLength(3) // still just our three
  })

  it('registers run_analyst() as the daily Analyst cron entry point', async () => {
    const jobs = await querySql<{ jobname: string; command: string }>(
      `select jobname, command from cron.job where jobname = 'analyst-daily'`,
    )
    expect(jobs).toHaveLength(1)
    expect(jobs[0].command).toMatch(/run_analyst\(\)/)
  })

  it('exposes findings to the owning user under RLS and hides them from others', async () => {
    const admin = createServiceRoleClient()
    const other = await signUpTestUser(admin)
    try {
      // Owner sees their findings through an RLS-scoped (anon-key) client.
      const ownerClient = await createUserClient(user.email, user.password)
      const owned = await ownerClient.from('findings').select('id').eq('profile_id', profileId)
      expect(owned.error).toBeNull()
      expect((owned.data ?? []).length).toBe(3)

      // A different user sees none of them.
      const otherClient = await createUserClient(other.email, other.password)
      const foreign = await otherClient.from('findings').select('id').eq('profile_id', profileId)
      expect(foreign.error).toBeNull()
      expect(foreign.data ?? []).toHaveLength(0)
    } finally {
      await deleteTestUser(admin, other.id)
    }
  })
})
