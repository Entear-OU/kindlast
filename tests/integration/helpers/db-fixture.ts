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

// Transient, retryable Postgres failure classes: deadlock (40P01) and
// serialization failure (40001). Suites run in parallel against one local DB, so
// DDL that takes heavy locks (e.g. `drop table` on a fixture with an FK to
// auth.users) can deadlock with another suite churning auth.users. Postgres
// aborts one side; retrying the statement succeeds.
const RETRYABLE_SQLSTATE = new Set(['40P01', '40001'])

async function runSql(sql: string, attempts = 5): Promise<void> {
  for (let attempt = 1; ; attempt++) {
    const client = new Client({ connectionString: LOCAL_DB_URL })
    await client.connect()
    try {
      await client.query(sql)
      return
    } catch (err) {
      const code = (err as { code?: string })?.code
      if (code && RETRYABLE_SQLSTATE.has(code) && attempt < attempts) {
        await new Promise((resolve) => setTimeout(resolve, 50 * attempt))
        continue
      }
      throw err
    } finally {
      await client.end()
    }
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

/**
 * Run a `select` (or any returning statement) directly against the local DB.
 * Use sparingly — most tests should go through Supabase clients so RLS is in
 * the loop. Schema-level inspection (extensions, triggers, functions) has no
 * Supabase-client equivalent, so direct queries are the pragmatic option.
 */
export async function querySql<Row extends Record<string, unknown> = Record<string, unknown>>(
  sql: string,
  params: ReadonlyArray<unknown> = [],
): Promise<Row[]> {
  const client = new Client({ connectionString: LOCAL_DB_URL })
  await client.connect()
  try {
    const result = await client.query<Row>(sql, [...params])
    return result.rows
  } finally {
    await client.end()
  }
}
