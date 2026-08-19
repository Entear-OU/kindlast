/**
 * Connection helpers for the database test suite (ENT-192).
 *
 * The suite runs against the compose stack in deploy/compose.yaml
 * (`docker compose -f deploy/compose.yaml up`), which publishes postgres-app
 * on 127.0.0.1:5433 for a single checkout. Every URL is overridable through
 * the environment so CI, or a worktree running its own stack, can point
 * elsewhere.
 *
 * ONE STACK PER WORKTREE (ENT-250). A second checkout of this repository runs
 * its own Postgres on its own port, and this suite has to reach the one its
 * own branch brought up, or it asserts a schema somebody else's unmerged
 * migration wrote. `scripts/stack-env.sh` derives that port and exports both
 * `KINDLAST_PG_APP_PORT` and the `PG_*_URL` set below:
 *
 *     eval "$(./scripts/stack-env.sh)"
 *     bun run test:db
 *
 * Reading `KINDLAST_PG_APP_PORT` as well as `PG_PORT` is not redundancy for
 * its own sake. It is the variable compose itself reads, so setting only that
 * one, by hand or from deploy/.env, still lands this suite on the right stack
 * rather than on 5433 and somebody else's database.
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
const PORT = process.env.PG_PORT ?? process.env.KINDLAST_PG_APP_PORT ?? '5433'

/**
 * The development DSN for one database role, on whichever stack this worktree
 * is pointed at.
 *
 * The suites that need a role beyond the three below (agent, billing, ingest)
 * used to spell the whole URL out, which put `127.0.0.1:5433` in a dozen files
 * and made the port a thing you had to remember to change in all of them. The
 * passwords are the compose defaults, and they are development-only values by
 * construction: `deploy/postgres/init/01-roles.sh` is where they come from.
 */
export function roleUrl(role: string): string {
  const override = process.env[`PG_${role.toUpperCase()}_URL`]
  if (override) return override
  return `postgres://kindlast_${role}:${role}-dev-password@${HOST}:${PORT}/kindlast`
}

export const SUPER_URL =
  process.env.PG_SUPER_URL ??
  `postgres://postgres:postgres-dev-password@${HOST}:${PORT}/kindlast`
export const MIGRATOR_URL = roleUrl('migrator')
export const APP_URL = roleUrl('app')

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
