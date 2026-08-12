#!/usr/bin/env tsx
/**
 * Regenerate the obligations seed migration from the curated corpus (ENT-157).
 *
 *   bun run generate:obligations-seed
 *
 * Reads data/corpus/obligations.json, validates it with the same Zod schema
 * the runtime ingest uses, and writes the idempotent seed migration. The
 * corpus JSON is the single source of truth; this codegen is the only thing
 * that should ever touch the generated migration. A drift-guard unit test
 * (__tests__/lib/corpus/obligations-seed-sql.test.ts) fails if the committed
 * migration and the corpus disagree, so the regen step can't be forgotten.
 *
 * Re-running is safe and produces byte-identical output for an unchanged
 * corpus. Edit the corpus, run this, commit both files.
 */

import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { cwd, exit } from 'node:process'

import { parseObligationsData } from '../lib/corpus/obligations'
import { buildObligationsSeedMigration } from '../lib/corpus/obligations-seed'

const CORPUS_PATH = 'data/corpus/obligations.json'
const MIGRATION_PATH =
  'supabase/migrations/20260602120000_seed_obligations_catalogue.sql'

function main(): void {
  const corpusAbs = resolve(cwd(), CORPUS_PATH)
  const migrationAbs = resolve(cwd(), MIGRATION_PATH)

  let data
  try {
    data = parseObligationsData(JSON.parse(readFileSync(corpusAbs, 'utf8')))
  } catch (err) {
    console.error(`generate:obligations-seed: ${CORPUS_PATH} is malformed:`)
    console.error(err instanceof Error ? err.message : String(err))
    exit(1)
  }

  const sql = buildObligationsSeedMigration(data)
  writeFileSync(migrationAbs, sql, 'utf8')
  console.log(
    `generate:obligations-seed: wrote ${data.obligations.length} obligations → ${MIGRATION_PATH}`,
  )
}

main()
