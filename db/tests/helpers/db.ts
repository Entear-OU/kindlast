/**
 * Connection helpers for the database test suite (ENT-192).
 *
 * The suite runs against the compose stack in deploy/compose.yaml
 * (`docker compose -f deploy/compose.yaml up`), which publishes postgres-app
 * on 127.0.0.1:5433. Every URL is overridable through the environment so CI
 * or a non-default local setup can point elsewhere.
 *
 * Three connections, because the role split is the thing under test:
 *   - super: the container superuser. Bypasses RLS; used to prove the
 *     isolation tests can distinguish enforcement from absence of data.
 *   - migrator: kindlast_migrator, schema owner, BYPASSRLS. Used to seed
 *     fixtures the app role is then shown or not shown.
 *   - app: kindlast_app, the role the application will connect as.
 *     NOSUPERUSER, NOBYPASSRLS, owns nothing. RLS is its whole world.
 */
import { Client } from 'pg'

const HOST = process.env.PG_HOST ?? '127.0.0.1'
const PORT = process.env.PG_PORT ?? '5433'

export const SUPER_URL =
  process.env.PG_SUPER_URL ??
  `postgres://postgres:postgres-dev-password@${HOST}:${PORT}/kindlast`
export const MIGRATOR_URL =
  process.env.PG_MIGRATOR_URL ??
  `postgres://kindlast_migrator:migrator-dev-password@${HOST}:${PORT}/kindlast`
export const APP_URL =
  process.env.PG_APP_URL ??
  `postgres://kindlast_app:app-dev-password@${HOST}:${PORT}/kindlast`

export async function connect(url: string): Promise<Client> {
  const client = new Client({ connectionString: url })
  await client.connect()
  return client
}

/**
 * Sets the two tenancy GUCs the RLS policies read, session-wide.
 * Mirrors what Core API middleware will do per request (§20.1 of the
 * core-api-surface doc): resolve the active org once, then set both.
 */
export async function setTenant(
  client: Client,
  orgId: string,
  userId: string,
): Promise<void> {
  await client.query("select set_config('app.current_org_id', $1, false)", [
    orgId,
  ])
  await client.query("select set_config('app.current_user_id', $1, false)", [
    userId,
  ])
}

/** True when the compose stack's postgres-app answers on the expected port. */
export async function isStackReachable(): Promise<boolean> {
  try {
    const c = new Client({
      connectionString: SUPER_URL,
      connectionTimeoutMillis: 2000,
    })
    await c.connect()
    await c.end()
    return true
  } catch {
    return false
  }
}
