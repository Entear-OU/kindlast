#!/usr/bin/env tsx
/**
 * Ingest the GDPR full-text snapshot at `data/corpus/gdpr.json` into the
 * regulatory corpus tables. Idempotent — re-runs merge by CELEX number and
 * (document_id, article_number) / (document_id, recital_number), so calling
 * this script twice produces the same row state, not duplicates (ENT-48).
 *
 * Usage:
 *
 *   pnpm ingest:gdpr               # local Supabase from .env.local
 *   SUPABASE_URL=... SUPABASE_SECRET_KEY=... pnpm ingest:gdpr   # remote
 *
 * The corpus tables have no INSERT policy for anon/authenticated, so this
 * MUST run with the service-role key. The script bails loudly if either env
 * var is missing — silently falling back to the anon key would write zero
 * rows and look "successful", which is the worst possible failure mode for
 * a one-shot ingest job.
 */

import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { argv, cwd, exit, loadEnvFile } from 'node:process'

import { createClient } from '@supabase/supabase-js'

import { ingestRegulation, parseRegulationData } from '../lib/corpus/ingest'

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

const DEFAULT_DATA_PATH = 'data/corpus/gdpr.json'

async function main(): Promise<void> {
  const dataPath = argv[2] ?? DEFAULT_DATA_PATH
  const supabaseUrl = process.env.SUPABASE_URL
  const serviceKey = process.env.SUPABASE_SECRET_KEY

  if (!supabaseUrl || !serviceKey) {
    console.error(
      'ingest-gdpr: SUPABASE_URL and SUPABASE_SECRET_KEY must be set ' +
        '(check .env.local for local dev, or export them for remote).',
    )
    exit(1)
  }

  const absolutePath = resolve(cwd(), dataPath)
  console.log(`ingest-gdpr: loading ${absolutePath}`)
  let raw: unknown
  try {
    const text = await readFile(absolutePath, 'utf8')
    raw = JSON.parse(text)
  } catch (err) {
    console.error(
      `ingest-gdpr: failed to read/parse ${absolutePath}: ${err instanceof Error ? err.message : String(err)}`,
    )
    exit(1)
  }

  let data
  try {
    data = parseRegulationData(raw)
  } catch (err) {
    console.error('ingest-gdpr: source data is malformed:')
    console.error(err instanceof Error ? err.message : String(err))
    exit(1)
  }

  console.log(
    `ingest-gdpr: validated payload — ${data.articles.length} articles, ${data.recitals.length} recitals` +
      (data.articleRecitals ? `, ${data.articleRecitals.length} links` : ''),
  )

  const supabase = createClient(supabaseUrl, serviceKey, {
    auth: { autoRefreshToken: false, persistSession: false },
  })

  const result = await ingestRegulation(supabase, data)
  console.log(
    `ingest-gdpr: done — document ${result.documentId}, ` +
      `${result.articlesUpserted} articles, ${result.recitalsUpserted} recitals, ` +
      `${result.linksUpserted} links upserted.`,
  )
}

main().catch((err) => {
  console.error('ingest-gdpr: ingest failed:')
  console.error(err instanceof Error ? err.stack ?? err.message : String(err))
  exit(1)
})
