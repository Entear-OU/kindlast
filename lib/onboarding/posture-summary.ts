import type { ComplianceProfile } from './extraction'

/**
 * Posture summary projection (ENT-46).
 *
 * Pure function: takes the structured `ComplianceProfile` (ENT-45) and
 * returns the green/red lists + one draft "top action" the founder can
 * approve inline at the end of onboarding.
 *
 * Why a deterministic projection instead of a second LLM pass:
 *
 *   * The AC requires <10s from final user turn to summary visible, and
 *     extraction already burns one `generateObject` round-trip. A second
 *     model call would risk the budget for no listening reward.
 *   * The UI has structured slots (covered key+label, missing key+label,
 *     draft finding with severity + regulation reference). A rule-based
 *     projection is easier to assert in tests than coaxing structured
 *     output from a free-form completion.
 *   * The mapping from profile fields to "covered vs missing" is
 *     mechanical — there's no compliance judgement here that benefits
 *     from a model's nuance. The Analyst agent (later) is where nuance
 *     belongs.
 */

export type PostureSeverity = 'critical' | 'high' | 'medium' | 'low'

export type PostureItem = {
  /** Stable machine key so consumers can react to a specific item. */
  key: string
  /** Plain-English label for the founder. */
  label: string
  /** Optional founder-facing detail (e.g. the value we captured). */
  detail?: string
}

export type DraftFinding = {
  /** Stable id derived from the action key — survives re-renders. */
  id: string
  /** Key for the action; mirrors a `PostureItem.key` when applicable. */
  key: string
  title: string
  /** One-paragraph description, plain English. */
  description: string
  /** Primary regulatory anchor (e.g. "GDPR Article 30"). */
  regulation: string
  severity: PostureSeverity
}

export type PostureSummary = {
  covered: PostureItem[]
  missing: PostureItem[]
  topAction: DraftFinding
}

// `staffCount` threshold above which we assume the DPO question is non-
// trivial. GDPR Article 37 itself doesn't gate on a headcount — it gates on
// the nature of processing — but for an MVP nudge we only push DPO as the
// top action when there's enough scale that the answer is likely "yes you
// need one". Below this we still surface DPO as a missing item but don't
// promote it past more actionable gaps.
const DPO_TOP_ACTION_STAFF_THRESHOLD = 50

export function computePostureSummary(profile: ComplianceProfile): PostureSummary {
  const covered: PostureItem[] = []
  const missing: PostureItem[] = []

  // Business mapped — always covered: the founder named industry +
  // jurisdictions, otherwise we wouldn't have gotten this far.
  covered.push({
    key: 'business_mapped',
    label: 'Business profile mapped',
    detail:
      profile.euJurisdictions.length > 0
        ? `${profile.industry} · ${profile.euJurisdictions.join(', ')}`
        : profile.industry,
  })

  // Data inventory — covered as soon as the founder named any categories.
  if (profile.dataCategories.length > 0) {
    covered.push({
      key: 'data_inventory',
      label: 'Personal data inventory drafted',
      detail: profile.dataCategories.slice(0, 3).join(', '),
    })
  } else {
    missing.push({
      key: 'data_inventory',
      label: 'Personal data inventory',
    })
  }

  // AI systems catalogue — covered when AI tools were named (or explicitly
  // none). We treat "answered" as covered because the inventory is the
  // baseline; the literacy gap is a separate item below.
  if (profile.aiSystems.length > 0) {
    covered.push({
      key: 'ai_systems',
      label: 'AI tools catalogued',
      detail: profile.aiSystems.slice(0, 3).join(', '),
    })
  }

  // DPO designation.
  if (profile.hasDpo === 'yes') {
    covered.push({ key: 'dpo', label: 'Data Protection Officer designated' })
  } else {
    missing.push({
      key: 'dpo',
      label: 'Data Protection Officer',
      detail: profile.hasDpo === 'unsure' ? 'Founder unsure' : undefined,
    })
  }

  // ROPA — the most common gap, near-universal for early SMEs.
  if (profile.hasRopa === 'yes') {
    covered.push({ key: 'ropa', label: 'Record of Processing Activities' })
  } else {
    missing.push({
      key: 'ropa',
      label: 'Record of Processing Activities',
      detail: profile.hasRopa === 'unsure' ? 'Founder unsure' : undefined,
    })
  }

  // AI literacy (Article 4 EU AI Act). Always missing when AI is in use —
  // we never asked the founder about training, so we can't claim it's
  // covered. Surfacing it here is the value-add of the summary.
  if (profile.aiSystems.length > 0) {
    missing.push({
      key: 'ai_literacy',
      label: 'AI literacy training (Article 4)',
    })
  }

  // Cross-border transfer safeguards — only relevant when transfers happen.
  if (profile.transfersOutsideEu === 'yes') {
    missing.push({
      key: 'transfer_safeguards',
      label: 'Cross-border transfer safeguards',
      detail:
        profile.transferDestinations.length > 0
          ? profile.transferDestinations.slice(0, 3).join(', ')
          : undefined,
    })
  }

  const topAction = pickTopAction(profile, missing)

  return { covered, missing, topAction }
}

