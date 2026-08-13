#!/usr/bin/env tsx
/**
 * Ingest the curated EDPB / WP29 guidelines snapshot into the regulatory
 * corpus (ENT-50). Idempotent — re-runs merge by `slug`, so calling this
 * script twice produces the same row state, not duplicates.
 *
 *   bun run ingest:edpb-guidelines
 *
 *   # or call directly:
 *   tsx scripts/ingest-edpb-guidelines.ts data/corpus/edpb-guidelines.json
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

import { ingestGuidelines, parseGuidelinesData } from '../lib/corpus/guidelines'

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

const DEFAULT_SNAPSHOT = '../../data/corpus/edpb-guidelines.json'

async function main(): Promise<void> {
  const dataPath = argv[2] ?? DEFAULT_SNAPSHOT
  const supabaseUrl = process.env.SUPABASE_URL
  const serviceKey = process.env.SUPABASE_SECRET_KEY

  if (!supabaseUrl || !serviceKey) {
    console.error(
      'ingest:edpb-guidelines: SUPABASE_URL and SUPABASE_SECRET_KEY must be set ' +
        '(check .env.local for local dev, or export them for remote).',
    )
    exit(1)
  }

  const absolutePath = resolve(cwd(), dataPath)
  console.log(`ingest:edpb-guidelines: loading ${absolutePath}`)

  let raw: unknown
  try {
    const text = await readFile(absolutePath, 'utf8')
    raw = JSON.parse(text)
  } catch (err) {
    console.error(
      `ingest:edpb-guidelines: failed to read/parse ${absolutePath}: ${err instanceof Error ? err.message : String(err)}`,
    )
    exit(1)
  }

  let data
  try {
    data = parseGuidelinesData(raw)
  } catch (err) {
    console.error('ingest:edpb-guidelines: source data is malformed:')
    console.error(err instanceof Error ? err.message : String(err))
    exit(1)
  }

  console.log(
    `ingest:edpb-guidelines: validated payload — ${data.guidelines.length} guidelines`,
  )

  const supabase = createClient(supabaseUrl, serviceKey, {
    auth: { autoRefreshToken: false, persistSession: false },
  })

  const result = await ingestGuidelines(supabase, data)
  console.log(
    `ingest:edpb-guidelines: done — ${result.guidelinesUpserted} guidelines upserted.`,
  )
}

main().catch((err) => {
  console.error('ingest:edpb-guidelines: failed:')
  console.error(err instanceof Error ? err.stack ?? err.message : String(err))
  exit(1)
})
