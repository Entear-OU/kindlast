/**
 * The platform products never touch the domain database (ENT-256, §14.3).
 *
 * postgres-platform holds Zitadel's database and Temporal's two, and the rule
 * that goes with it is "four roles, four connection strings, no overlap": the
 * application never connects to postgres-platform, and neither Zitadel nor
 * Temporal ever connects to postgres-app. Half of that cannot be asserted from
 * here, because postgres-platform publishes no port and this suite reaches
 * only the domain database. What can be asserted is the half that would be the
 * first symptom of somebody taking the shortcut: a `temporal` or `zitadel`
 * role on postgres-app, which is what it would take for either to be pointed
 * at the domain Postgres "just for now".
 *
 * Small on purpose. It is here so the rule has a test that goes red, rather
 * than a sentence in a compose comment that does not.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import { connect, isStackReachable, SUPER_URL } from './helpers/db'

const reachable = await isStackReachable()

let admin: Client

beforeAll(async () => {
  if (!reachable) return
  admin = await connect(SUPER_URL)
})

afterAll(async () => {
  if (!reachable) return
  await admin.end()
})

describe.skipIf(!reachable)(
  'the platform products stay off the domain database',
  () => {
    it.each(['temporal', 'zitadel'])(
      'has no role named %s on postgres-app',
      async (name) => {
        const { rows } = await admin.query(
          `select 1 from pg_roles where rolname = $1`,
          [name],
        )
        expect(rows).toHaveLength(0)
      },
    )

    it('has no database named for either on postgres-app', async () => {
      const { rows } = await admin.query(
        `select datname from pg_database
        where datname in ('temporal', 'temporal_visibility', 'zitadel')`,
      )
      expect(rows).toHaveLength(0)
    })
  },
)
