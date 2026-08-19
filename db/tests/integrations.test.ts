/**
 * Connecting a customer's systems, and what the database refuses (ENT-231).
 *
 * # THE PROPERTY THAT IS NOT ISOLATION
 *
 * Isolation is asserted here too, because these tables hold endpoints and
 * credentials and one organisation seeing another's would be the worst version
 * of a tenancy bug. But the interesting property is a different one:
 *
 *   A CONNECTION'S WRITE-CAPABLE TOOLS ARE UNREACHABLE UNLESS EXPLICITLY
 *   GRANTED, AND THE APPLICATION CANNOT RELABEL A TOOL TO GET ROUND IT.
 *
 * Three independent things enforce it and each is asserted separately, because
 * each covers a caller the others do not:
 *
 *   1. A DEFAULT. `granted` is false, so a discovery insert that says nothing
 *      about a tool produces a tool nobody may call. The lazy path is the safe
 *      one.
 *   2. A COLUMN-LEVEL GRANT. `kindlast_app` holds update on the three grant
 *      columns and NOT on `write_capable`, so a caller cannot relabel a write
 *      tool as read-only and walk it past the gate with every other check
 *      still passing.
 *   3. THE GATEWAY, which refuses independently and is tested in Go. Not
 *      asserted here, and named so the absence is deliberate: a table cannot
 *      stop a request that never asks it.
 *
 * Remove any one and the others still look green from one side. That is why
 * they are tested as separate properties rather than as one.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  isStackReachable,
  setTenant,
  roleUrl,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const reachable = await isStackReachable()

const AGENT_URL = roleUrl('agent')

let migrator: Client
let app: Client
let agent: Client

const orgA = randomUUID()
const orgB = randomUUID()
const ada = randomUUID()

async function seedOrg(org: string, label: string): Promise<void> {
  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `${label} ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, ada],
  )
}

/** Create a connection as the application would, through the app role. */
async function connectSystem(org: string, name: string): Promise<string> {
  const r = await app.query(
    `insert into integrations (org_id, kind, display_name, endpoint_url, created_by)
     values ($1, 'mcp', $2, 'https://tools.example.com/mcp', $3)
     returning id`,
    [org, name, ada],
  )
  return r.rows[0].id
}

/** Record a discovered tool, saying nothing about whether it is granted. */
async function discoverTool(
  org: string,
  connection: string,
  name: string,
  writeCapable: boolean,
): Promise<string> {
  const r = await app.query(
    `insert into integration_tools (org_id, integration_id, name, write_capable)
     values ($1, $2, $3, $4) returning id`,
    [org, connection, name, writeCapable],
  )
  return r.rows[0].id
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  agent = await connect(AGENT_URL)
  await seedOrg(orgA, 'Integrations A')
  await seedOrg(orgB, 'Integrations B')
  await setTenant(app, orgA, ada)
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(`delete from organisations where id in ($1, $2)`, [
    orgA,
    orgB,
  ])
  await Promise.all([migrator.end(), app.end(), agent.end()])
})

