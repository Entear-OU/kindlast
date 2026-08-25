/**
 * The two definer functions the scheduled fetch stands on (ENT-279, migration
 * 00048, arity widened by 00050): `fetch_targets()`, which lists what is due across every
 * organisation, and `integration_fetch_context()`, which says whose consent
 * one fetch runs under.
 *
 * A definer function is how RLS gets bypassed by accident, so what this suite
 * pins is who may execute each and what each gives away. The split is the
 * design: the producer role learns WHICH work exists and never how to do it,
 * the application role learns WHOSE authority to assume and then reads
 * everything else under the ordinary two-GUC policy. Each function granted to
 * the other role would collapse that split, which is why the cross grants are
 * asserted absent rather than assumed.
 *
 * What is deliberately NOT here is any change to `kindlast_agent`'s grants:
 * integrations.test.ts already proves the producer role cannot read
 * `credential_ciphertext`, and ENT-279's whole first half is built so that
 * stays true. The behaviour of the filters inside `fetch_targets` (granted
 * only, read-only only, active only, recent attempts suppress) is asserted in
 * apps/core-api/internal/store/postgres/fetch_test.go against the same stack.
 *
 * Proven able to fail: granting the application role execute on
 * `fetch_targets` turns the first group red; revoking the agent's execute
 * turns its listing test red with "permission denied for function".
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  isStackReachable,
  roleUrl,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const AGENT_URL = roleUrl('agent')

const reachable = (await isStackReachable()) && (await agentReachable())

async function agentReachable(): Promise<boolean> {
  try {
    const client = await connect(AGENT_URL)
    await client.end()
    return true
  } catch {
    return false
  }
}

const org = randomUUID()
const ada = randomUUID()
const connection = randomUUID()

let migrator: Client
let agent: Client
let app: Client

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  agent = await connect(AGENT_URL)
  app = await connect(APP_URL)

  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `Scheduled fetch ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, ada],
  )
  await migrator.query(
    `insert into integrations (id, org_id, kind, display_name, endpoint_url)
     values ($1, $2, 'mcp', 'Helpdesk', 'https://tools.example.com/mcp')`,
    [connection, org],
  )
  await migrator.query(
    `insert into integration_tools
       (org_id, integration_id, name, write_capable, granted, granted_at)
     values ($1, $2, 'list_records', false, true, now())`,
    [org, connection],
  )
  await migrator.query(
    `insert into integration_consents
       (org_id, integration_id, consented_by, endpoint_url, granted_tools)
     values ($1, $2, $3, 'https://tools.example.com/mcp', array['list_records'])`,
    [org, connection, ada],
  )
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from organisations where id = $1`, [org])
  await Promise.all([migrator.end(), agent.end(), app.end()])
})

describe.skipIf(!reachable)('only the producer may list fetch targets', () => {
  it('the application role cannot execute it', async () => {
    await app.query(`select set_config('app.current_org_id', $1, false)`, [org])
    await app.query(`select set_config('app.current_user_id', $1, false)`, [
      ada,
    ])
    await expect(
      app.query(
        `select * from public.fetch_targets(interval '24 hours', interval '1 hour', 10)`,
      ),
    ).rejects.toThrow(/permission denied for function/)
  })

  it('the producer role can, with no tenant set, and learns ids and a tool name only', async () => {
    const r = await agent.query(
      `select * from public.fetch_targets(interval '24 hours', interval '1 hour', 1000)`,
    )
    const listed = r.rows.filter(
      (row: { integration_id: string }) => row.integration_id === connection,
    )
    expect(listed).toHaveLength(1)
    expect(listed[0].org_id).toBe(org)
    expect(listed[0].tool).toBe('list_records')
    // Three columns. An endpoint or a credential here would be the producer
    // role learning how to dial rather than what is due.
    expect(r.fields.map((f) => f.name).sort()).toEqual([
      'integration_id',
      'org_id',
      'tool',
    ])
  })
})

describe.skipIf(!reachable)(
  'only the application may read whose consent a fetch runs under',
  () => {
    it('the producer role cannot execute it', async () => {
      await expect(
        agent.query(`select * from public.integration_fetch_context($1)`, [
          connection,
        ]),
      ).rejects.toThrow(/permission denied for function/)
    })

    it('the application role can, before any tenancy is set, which is the point of it', async () => {
      // A fresh session with no GUCs: the policies on `integrations` would
      // show this session nothing, and this function is how it learns which
      // organisation and which person to become.
      const fresh = await connect(APP_URL)
      try {
        const r = await fresh.query(
          `select org_id, consented_by from public.integration_fetch_context($1)`,
          [connection],
        )
        expect(r.rows).toHaveLength(1)
        expect(r.rows[0].org_id).toBe(org)
        expect(r.rows[0].consented_by).toBe(ada)
      } finally {
        await fresh.end()
      }
    })
  },
)
