import { createClient, type SupabaseClient } from '@supabase/supabase-js'

/**
 * Client factories for integration tests against a local Supabase stack.
 *
 * Defaults match `supabase start` output for a freshly initialised project; override
 * via env vars if you point tests at a different stack (e.g. a sandbox project).
 */

export const LOCAL_SUPABASE_URL =
  process.env.SUPABASE_TEST_URL ?? 'http://127.0.0.1:54321'

// Well-known local-dev keys emitted by `supabase start`. Safe to commit — they
// are not secrets and only ever authenticate against a locally-bound stack.
const DEFAULT_ANON_KEY =
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6ImFub24iLCJleHAiOjE5ODM4MTI5OTZ9.CRXP1A7WOeoJeXxjNni43kdQwgnWNReilDMblYTn_I0'
const DEFAULT_SERVICE_ROLE_KEY =
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZS1kZW1vIiwicm9sZSI6InNlcnZpY2Vfcm9sZSIsImV4cCI6MTk4MzgxMjk5Nn0.EGIM96RAZx35lJzdJsyH-qQwv8Hdp7fsn3W0YpN81IU'

const ANON_KEY = process.env.SUPABASE_TEST_ANON_KEY ?? DEFAULT_ANON_KEY
const SERVICE_ROLE_KEY =
  process.env.SUPABASE_TEST_SERVICE_ROLE_KEY ?? DEFAULT_SERVICE_ROLE_KEY

const NO_SESSION = {
  auth: { persistSession: false, autoRefreshToken: false },
} as const

/** Anon client — what a logged-out browser would use. */
export function createAnonClient(): SupabaseClient {
  return createClient(LOCAL_SUPABASE_URL, ANON_KEY, NO_SESSION)
}

/** Service-role client — bypasses RLS; use only for fixture setup/teardown. */
export function createServiceRoleClient(): SupabaseClient {
  return createClient(LOCAL_SUPABASE_URL, SERVICE_ROLE_KEY, NO_SESSION)
}

/** Anon client signed in as a specific test user, e.g. for RLS assertions. */
export async function createUserClient(
  email: string,
  password: string,
): Promise<SupabaseClient> {
  const client = createClient(LOCAL_SUPABASE_URL, ANON_KEY, NO_SESSION)
  const { error } = await client.auth.signInWithPassword({ email, password })
  if (error) {
    throw new Error(`signIn failed for ${email}: ${error.message}`)
  }
  return client
}

/**
 * Probe the local stack with a short timeout. Returns true if reachable, false
 * otherwise — never throws. Used to skip integration suites when Docker / the
 * Supabase CLI isn't running.
 */
export async function isLocalSupabaseReachable(): Promise<boolean> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 1000)
  try {
    const res = await fetch(`${LOCAL_SUPABASE_URL}/auth/v1/health`, {
      headers: { apikey: ANON_KEY },
      signal: controller.signal,
    })
    return res.ok
  } catch {
    return false
  } finally {
    clearTimeout(timer)
  }
}
