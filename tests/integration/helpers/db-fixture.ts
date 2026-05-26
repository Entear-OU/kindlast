import { Client } from 'pg'

/**
 * Apply a SQL fixture against the local Supabase Postgres directly.
 *
 * Why a direct `pg` connection instead of a Supabase RPC? DDL through the
 * Supabase HTTP API requires a permissive `exec_sql` function in the schema,
 * which is a footgun if it ever leaks to a non-local stack. A direct connection
 * is scoped to the local CLI's published port (54322) and never touches remote.
 *
 * Fixtures are intentionally written idempotently (`create table if not
 * exists`, `drop policy if exists` + `create policy`) so suites can re-run
 * without a full `supabase db reset`.
 */

const LOCAL_DB_URL =
  process.env.SUPABASE_TEST_DB_URL ??
  'postgresql://postgres:postgres@127.0.0.1:54322/postgres'

async function runSql(sql: string): Promise<void> {
  const client = new Client({ connectionString: LOCAL_DB_URL })
  await client.connect()
  try {
    await client.query(sql)
  } finally {
    await client.end()
  }
}

export async function applyFixtureSql(sql: string): Promise<void> {
  try {
    await runSql(sql)
  } catch (err) {
    throw new Error(
      `applyFixtureSql failed: ${err instanceof Error ? err.message : String(err)}`,
    )
  }
}

export async function dropFixtureSql(sql: string): Promise<void> {
  try {
    await runSql(sql)
  } catch (err) {
    throw new Error(
      `dropFixtureSql failed: ${err instanceof Error ? err.message : String(err)}`,
    )
  }
}