describe.skipIf(!reachable)(
  'a tool is unreachable until somebody grants it',
  () => {
    it('defaults every discovered tool to not granted, whatever it can do', async () => {
      const connection = await connectSystem(orgA, `Helpdesk ${randomUUID()}`)
      await discoverTool(orgA, connection, 'search_tickets', false)
      await discoverTool(orgA, connection, 'close_ticket', true)

      const r = await app.query(
        `select name, write_capable, granted, granted_at, granted_by
           from integration_tools where integration_id = $1 order by name`,
        [connection],
      )

      expect(r.rows).toHaveLength(2)
      for (const row of r.rows) {
        expect(row.granted, `${row.name} granted`).toBe(false)
        expect(row.granted_at, `${row.name} granted_at`).toBeNull()
        expect(row.granted_by, `${row.name} granted_by`).toBeNull()
      }
    })

    it('refuses to relabel a write-capable tool, by privilege', async () => {
      const connection = await connectSystem(orgA, `Relabel ${randomUUID()}`)
      await discoverTool(orgA, connection, 'close_ticket', true)

      // Not "no rows updated" and not a policy refusal. A PERMISSION error,
      // because the grant never included this column. The distinction is the
      // whole point: a policy can be widened by a later migration that looks
      // reasonable, where a column-level grant has to be widened on purpose.
      await expect(
        app.query(
          `update integration_tools set write_capable = false
            where integration_id = $1 and name = 'close_ticket'`,
          [connection],
        ),
      ).rejects.toThrow(/permission denied/i)
    })

    it('refuses to rename a tool, which would move a grant onto another', async () => {
      const connection = await connectSystem(orgA, `Rename ${randomUUID()}`)
      await discoverTool(orgA, connection, 'search_tickets', false)

      await expect(
        app.query(
          `update integration_tools set name = 'close_ticket'
            where integration_id = $1`,
          [connection],
        ),
      ).rejects.toThrow(/permission denied/i)
    })

    it('permits flipping the grant, which is the only edit there is', async () => {
      const connection = await connectSystem(orgA, `Grant ${randomUUID()}`)
      await discoverTool(orgA, connection, 'close_ticket', true)

      const r = await app.query(
        `update integration_tools
            set granted = true, granted_at = now(), granted_by = $2
          where integration_id = $1 and name = 'close_ticket'
          returning granted, write_capable`,
        [connection, ada],
      )
      expect(r.rows[0].granted).toBe(true)
      // And the label it was granted under is unchanged, which is what makes
      // the grant mean something a year later.
      expect(r.rows[0].write_capable).toBe(true)
    })

    it('refuses a grant with no timestamp, so a grant always says when', async () => {
      const connection = await connectSystem(orgA, `Whenless ${randomUUID()}`)
      await discoverTool(orgA, connection, 'close_ticket', true)

      await expect(
        app.query(
          `update integration_tools set granted = true
            where integration_id = $1 and name = 'close_ticket'`,
          [connection],
        ),
      ).rejects.toThrow(/integration_tools_grant_consistent/i)
    })
  },
)

describe.skipIf(!reachable)('a consent record cannot be revised', () => {
  it('refuses an update to a consent, by privilege', async () => {
    const connection = await connectSystem(orgA, `Consent ${randomUUID()}`)
    const r = await app.query(
      `insert into integration_consents
         (org_id, integration_id, consented_by, endpoint_url, granted_tools)
       values ($1, $2, $3, 'https://tools.example.com/mcp', array['search_tickets'])
       returning id`,
      [orgA, connection, ada],
    )
    const consent = r.rows[0].id

    // A consent record that can be edited afterwards is not a consent record.
    await expect(
      app.query(
        `update integration_consents set granted_tools = array['close_ticket']
          where id = $1`,
        [consent],
      ),
    ).rejects.toThrow(/permission denied/i)

    // And it cannot be deleted either, so widening an allow-list leaves the
    // narrower agreement in place to be found.
    await expect(
      app.query(`delete from integration_consents where id = $1`, [consent]),
    ).rejects.toThrow(/permission denied/i)
  })
})

