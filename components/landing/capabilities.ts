/**
 * What Kindlast actually covers, as `/features` states it.
 *
 * A data module rather than JSX for the same reason `pipeline-stages.ts` is
 * one: the copy is the claim, and the claim has to be checkable against the
 * code it describes.
 *
 * The version of this page that shipped before was written against a product
 * that no longer exists (arguably never did). It advertised a 0-100 compliance
 * score with progress over time and audit-ready PDF export; a grep for
 * `compliance_score` or `pdf` across `lib`, `app`, `components` and `supabase`
 * matched the marketing component and nothing else. It also drew a panel of
 * four invented percentages and an AI Act tier chart with a "You" badge, both
 * decoration dressed as product data. And it described a one-shot assessment
 * ("AI evaluates your business") on a site whose other three pages describe a
 * system that runs on a schedule and waits for approval.
 *
 * The repository is public and every other page on this site ends by telling
 * people to go and check it. So the rule for anything in this file: it exists
 * in the tree, and the entry names where.
 */

export interface Register {
  /** How a reader would say it. */
  name: string
  /** The term of art, used as the label. */
  short: string
  /** What the register holds. */
  body: string
  /** What the agents do to it, unprompted. Nothing here is passive storage. */
  watched: string
  /** Where it lives in the tree, for anyone reading along in the repo. */
  source: string
}

/**
 * The three registers.
 *
 * These are the substrate the rest of the product stands on, and the old page
 * did not mention any of them. Everything the Watcher detects is a discrepancy
 * between one of these and the obligations catalogue, and all three of the
 * Executor's write paths (`executor_create_ropa`, `executor_create_dsar`,
 * `executor_create_ai_system`) land here.
 */
export const REGISTERS: Register[] = [
  {
    name: 'Records of processing',
    short: 'ROPA',
    body: 'What personal data you handle, why, on what lawful basis, who else sees it, where it goes, and how long you keep it. Article 30 expects this to exist and to be current.',
    watched:
      'When an activity turns up with no entry behind it, that becomes a finding rather than something you discover during an audit.',
    source: 'lib/records/ropa.ts',
  },
  {
    name: 'Data subject requests',
    short: 'DSAR log',
    body: 'Every access, deletion, correction and portability request, with the statutory clock it is running against and where it has got to.',
    watched:
      'The clock is checked daily. Inside ten days, or already overdue, the request escalates on its own under Article 12(3).',
    source: 'lib/records/dsar.ts',
  },
  {
    name: 'AI systems',
    short: 'AI system register',
    body: 'The AI you build or deploy, sorted by the risk tier the EU AI Act puts it in, with the obligations that follow from that tier.',
    watched:
      'Operating an AI system with nothing in the register is itself a gap, and it surfaces on day one rather than at the deadline.',
    source: 'lib/records/ai-system.ts',
  },
]

export interface AnatomyField {
  /** The field's name in plain language. */
  label: string
  /** The value this field carries for the specimen finding. */
  value: string
  /** The column it is read from, so the page can be checked against the code. */
  column: string
  /** Why this field is on a finding at all. */
  note: string
}

/**
 * One real finding, taken apart.
 *
 * This is the page's centrepiece, and it is deliberately the same signal
 * `/how-it-works` follows end to end: a marketing analytics tool processing
 * personal data with no record behind it. A reader moving between the two pages
 * should be looking at one example, not two.
 *
 * Every `column` here is in the select list in `lib/feed/findings.ts`. That is
 * the whole point of the section: a feature list is a promise, and a specimen
 * with its columns named is closer to evidence.
 */
export const FINDING_ANATOMY: AnatomyField[] = [
  {
    label: 'The obligation',
    value: 'GDPR Article 30',
    column: 'regulatory_obligation',
    note: 'Drawn from the obligations catalogue rather than invented per finding, so two findings about the same rule cite the same rule.',
  },
  {
    label: 'The source text',
    value: 'The article itself, with the paragraphs it turns on',
    column: 'citation_url',
    note: 'The verbatim regulatory text sits behind the finding, so you can read what it actually says instead of taking the summary on trust.',
  },
  {
    label: 'How serious',
    value: 'High',
    column: 'severity',
    note: 'One of low, medium, high or critical. It is what drives the red band on your dashboard and what gets a deadline alert sent early.',
  },
  {
    label: 'How much work',
    value: 'Hours',
    column: 'effort_estimate',
    note: 'Minutes, hours or days. A finding you cannot budget for is a finding you postpone.',
  },
  {
    label: 'The one thing to do',
    value: 'Create the missing record of processing for marketing analytics',
    column: 'proposed_action',
    note: 'Exactly one action, phrased as an instruction. A deterministic critic rejects the draft and regenerates it if it hedges.',
  },
  {
    label: 'Its identity',
    value: 'ropa-gap:marketing-analytics',
    column: 'dedup_key',
    note: 'Stable across runs, so tomorrow updates this finding rather than sending you a second copy of it.',
  },
]

export interface CorpusSource {
  name: string
  detail: string
}

/**
 * What the Analyst reads before it says anything.
 *
 * Counts of articles and recitals are fixed properties of the regulations, so
 * they are safe to state. Enforcement tallies, fine figures and deadline dates
 * are not, and have already been pulled off this site once for going stale.
 */
export const CORPUS_SOURCES: CorpusSource[] = [
  {
    name: 'The GDPR',
    detail: 'All 99 articles and 173 recitals, paragraph by paragraph.',
  },
  {
    name: 'The EU AI Act',
    detail: 'All 113 articles, the recitals, and the Annex III high-risk list.',
  },
  {
    name: 'EDPB guidelines',
    detail:
      'The guidance the supervisory authorities themselves work from, where it bears on an obligation.',
  },
  {
    name: 'DPA enforcement decisions',
    detail:
      'Landmark decisions from national regulators, so a finding can point at how a rule has actually been applied.',
  },
]

export interface Guarantee {
  title: string
  body: string
}

/**
 * The limits, stated as limits.
 *
 * The old page had a "Privacy-First Architecture" card claiming data "never
 * leaves the secure pipeline". In the sense a reader would take it, that is not
 * true: an LLM drafts every finding, so a model provider is in the loop.
 * Self-hosting is the honest answer to the question that sentence was reaching
 * for, and it is a stronger one.
 */
export const GUARANTEES: Guarantee[] = [
  {
    title: 'Nothing is written without your approval',
    body: 'There is no autonomous write path. The Executor runs on an explicit approval and on nothing else, and a finding you ignore simply stays open.',
  },
  {
    title: 'Every action leaves an audit log row',
    body: 'What was done, when, and who approved it, written immutably. That row is the artefact an auditor can actually use.',
  },
  {
    title: 'Isolation is enforced by the database',
    body: 'Tenant separation is row-level security in Postgres, not a where-clause in application code that one missed call site can defeat.',
  },
  {
    title: 'You can run the whole thing yourself',
    body: 'The engine is public under AGPL-3.0, so you can self-host it and keep your records inside your own infrastructure, or read it before you trust it.',
  },
]
