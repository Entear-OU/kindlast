/**
 * Where an organisation's model runs, and what the database refuses (ENT-236).
 *
 * # THE PROPERTY THAT IS NOT ISOLATION
 *
 * Isolation is asserted here too, because this table holds a provider API key
 * and one organisation reading another's would be the worst kind of tenancy
 * bug. But the interesting property is a different one, and it is the reason
 * this table exists rather than a settings row:
 *
 *   POINTING AN ORGANISATION AT A HOSTED PROVIDER IS A DECISION THAT CANNOT
 *   BE EDITED AWAY. The provider, the endpoint and the model are not
 *   updatable by the application at all: moving is revoking one row and
 *   inserting another, so the sequence of rows is the history of where this
 *   customer's compliance data has been processed.
 *
 * Four independent things enforce that, and each is asserted separately
 * because each covers a caller the others do not:
 *
 *   1. A COLUMN-LEVEL GRANT. `kindlast_app` holds update on the status,
 *      revocation and credential columns and on nothing else, so the endpoint
 *      cannot be repointed in place with every other check still passing.
 *   2. A PARTIAL UNIQUE INDEX. One active row per organisation, so "switch
 *      provider" cannot quietly become "two providers, and whichever the
 *      query happened to order first".
 *   3. A CHECK CONSTRAINT. A revoked row holds no ciphertext, so reverting to
 *      the bundled model destroys the key rather than parking it.
 *   4. NO DELETE FOR ANYBODY. The record of a decision outlives the decision.
 *
 * Remove any one and the others still look green from one side, which is why
 * they are separate properties rather than one.
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

/** Point an organisation at a hosted provider, as the application would. */
async function chooseHosted(
  client: Client,
  org: string,
  provider = 'openai',
): Promise<string> {
  const r = await client.query(
    `insert into org_model_config
       (org_id, provider, base_url, model,
        credential_ciphertext, credential_key_id, credential_last_four,
        created_by)
     values ($1, $2, 'https://api.example.com', 'gpt-oss-120b',
             '\\xdeadbeef'::bytea, '2026-08', 'ab12', $3)
     returning id`,
    [org, provider, ada],
  )
  return r.rows[0].id
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  agent = await connect(AGENT_URL)
  await seedOrg(orgA, 'Model A')
  await seedOrg(orgB, 'Model B')
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

describe.skipIf(!reachable)('the choice is a record, not a setting', () => {
  it('refuses to repoint an existing choice at another endpoint', async () => {
    const id = await chooseHosted(
      app,
      orgA,
      `openai-${randomUUID().slice(0, 8)}`,
    )

    await expect(
      app.query(
        `update org_model_config set base_url = 'https://elsewhere.example.com'
           where id = $1`,
        [id],
      ),
    ).rejects.toThrow(/permission denied/i)

    await expect(
      app.query(
        `update org_model_config set provider = 'somebody-else' where id = $1`,
        [id],
      ),
    ).rejects.toThrow(/permission denied/i)

    await expect(
      app.query(
        `update org_model_config set model = 'another-model' where id = $1`,
        [id],
      ),
    ).rejects.toThrow(/permission denied/i)

    // And the row still says what it said.
    const after = await app.query(
      `select base_url, model from org_model_config where id = $1`,
      [id],
    )
    expect(after.rows[0].base_url).toBe('https://api.example.com')

    await migrator.query(`delete from org_model_config where id = $1`, [id])
  })

  it('allows one active choice per organisation and no more', async () => {
    const first = await chooseHosted(
      app,
      orgA,
      `first-${randomUUID().slice(0, 8)}`,
    )

    await expect(
      chooseHosted(app, orgA, `second-${randomUUID().slice(0, 8)}`),
    ).rejects.toThrow(/duplicate key|unique/i)

    // Revoking the first frees the slot, which is what makes switching a
    // sequence of decisions rather than an edit.
    await app.query(
      `update org_model_config
          set status = 'revoked', revoked_at = now(), revoked_by = $2,
              credential_ciphertext = null, credential_key_id = null
        where id = $1`,
      [first, ada],
    )

    const second = await chooseHosted(
      app,
      orgA,
      `second-${randomUUID().slice(0, 8)}`,
    )
    expect(second).toBeTruthy()

    await migrator.query(`delete from org_model_config where org_id = $1`, [
      orgA,
    ])
  })

  it('refuses to keep a credential on a revoked choice', async () => {
    const id = await chooseHosted(
      app,
      orgA,
      `keeper-${randomUUID().slice(0, 8)}`,
    )

    await expect(
      app.query(
        `update org_model_config set status = 'revoked', revoked_at = now() where id = $1`,
        [id],
      ),
    ).rejects.toThrow(/org_model_config_revoked_holds_no_credential/i)

    await migrator.query(`delete from org_model_config where id = $1`, [id])
  })

  it('gives nobody a delete, so reverting cannot erase the record', async () => {
    const id = await chooseHosted(
      app,
      orgA,
      `undeletable-${randomUUID().slice(0, 8)}`,
    )

    await expect(
      app.query(`delete from org_model_config where id = $1`, [id]),
    ).rejects.toThrow(/permission denied/i)

    await migrator.query(`delete from org_model_config where id = $1`, [id])
  })
})

describe.skipIf(!reachable)('one organisation never sees another key', () => {
  it('reads nothing of another organisation, ciphertext included', async () => {
    // Seeded by the migrator so it exists independently of what the app role
    // can do, which is the whole point: the row is really there.
    await migrator.query(
      `insert into org_model_config
         (org_id, provider, base_url, model,
          credential_ciphertext, credential_key_id, credential_last_four)
       values ($1, 'openai', 'https://api.example.com', 'gpt-oss-120b',
               '\\xfeedface'::bytea, '2026-08', 'zz99')`,
      [orgB],
    )

    const visible = await app.query(
      `select count(*)::int as n from org_model_config where org_id = $1`,
      [orgB],
    )
    expect(visible.rows[0].n).toBe(0)

    // And the superuser can see it, which proves the assertion above is
    // enforcement rather than an empty table.
    const seeded = await migrator.query(
      `select count(*)::int as n from org_model_config where org_id = $1`,
      [orgB],
    )
    expect(seeded.rows[0].n).toBe(1)

    await migrator.query(`delete from org_model_config where org_id = $1`, [
      orgB,
    ])
  })

  it('refuses to write a choice into an organisation the caller is not in', async () => {
    await expect(chooseHosted(app, orgB)).rejects.toThrow(/row-level security/i)
  })
})

describe.skipIf(!reachable)('the producer role reads what a run needs', () => {
  it('reads the endpoint and the sealed credential, and not who chose it', async () => {
    const id = await chooseHosted(
      app,
      orgA,
      `agentread-${randomUUID().slice(0, 8)}`,
    )

    // 00037 (ENT-272) made `org_model_config_agent` org equality, so the
    // producer says whose endpoint it is resolving before it reads one. It
    // always knew: `ActiveModelChoiceForOrg` takes an org id and wrote it into
    // a `where` clause, and the clause was the only thing scoping the read.
    await agent.query(`select set_config('app.current_org_id', $1, false)`, [
      orgA,
    ])

    const r = await agent.query(
      `select provider, base_url, model, credential_ciphertext, credential_key_id
         from org_model_config where id = $1`,
      [id],
    )
    expect(r.rows[0].provider).toContain('agentread-')
    expect(r.rows[0].credential_key_id).toBe('2026-08')

    // `created_by` is not the producer's business: it decides nothing about a
    // run and naming the columns costs one line.
    await expect(
      agent.query(`select created_by from org_model_config where id = $1`, [
        id,
      ]),
    ).rejects.toThrow(/permission denied/i)

    await migrator.query(`delete from org_model_config where id = $1`, [id])
  })

  it('cannot write a choice at all', async () => {
    await expect(
      agent.query(
        `insert into org_model_config (org_id, provider, base_url, model)
         values ($1, 'openai', 'https://api.example.com', 'm')`,
        [orgA],
      ),
    ).rejects.toThrow(/permission denied/i)
  })
})

describe.skipIf(!reachable)('a run says which provider served it', () => {
  it('defaults an agent run to the instance model rather than guessing', async () => {
    await agent.query(`select set_config('app.current_org_id', $1, false)`, [
      orgA,
    ])
    const r = await agent.query(
      `insert into agent_runs
         (org_id, skill, skill_version, model, model_version,
          outcome, started_at, finished_at)
       values ($1, 'analyst', '1', 'qwen', 'sha256:x', 'succeeded', now(), now())
       returning provider`,
      [orgA],
    )
    expect(r.rows[0].provider).toBe('instance')
    await migrator.query(`delete from agent_runs where org_id = $1`, [orgA])
  })
})