describe.skipIf(!reachable)(
  'the fetch log records refusals and cannot be tidied',
  () => {
    it('stores a refusal with its reason', async () => {
      const connection = await connectSystem(orgA, `Refusals ${randomUUID()}`)

      const r = await app.query(
        `insert into integration_fetches
           (org_id, integration_id, tool, outcome, detail, requested_by)
         values ($1, $2, 'close_ticket', 'refused',
                 'the tool is not granted on this connection', $3)
         returning outcome, detail, redactions`,
        [orgA, connection, ada],
      )
      expect(r.rows[0].outcome).toBe('refused')
      expect(r.rows[0].detail).toMatch(/not granted/)
      // Zero is a fact rather than a missing value: it says the redactor ran.
      expect(r.rows[0].redactions).toBe(0)
    })

    it('refuses a refusal that says nothing', async () => {
      const connection = await connectSystem(orgA, `Silent ${randomUUID()}`)

      await expect(
        app.query(
          `insert into integration_fetches (org_id, integration_id, tool, outcome)
           values ($1, $2, 'close_ticket', 'refused')`,
          [orgA, connection],
        ),
      ).rejects.toThrow(/integration_fetches_detail_present/i)
    })

    it('refuses evidence on anything but a success', async () => {
      const connection = await connectSystem(orgA, `Evidence ${randomUUID()}`)
      const evidence = await app.query(
        `insert into org_evidence (org_id, source, connection_id, observed_at, kind)
         values ($1, 'integration', $2, now(), 'integration.search_tickets')
         returning id`,
        [orgA, connection],
      )

      await expect(
        app.query(
          `insert into integration_fetches
             (org_id, integration_id, tool, outcome, detail, evidence_id)
           values ($1, $2, 'search_tickets', 'failed', 'the endpoint timed out', $3)`,
          [orgA, connection, evidence.rows[0].id],
        ),
      ).rejects.toThrow(/integration_fetches_evidence_only_on_success/i)
    })

    it('cannot be updated or deleted by the application', async () => {
      const connection = await connectSystem(orgA, `Immutable ${randomUUID()}`)
      const r = await app.query(
        `insert into integration_fetches
           (org_id, integration_id, tool, outcome, detail)
         values ($1, $2, 'close_ticket', 'refused', 'not granted')
         returning id`,
        [orgA, connection],
      )

      await expect(
        app.query(
          `update integration_fetches set outcome = 'succeeded' where id = $1`,
          [r.rows[0].id],
        ),
      ).rejects.toThrow(/permission denied/i)
      await expect(
        app.query(`delete from integration_fetches where id = $1`, [
          r.rows[0].id,
        ]),
      ).rejects.toThrow(/permission denied/i)
    })
  },
)

describe.skipIf(!reachable)('an observation names where it came from', () => {
  it('refuses evidence pointing at a connection that does not exist', async () => {
    // The foreign key 00020 could not add, because this table did not exist.
    // Deferred to commit, so the refusal arrives at COMMIT rather than at the
    // INSERT: assert inside an explicit transaction so the failure is visible.
    await app.query('begin')
    await app.query(
      `insert into org_evidence (org_id, source, connection_id, observed_at, kind)
       values ($1, 'integration', $2, now(), 'integration.invented')`,
      [orgA, randomUUID()],
    )
    await expect(app.query('commit')).rejects.toThrow(
      /org_evidence_connection_fk/i,
    )
    await app.query('rollback').catch(() => undefined)
  })

  it('links an audit row to the evidence a decision rested on', async () => {
    const connection = await connectSystem(orgA, `Audited ${randomUUID()}`)
    const evidence = await app.query(
      `insert into org_evidence (org_id, source, connection_id, observed_at, kind)
       values ($1, 'integration', $2, now(), 'integration.search_tickets')
       returning id`,
      [orgA, connection],
    )

    // An audit row as the executor writes one.
    const audit = await migrator.query(
      `insert into audit_log
         (org_id, user_id, action_type, target_table, approving_user_id)
       values ($1, $2, 'review', 'findings', $2) returning id`,
      [orgA, ada],
    )

    await app.query(
      `insert into audit_evidence (org_id, audit_id, evidence_id)
       values ($1, $2, $3)`,
      [orgA, audit.rows[0].id, evidence.rows[0].id],
    )

    const linked = await app.query(
      `select e.kind from audit_evidence ae
         join org_evidence e on e.id = ae.evidence_id
        where ae.audit_id = $1`,
      [audit.rows[0].id],
    )
    expect(linked.rows.map((r) => r.kind)).toEqual([
      'integration.search_tickets',
    ])
  })

  it('refuses a link to an observation that does not exist', async () => {
    const audit = await migrator.query(
      `insert into audit_log
         (org_id, user_id, action_type, target_table, approving_user_id)
       values ($1, $2, 'review', 'findings', $2) returning id`,
      [orgA, ada],
    )

    // The reason this is a junction table rather than a uuid[] column: an
    // array of ids is a set of claims that some row somewhere exists, and
    // Postgres 17 has no array foreign key to check them.
    await expect(
      app.query(
        `insert into audit_evidence (org_id, audit_id, evidence_id)
         values ($1, $2, $3)`,
        [orgA, audit.rows[0].id, randomUUID()],
      ),
    ).rejects.toThrow(/audit_evidence_evidence_id_fkey|foreign key/i)
  })
})

