import { type Obligation, type ObligationsData, toRow } from './obligations'

/**
 * Generate the SQL seed migration for the obligations catalogue (ENT-157).
 *
 * The curated corpus (`data/corpus/obligations.json`) is the single source of
 * truth. Historically the only path into `public.obligations` was a hand-run
 * `bun run ingest:obligations`, which no migration/seed/CI step ever invoked — so
 * production ran with an empty catalogue and the Watcher found no gaps. This
 * module renders the same corpus into an idempotent migration that ships with
 * `supabase/migrations`, so every environment seeds the catalogue from
 * migrations alone.
 *
 * `buildObligationsSeedMigration` is a pure function of the corpus data: same
 * input → byte-identical output. A drift-guard test regenerates it and asserts
 * the committed migration file matches, so the corpus JSON and the migration
 * can never silently diverge. The column mapping is delegated to `toRow` (the
 * same flattening the runtime ingest uses), so there is exactly one place that
 * knows how an `Obligation` maps onto the table.
 */

/** A SQL string literal, single-quotes doubled. `null` → the keyword NULL. */
function sqlStr(value: string | null): string {
  if (value === null) return 'null'
  return `'${value.replace(/'/g, "''")}'`
}

/** A SQL integer literal, or the keyword NULL. */
function sqlInt(value: number | null): string {
  return value === null ? 'null' : String(value)
}

/** A JSONB literal: the JSON serialised, quoted as text, cast to jsonb. */
function sqlJsonb(value: Record<string, unknown>): string {
  return `${sqlStr(JSON.stringify(value))}::jsonb`
}

/** A Postgres text[] array literal (e.g. array['a','b']::text[]). */
function sqlTextArray(values: ReadonlyArray<string>): string {
  if (values.length === 0) return `'{}'::text[]`
  return `array[${values.map((v) => sqlStr(v)).join(', ')}]::text[]`
}

// Column order for the INSERT — matches the `toRow` shape one-to-one. The
// `do update` clause below must stay in lockstep with this list.
const COLUMNS = [
  'slug',
  'title',
  'summary',
  'citation_celex',
  'citation_kind',
  'citation_article',
  'citation_recital',
  'citation_annex',
  'citation_paragraph',
  'applies_when',
  'severity',
  'due_within_days',
  'recurrence',
  'effective_date',
  'topic_tags',
] as const

function valuesTuple(o: Obligation): string {
  const r = toRow(o)
  const cells = [
    sqlStr(r.slug),
    sqlStr(r.title),
    sqlStr(r.summary),
    sqlStr(r.citation_celex),
    sqlStr(r.citation_kind),
    sqlInt(r.citation_article),
    sqlInt(r.citation_recital),
    sqlStr(r.citation_annex),
    sqlStr(r.citation_paragraph),
    sqlJsonb(r.applies_when),
    sqlStr(r.severity),
    sqlInt(r.due_within_days),
    sqlStr(r.recurrence),
    sqlStr(r.effective_date),
    sqlTextArray(r.topic_tags),
  ]
  return `  (${cells.join(',\n   ')})`
}

const HEADER = `-- Seed the obligations catalogue (ENT-157)
--
-- Generated from data/corpus/obligations.json by
-- scripts/generate-obligations-seed.ts — DO NOT EDIT BY HAND. Re-run
-- \`bun run generate:obligations-seed\` after editing the corpus; a drift-guard
-- unit test (__tests__/lib/corpus/obligations-seed-sql.test.ts) fails if this
-- file and the corpus disagree.
--
-- Why a migration and not the \`bun run ingest:obligations\` script: nothing ever
-- ran that script automatically, so production shipped with an empty
-- \`public.obligations\` and the Watcher's gap detector iterated zero rows —
-- an empty feed for every real user (ENT-157). Seeding from a migration means
-- every environment (local reset, CI \`supabase start\`, remote deploy) gets
-- the catalogue with no manual step.
--
-- Idempotent: \`on conflict (slug) do update\` upserts by the natural key, so
-- re-applying the migration (or re-running the curated ingest) converges to
-- the same row state. Rows removed from a later corpus snapshot are left
-- in place — same non-destructive policy as the runtime ingest.
`

/**
 * Render the full text of the seed migration for `data`. Deterministic:
 * obligations are emitted in corpus order, so the output is stable across
 * runs and diffable in review.
 */
export function buildObligationsSeedMigration(data: ObligationsData): string {
  const tuples = data.obligations.map(valuesTuple).join(',\n')
  const updates = COLUMNS.filter((c) => c !== 'slug')
    .map((c) => `  ${c} = excluded.${c}`)
    .join(',\n')

  return `${HEADER}
insert into public.obligations
  (${COLUMNS.join(', ')})
values
${tuples}
on conflict (slug) do update set
${updates};
`
}
