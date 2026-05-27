#!/usr/bin/env tsx
/**
 * Ingest the curated obligations catalogue snapshot (ENT-52). Idempotent —
 * re-runs merge by `slug`, so calling this script twice produces the same
 * row state, not duplicates.
 *
 *   pnpm ingest:obligations
 *
 *   # or call directly:
 *   tsx scripts/ingest-obligations.ts data/corpus/obligations.json
 *
 * Auth: the `obligations` table has no INSERT policy for anon /
 * authenticated, so this MUST run with the service-role key. The script
 * bails loudly if either env var is missing — silently falling back to
 * the anon key would write zero rows and look "successful", which is the
 * worst possible failure mode for a one-shot ingest job.
 */

import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { argv, cwd, exit, loadEnvFile } from 'node:process'

import { createClient } from '@supabase/supabase-js'

import { ingestObligations, parseObligationsData } from '../lib/corpus/obligations'

// Load env from .env.local (local dev) then .env (fallback). Both files are
// optional; if they're missing, we expect the caller to have set env vars
// directly (CI / remote deploys).
for (const file of ['.env.local', '.env']) {
  try {
    loadEnvFile(file)
  } catch {
    // file doesn't exist or isn't readable — fall through, env may already be set
  }
}

const DEFAULT_SNAPSHOT = 'data/corpus/obligations.json'

async function main(): Promise<void> {
  const dataPath = argv[2] ?? DEFAULT_SNAPSHOT
  const supabaseUrl = process.env.SUPABASE_URL
  const serviceKey = process.env.SUPABASE_SECRET_KEY

  if (!supabaseUrl || !serviceKey) {
    console.error(
      'ingest:obligations: SUPABASE_URL and SUPABASE_SECRET_KEY must be set ' +
        '(check .env.local for local dev, or export them for remote).',
    )
    exit(1)
  }

  const absolutePath = resolve(cwd(), dataPath)
  console.log(`ingest:obligations: loading ${absolutePath}`)

  let raw: unknown
  try {
    const text = await readFile(absolutePath, 'utf8')
    raw = JSON.parse(text)
  } catch (err) {
    console.error(
      `ingest:obligations: failed to read/parse ${absolutePath}: ${err instanceof Error ? err.message : String(err)}`,
    )
    exit(1)
  }

  let data
  try {
    data = parseObligationsData(raw)
  } catch (err) {
    console.error('ingest:obligations: source data is malformed:')
    console.error(err instanceof Error ? err.message : String(err))
    exit(1)
  }

  console.log(
    `ingest:obligations: validated payload — ${data.obligations.length} obligations`,
  )

  const supabase = createClient(supabaseUrl, serviceKey, {
    auth: { autoRefreshToken: false, persistSession: false },
  })

  const result = await ingestObligations(supabase, data)
  console.log(
    `ingest:obligations: done — ${result.obligationsUpserted} obligations upserted.`,
  )
}

main().catch((err) => {
  console.error('ingest:obligations: failed:')
  console.error(err instanceof Error ? err.stack ?? err.message : String(err))
  exit(1)
})