describe.skipIf(!reachable)('one organisation cannot see another', () => {
  it('shows no connection, tool, consent or fetch belonging to the other', async () => {
    const mine = await connectSystem(orgA, `Mine ${randomUUID()}`)
    await discoverTool(orgA, mine, 'search_tickets', false)

    // Seeded as the migrator, which bypasses RLS, so orgB's rows exist without
    // the app role ever having been able to write them.
    const theirs = randomUUID()
    await migrator.query(
      `insert into integrations (id, org_id, kind, display_name, endpoint_url)
       values ($1, $2, 'mcp', $3, 'https://theirs.example.com/mcp')`,
      [theirs, orgB, `Theirs ${randomUUID()}`],
    )
    await migrator.query(
      `insert into integration_tools (org_id, integration_id, name, write_capable)
       values ($1, $2, 'their_tool', true)`,
      [orgB, theirs],
    )
    await migrator.query(
      `insert into integration_fetches (org_id, integration_id, tool, outcome, detail)
       values ($1, $2, 'their_tool', 'refused', 'not granted')`,
      [orgB, theirs],
    )

    // The app connection is tenanted to orgA.
    const connections = await app.query(
      `select id from integrations where id = $1`,
      [theirs],
    )
    expect(connections.rows).toHaveLength(0)

    const tools = await app.query(
      `select name from integration_tools where name = 'their_tool'`,
    )
    expect(tools.rows).toHaveLength(0)

    const fetches = await app.query(
      `select id from integration_fetches where integration_id = $1`,
      [theirs],
    )
    expect(fetches.rows).toHaveLength(0)

    // And the migrator, which bypasses RLS, sees both. That is what proves the
    // reads above are enforcement rather than absence of data.
    const all = await migrator.query(
      `select id from integrations where id in ($1, $2)`,
      [mine, theirs],
    )
    expect(all.rows).toHaveLength(2)
  })

  it('refuses to create a connection in another organisation', async () => {
    await expect(
      app.query(
        `insert into integrations (org_id, kind, display_name, endpoint_url)
         values ($1, 'mcp', $2, 'https://tools.example.com/mcp')`,
        [orgB, `Smuggled ${randomUUID()}`],
      ),
    ).rejects.toThrow(/row-level security/i)
  })

  it('refuses to move a connection into another organisation', async () => {
    const mine = await connectSystem(orgA, `Movable ${randomUUID()}`)

    // `with check` on the update policy, not just `using`. Without it this is
    // a tenancy escape written as an update, and the read above would still
    // look correct.
    await expect(
      app.query(`update integrations set org_id = $2 where id = $1`, [
        mine,
        orgB,
      ]),
    ).rejects.toThrow(/permission denied|row-level security/i)
  })
})

describe.skipIf(!reachable)(
  'the producer role reads and writes nothing',
  () => {
    it('cannot read a stored credential', async () => {
      const connection = await connectSystem(orgA, `Sealed ${randomUUID()}`)
      await migrator.query(
        `update integrations
          set credential_ciphertext = $2, credential_key_id = 'test'
        where id = $1`,
        [connection, Buffer.from('sealed-bytes')],
      )

      // Column-level select. The agent holds no key, so a ciphertext would buy
      // an attacker little; naming the columns costs one line and removes the
      // argument entirely.
      await expect(
        agent.query(`select credential_ciphertext from integrations`),
      ).rejects.toThrow(/permission denied/i)

      const readable = await agent.query(
        `select display_name from integrations where id = $1`,
        [connection],
      )
      expect(readable.rows).toHaveLength(1)
    })

    it('cannot create, grant or revoke anything', async () => {
      const connection = await connectSystem(orgA, `AgentWrite ${randomUUID()}`)

      await expect(
        agent.query(
          `insert into integrations (org_id, kind, display_name, endpoint_url)
         values ($1, 'mcp', 'agent made this', 'https://x.example.com/mcp')`,
          [orgA],
        ),
      ).rejects.toThrow(/permission denied/i)

      await expect(
        agent.query(
          `update integration_tools set granted = true where integration_id = $1`,
          [connection],
        ),
      ).rejects.toThrow(/permission denied/i)

      await expect(
        agent.query(
          `update integrations set status = 'revoked' where id = $1`,
          [connection],
        ),
      ).rejects.toThrow(/permission denied/i)
    })
  },
)