function pickTopAction(
  profile: ComplianceProfile,
  missing: PostureItem[],
): DraftFinding {
  const missingKeys = new Set(missing.map((item) => item.key))

  // 1. ROPA — baseline GDPR documentation. Promote whenever absent.
  if (missingKeys.has('ropa')) {
    return draft({
      key: 'ropa',
      title: 'Draft your Record of Processing Activities',
      description:
        'GDPR Article 30 requires every controller to keep a Record of Processing Activities listing each purpose, the data categories involved, the recipients, and retention periods. Starting one now means later findings can extend an existing record instead of asking you to build it under deadline pressure.',
      regulation: 'GDPR Article 30',
      severity: 'high',
    })
  }

  // 2. AI literacy — Article 4 has been enforceable since Feb 2025 and is
  //    the lowest-effort gap to close: a documented training record.
  if (missingKeys.has('ai_literacy')) {
    const tools = profile.aiSystems.slice(0, 2).join(', ')
    return draft({
      key: 'ai_literacy',
      title: 'Document AI literacy training for your team',
      description: `EU AI Act Article 4 requires documented AI literacy for every staff member using AI tools at work${
        tools ? ` (you mentioned ${tools})` : ''
      }. This obligation has been in force since February 2025. A brief training session plus an attestation log is enough to evidence it.`,
      regulation: 'EU AI Act Article 4',
      severity: 'high',
    })
  }

  // 3. Cross-border transfer safeguards.
  if (missingKeys.has('transfer_safeguards')) {
    const destinations = profile.transferDestinations.slice(0, 2).join(', ')
    return draft({
      key: 'transfer_safeguards',
      title: 'Document your cross-border transfer mechanism',
      description: `You mentioned data leaves the EU${
        destinations ? ` (${destinations})` : ''
      }. GDPR Chapter V requires a documented transfer mechanism, typically Standard Contractual Clauses or an adequacy decision, for every destination outside the EEA. List each vendor and the mechanism that covers it.`,
      regulation: 'GDPR Chapter V',
      severity: 'medium',
    })
  }

  // 4. DPO designation — only promote past lower-severity items when scale
  //    justifies it. Below the threshold the gap stays in the missing list
  //    but doesn't claim the top slot.
  if (
    missingKeys.has('dpo') &&
    profile.staffCount !== null &&
    profile.staffCount >= DPO_TOP_ACTION_STAFF_THRESHOLD
  ) {
    return draft({
      key: 'dpo',
      title: 'Designate a Data Protection Officer',
      description: `With around ${profile.staffCount} staff and personal data flowing through your operations, GDPR Article 37 likely requires you to designate a Data Protection Officer. The DPO can be internal or an external consultant. What matters is the named, contactable accountability point.`,
      regulation: 'GDPR Article 37',
      severity: 'medium',
    })
  }

  // 5. Fallback nudge so the card always has something concrete to offer
  //    even for the rare profile where every higher-priority gap is closed.
  return draft({
    key: 'privacy_policy_review',
    title: 'Schedule a privacy policy review',
    description:
      "You've covered the structural gaps an SME usually hits first. A quarterly review of your public privacy policy against your actual processing activities is the highest-value next step. It keeps the document in sync with how the business has evolved.",
    regulation: 'GDPR Article 13',
    severity: 'low',
  })
}

function draft(args: {
  key: string
  title: string
  description: string
  regulation: string
  severity: PostureSeverity
}): DraftFinding {
  return {
    id: `posture-${args.key}`,
    ...args,
  }
}
