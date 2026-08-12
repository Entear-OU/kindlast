#!/usr/bin/env tsx
/**
 * Ingest the curated DPA enforcement-decisions snapshot into the regulatory
 * corpus (ENT-99). Idempotent — re-runs merge by `slug`, so calling this
 * script twice produces the same row state, not duplicates.
 *
 *   bun run ingest:enforcement
 *
 *   # or call directly:
 *   tsx scripts/ingest-enforcement.ts data/corpus/enforcement-decisions.json
 *
 * Auth: corpus tables have no INSERT policy for anon/authenticated, so
 * this MUST run with the service-role key. The script bails loudly if
 * either env var is missing — silently falling back to the anon key
 * would write zero rows and look "successful", which is the worst
 * possible failure mode for a one-shot ingest job.
 */

import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { argv, cwd, exit, loadEnvFile } from 'node:process'

import { createClient } from '@supabase/supabase-js'

import { ingestEnforcementDecisions, parseEnforcementData } from '../lib/corpus/enforcement'

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

const DEFAULT_SNAPSHOT = '../../data/corpus/enforcement-decisions.json'

async function main(): Promise<void> {
  const dataPath = argv[2] ?? DEFAULT_SNAPSHOT
  const supabaseUrl = process.env.SUPABASE_URL
  const serviceKey = process.env.SUPABASE_SECRET_KEY

  if (!supabaseUrl || !serviceKey) {
    console.error(
      'ingest:enforcement: SUPABASE_URL and SUPABASE_SECRET_KEY must be set ' +
        '(check .env.local for local dev, or export them for remote).',
    )
    exit(1)
  }

  const absolutePath = resolve(cwd(), dataPath)
  console.log(`ingest:enforcement: loading ${absolutePath}`)

  let raw: unknown
  try {
    const text = await readFile(absolutePath, 'utf8')
    raw = JSON.parse(text)
  } catch (err) {
    console.error(
      `ingest:enforcement: failed to read/parse ${absolutePath}: ${err instanceof Error ? err.message : String(err)}`,
    )
    exit(1)
  }

  let data
  try {
    data = parseEnforcementData(raw)
  } catch (err) {
    console.error('ingest:enforcement: source data is malformed:')
    console.error(err instanceof Error ? err.message : String(err))
    exit(1)
  }

  console.log(
    `ingest:enforcement: validated payload — ${data.decisions.length} decisions`,
  )

  const supabase = createClient(supabaseUrl, serviceKey, {
    auth: { autoRefreshToken: false, persistSession: false },
  })

  const result = await ingestEnforcementDecisions(supabase, data)
  console.log(`ingest:enforcement: done — ${result.decisionsUpserted} decisions upserted.`)
}

main().catch((err) => {
  console.error('ingest:enforcement: failed:')
  console.error(err instanceof Error ? err.stack ?? err.message : String(err))
  exit(1)
})