describe.skipIf(!reachable)('revocation is terminal and recorded', () => {
  it('records when and by whom, and refuses a revocation with no timestamp', async () => {
    const connection = await connectSystem(orgA, `Revoked ${randomUUID()}`)

    await expect(
      app.query(`update integrations set status = 'revoked' where id = $1`, [
        connection,
      ]),
    ).rejects.toThrow(/integrations_revocation_consistent/i)

    const r = await app.query(
      `update integrations
          set status = 'revoked', revoked_at = now(), revoked_by = $2
        where id = $1
        returning status, revoked_at`,
      [connection, ada],
    )
    expect(r.rows[0].status).toBe('revoked')
    expect(r.rows[0].revoked_at).not.toBeNull()
  })

  it('refuses to move a consented connection to a different host', async () => {
    const connection = await connectSystem(orgA, `Moved ${randomUUID()}`)

    // Not a policy refusal: a PERMISSION one, because `endpoint_url` is absent
    // from the column-level update grant. A connection whose endpoint could be
    // edited in place would let somebody point a consented connection at a
    // different host with no new consent, which is the consent mechanism
    // defeated by a single UPDATE.
    await expect(
      app.query(
        `update integrations set endpoint_url = 'https://elsewhere.example.com/mcp'
          where id = $1`,
        [connection],
      ),
    ).rejects.toThrow(/permission denied/i)
  })
})

describe.skipIf(!reachable)('erasing an organisation takes it all', () => {
  it('leaves no connection, tool, consent, fetch or observation behind', async () => {
    const doomed = randomUUID()
    await seedOrg(doomed, 'Doomed')
    await setTenant(app, doomed, ada)

    const connection = await connectSystem(doomed, `Doomed ${randomUUID()}`)
    await discoverTool(doomed, connection, 'search_tickets', false)
    const evidence = await app.query(
      `insert into org_evidence (org_id, source, connection_id, observed_at, kind)
       values ($1, 'integration', $2, now(), 'integration.search_tickets')
       returning id`,
      [doomed, connection],
    )
    await app.query(
      `insert into integration_fetches
         (org_id, integration_id, tool, outcome, evidence_id)
       values ($1, $2, 'search_tickets', 'succeeded', $3)`,
      [doomed, connection, evidence.rows[0].id],
    )
    await app.query(
      `insert into integration_consents
         (org_id, integration_id, consented_by, endpoint_url)
       values ($1, $2, $3, 'https://tools.example.com/mcp')`,
      [doomed, connection, ada],
    )

    // Erasure is one statement. That both cascade branches resolve at all is
    // the point of the deferred foreign key on org_evidence.connection_id:
    // Postgres promises no order between them, so an immediate check could see
    // an observation pointing at a connection that has already gone.
    await migrator.query(`delete from organisations where id = $1`, [doomed])

    for (const table of [
      'integrations',
      'integration_tools',
      'integration_consents',
      'integration_fetches',
      'org_evidence',
    ]) {
      const r = await migrator.query(
        `select count(*)::int as n from ${table} where org_id = $1`,
        [doomed],
      )
      expect(r.rows[0].n, `${table} rows left after erasure`).toBe(0)
    }

    await setTenant(app, orgA, ada)
  })
})
