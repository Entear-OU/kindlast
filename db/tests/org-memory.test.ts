/**
 * What Kindlast knows about an organisation, and who may change it (ENT-228).
 *
 * # THE PROPERTY THAT IS NOT ISOLATION
 *
 * Most of this suite's siblings prove that one organisation cannot see
 * another's rows, and that is asserted here too. But the interesting property
 * on these two tables is a different one: that a value can be SUPERSEDED and
 * never REWRITTEN.
 *
 * §26.5 says a customer can see, correct, export and erase what we believe
 * about them. Correction is only meaningful if the previous value survives it.
 * A schema where an update overwrites in place answers "what do you think" and
 * cannot answer "what did you think in March, when you produced this finding",
 * which is the question somebody checking a finding is actually asking.
 *
 * Three independent things enforce it, and each is asserted separately below,
 * because each covers a caller the others do not:
 *
 *   1. A COLUMN-LEVEL GRANT. `kindlast_app` holds `update (valid_to)` and not
 *      `update (value)`, so the application physically cannot rewrite a value.
 *   2. A PARTIAL UNIQUE INDEX. At most one open row per key, so a writer
 *      recording a new value must close the old one rather than adding a
 *      second answer.
 *   3. A TRIGGER. A closed row cannot be edited or re-opened by anybody,
 *      including `kindlast_migrator`, which bypasses RLS and holds every grant
 *      and which the first two therefore do not constrain at all.
 *
 * Remove any one and the other two still look green from the application's
 * side. That is why they are tested as three properties rather than as one.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  isStackReachable,
  setTenant,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const reachable = await isStackReachable()

const AGENT_URL =
  process.env.PG_AGENT_URL ??
  'postgres://kindlast_agent:agent-dev-password@127.0.0.1:5433/kindlast'

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

/** Record a fact as the application would, through the app role. */
async function record(
  org: string,
  key: string,
  value: unknown,
  source = 'human',
): Promise<string> {
  const r = await app.query(
    `insert into org_profile_facts (org_id, key, value, source, recorded_by)
     values ($1, $2, $3::jsonb, $4, $5) returning id`,
    [org, key, JSON.stringify(value), source, ada],
  )
  return r.rows[0].id
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  agent = await connect(AGENT_URL)
  await seedOrg(orgA, 'Memory A')
  await seedOrg(orgB, 'Memory B')
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

describe.skipIf(!reachable)('a value is superseded, never rewritten', () => {
  it('refuses an update to the value column, by privilege', async () => {
    const id = await record(orgA, 'has_dpo', 'unsure')

    // Not "no rows updated" and not a policy refusal. A permission error,
    // because the grant never included this column. The distinction matters:
    // a policy can be widened by a later migration that looks reasonable,
    // where a column-level grant has to be widened on purpose.
    await expect(
      app.query(
        `update org_profile_facts set value = '"yes"'::jsonb where id = $1`,
        [id],
      ),
    ).rejects.toThrow(/permission denied/i)
  })

  it('permits closing a fact, which is the only edit there is', async () => {
    const id = await record(orgA, 'has_ropa', 'no')
    const r = await app.query(
      `update org_profile_facts set valid_to = now() where id = $1`,
      [id],
    )
    expect(r.rowCount).toBe(1)
  })

  it('refuses a second open value for the same key', async () => {
    const key = `industry_${randomUUID().slice(0, 8)}`
    await record(orgA, key, 'saas')

    // The correction path is close-then-insert. Skipping the close is not a
    // second opinion, it is two current answers, and the database refuses
    // rather than leaving every reader to pick one.
    await expect(record(orgA, key, 'fintech')).rejects.toThrow(
      /org_profile_facts_one_open_per_key/i,
    )
  })

  it('accepts the new value once the old one is closed', async () => {
    const key = `staff_band_${randomUUID().slice(0, 8)}`
    const first = await record(orgA, key, 'under-50')
    await app.query(
      `update org_profile_facts set valid_to = now() where id = $1`,
      [first],
    )
    const second = await record(orgA, key, '50-250')

    const r = await app.query(
      `select value from org_profile_facts
        where org_id = $1 and key = $2 and valid_to is null`,
      [orgA, key],
    )
    expect(r.rows).toHaveLength(1)
    expect(r.rows[0].value).toBe('50-250')
    expect(second).not.toBe(first)
  })
})

describe.skipIf(!reachable)('history does not move, not even for us', () => {
  it('refuses to edit a closed fact as the migrator', async () => {
    const key = `closed_${randomUUID().slice(0, 8)}`
    const id = await record(orgA, key, 'before')
    await app.query(
      `update org_profile_facts set valid_to = now() where id = $1`,
      [id],
    )

    // AS THE MIGRATOR, WHICH IS THE POINT OF THIS TEST. It bypasses RLS and
    // holds every grant, so neither the policies nor the column-level grant
    // constrain it. Only the trigger does, and "nobody, including us" is the
    // claim this table makes to a customer.
    await expect(
      migrator.query(
        `update org_profile_facts set value = '"after"'::jsonb where id = $1`,
        [id],
      ),
    ).rejects.toThrow(/closed/i)
  })

  it('refuses to rewrite an open fact as the migrator', async () => {
    const key = `openedit_${randomUUID().slice(0, 8)}`
    const id = await record(orgA, key, 'before')

    // The other half of the trigger, and the half the column-level grant
    // cannot cover. The app is stopped by privilege; the migrator holds every
    // privilege, so an in-place rewrite of a CURRENT value is available to it
    // and nothing else refuses it.
    await expect(
      migrator.query(
        `update org_profile_facts set value = '"after"'::jsonb where id = $1`,
        [id],
      ),
    ).rejects.toThrow(/only permitted update is closing/i)
  })
})

describe.skipIf(!reachable)('the profile as of an instant', () => {
  it('reconstructs what a run would have read', async () => {
    const key = `basis_${randomUUID().slice(0, 8)}`
    const first = await record(orgA, key, 'consent')

    // The instant a run would have stamped on `agent_runs.profile_as_of`.
    const asOf = (await app.query(`select now() as t`)).rows[0].t

    await app.query(
      `update org_profile_facts set valid_to = now() where id = $1`,
      [first],
    )
    await record(orgA, key, 'legitimate-interest')

    const then = await app.query(
      `select value from org_profile_facts
        where org_id = $1 and key = $2
          and valid_from <= $3 and (valid_to is null or valid_to > $3)`,
      [orgA, key, asOf],
    )
    const now = await app.query(
      `select value from org_profile_facts
        where org_id = $1 and key = $2 and valid_to is null`,
      [orgA, key],
    )

    // The correction is what the next run reads, and the previous belief is
    // still what an earlier run is shown to have reasoned over.
    expect(then.rows).toHaveLength(1)
    expect(then.rows[0].value).toBe('consent')
    expect(now.rows[0].value).toBe('legitimate-interest')
  })
})

describe.skipIf(!reachable)('one organisation cannot reach another', () => {
  it('does not show B a fact recorded for A', async () => {
    const key = `secret_${randomUUID().slice(0, 8)}`
    await record(orgA, key, 'A only')

    await setTenant(app, orgB, ada)
    const r = await app.query(
      `select count(*)::int as n from org_profile_facts where key = $1`,
      [key],
    )
    expect(r.rows[0].n).toBe(0)
    await setTenant(app, orgA, ada)
  })

  it('refuses to write a fact into another organisation', async () => {
    // The org comes from the row and the policy compares it to the GUC, so
    // this is the middleware-bug case: a caller pointed at A writing a row
    // stamped B is refused rather than quietly stored.
    await expect(record(orgB, 'has_dpo', 'yes')).rejects.toThrow(
      /row-level security/i,
    )
  })

  it('refuses to move a row into another organisation by update', async () => {
    const id = await record(orgA, `moveable_${randomUUID().slice(0, 8)}`, 'x')

    // `with check` on the update policy, and the reason it is there: without
    // it, a tenancy escape is available as an ordinary update. The app cannot
    // change org_id at all here, since the column-level grant covers only
    // valid_to, so this asserts the outer of two locks.
    await expect(
      app.query(`update org_profile_facts set org_id = $1 where id = $2`, [
        orgB,
        id,
      ]),
    ).rejects.toThrow(/permission denied|row-level security/i)
  })
})

describe.skipIf(!reachable)('evidence records what we observed', () => {
  async function observe(org: string, hash: string | null): Promise<string> {
    const r = await app.query(
      `insert into org_evidence
         (org_id, source, kind, observed_at, body, content_hash)
       values ($1, 'integration', 'ropa_export', now(), '{"rows": 3}'::jsonb, $2)
       returning id`,
      [org, hash],
    )
    return r.rows[0].id
  }

  it('allows the same content to be observed twice', async () => {
    const hash = 'a'.repeat(64)
    const first = await observe(orgA, hash)
    const second = await observe(orgA, hash)

    // Deliberately NOT a unique constraint. "The tool still says this in
    // August" is a fact, not a duplicate, and deciding whether two
    // observations are the same thing is Go's call rather than an insert the
    // database silently refuses.
    expect(second).not.toBe(first)
  })

  it('refuses a content hash that is not a digest', async () => {
    await expect(observe(orgA, 'not-a-digest')).rejects.toThrow(
      /org_evidence_content_hash_check/i,
    )
  })

  it('permits recording supersession and nothing else', async () => {
    const older = await observe(orgA, null)
    const newer = await observe(orgA, null)

    const ok = await app.query(
      `update org_evidence set superseded_by = $1 where id = $2`,
      [newer, older],
    )
    expect(ok.rowCount).toBe(1)

    await expect(
      app.query(`update org_evidence set body = '{}'::jsonb where id = $1`, [
        older,
      ]),
    ).rejects.toThrow(/permission denied/i)
  })

  it('refuses a row that supersedes itself', async () => {
    const id = await observe(orgA, null)
    await expect(
      app.query(`update org_evidence set superseded_by = id where id = $1`, [
        id,
      ]),
    ).rejects.toThrow(/not_self_superseding/i)
  })
})

describe.skipIf(!reachable)('the agent reads and does not decide', () => {
  it('reads across organisations without a membership', async () => {
    const key = `agentread_${randomUUID().slice(0, 8)}`
    await record(orgA, key, 'visible')

    // Unconditional by policy, matching agent_runs: the agent runs for
    // organisations nobody is signed in to, so it has no tenancy GUCs to be
    // checked against.
    const r = await agent.query(
      `select count(*)::int as n from org_profile_facts where key = $1`,
      [key],
    )
    expect(r.rows[0].n).toBe(1)
  })

  it('cannot record a fact', async () => {
    // A profile the agent can edit is a profile the customer no longer owns,
    // which is the opposite of what this schema is for.
    await expect(
      agent.query(
        `insert into org_profile_facts (org_id, key, value, source)
         values ($1, 'invented', '"yes"'::jsonb, 'agent')`,
        [orgA],
      ),
    ).rejects.toThrow(/permission denied/i)
  })

  /**
   * ENT-231 NARROWED THIS, AND THE NARROWING IS THE POINT RATHER THAN A
   * WEAKENING.
   *
   * 00020 gave the producer role select and no insert on `org_evidence`, so
   * this used to be a privilege refusal. `IngestService.IngestEvidence` needs
   * to record what a scheduled fetch found for an organisation nobody is
   * signed in to, so 00025 grants insert and adds an org-scoped policy.
   *
   * What survives untouched is 00020's actual argument, which is about what an
   * organisation BELIEVES: the test above still holds, `org_profile_facts` is
   * still select-only for this role, and a profile the agent could edit would
   * still be a profile the customer no longer owns. Observing and believing are
   * the two shapes 00020 separated on purpose, and only the first moved.
   *
   * So the assertion changes from "it cannot write here at all" to "it can only
   * write where its GUC says", which is the property that now does the work.
   */
  it('cannot record an observation for an organisation its GUC does not name', async () => {
    // No tenancy GUC set on this connection at all, which is the state a
    // caller that forgot to set one is in. `current_setting(..., true)` is
    // then null, the policy compares against null, and the insert is refused.
    // The direction matters: a caller naming no organisation must write
    // nothing rather than anything.
    await expect(
      agent.query(
        `insert into org_evidence (org_id, source, kind, observed_at)
         values ($1, 'agent', 'guess', now())`,
        [orgA],
      ),
    ).rejects.toThrow(/row-level security/i)
  })

  it('records an observation for the organisation its GUC does name', async () => {
    // The guard above is only worth having if it can pass, and this is the
    // path IngestEvidence actually takes: set the org, then insert.
    await agent.query("select set_config('app.current_org_id', $1, false)", [
      orgA,
    ])
    const r = await agent.query(
      `insert into org_evidence (org_id, source, kind, observed_at)
       values ($1, 'agent', 'observed', now()) returning id`,
      [orgA],
    )
    expect(r.rows[0].id).toBeTruthy()

    // And it still cannot reach into another organisation with that GUC set,
    // which is what makes the GUC tenancy rather than a label.
    await expect(
      agent.query(
        `insert into org_evidence (org_id, source, kind, observed_at)
         values ($1, 'agent', 'smuggled', now())`,
        [orgB],
      ),
    ).rejects.toThrow(/row-level security/i)

    await agent.query("select set_config('app.current_org_id', '', false)")
  })

  it('still cannot record a belief, which is what 00020 was protecting', async () => {
    // Restated after the change above, so the two are read together: the
    // producer role gained the ability to record what it OBSERVED and gained
    // nothing at all over what the organisation BELIEVES.
    await agent.query("select set_config('app.current_org_id', $1, false)", [
      orgA,
    ])
    await expect(
      agent.query(
        `insert into org_profile_facts (org_id, key, value, source)
         values ($1, 'invented', '"yes"'::jsonb, 'agent')`,
        [orgA],
      ),
    ).rejects.toThrow(/permission denied/i)
    await agent.query("select set_config('app.current_org_id', '', false)")
  })
})

describe.skipIf(!reachable)('the grants say what the comments say', () => {
  it('gives the app update on exactly one column of each table', async () => {
    // Asserted out of the catalogue rather than trusted, because "updated only
    // by typed patches, never free-text rewrite" is a promise until it is a
    // privilege, and a privilege is a thing you can query.
    const r = await migrator.query(
      `select table_name, column_name
         from information_schema.column_privileges
        where grantee = 'kindlast_app'
          and privilege_type = 'UPDATE'
          and table_name in ('org_profile_facts', 'org_evidence')
        order by table_name, column_name`,
    )
    expect(r.rows).toEqual([
      { table_name: 'org_evidence', column_name: 'superseded_by' },
      { table_name: 'org_profile_facts', column_name: 'valid_to' },
    ])
  })

  it('gives nobody a delete, because erasure is not a correction', async () => {
    const r = await migrator.query(
      `select grantee, table_name
         from information_schema.role_table_grants
        where privilege_type = 'DELETE'
          and table_name in ('org_profile_facts', 'org_evidence')
          and grantee <> 'kindlast_migrator'`,
    )
    expect(r.rows).toEqual([])
  })
})

describe.skipIf(!reachable)('erasure takes the memory with it', () => {
  it('leaves no facts or evidence behind when an organisation goes', async () => {
    const doomed = randomUUID()
    await seedOrg(doomed, 'Erasable')
    await setTenant(app, doomed, ada)
    await record(doomed, 'has_dpo', 'no')
    await app.query(
      `insert into org_evidence (org_id, source, kind, observed_at)
       values ($1, 'human', 'note', now())`,
      [doomed],
    )
    await setTenant(app, orgA, ada)

    await migrator.query(`delete from organisations where id = $1`, [doomed])

    // Counted rather than assumed, and counted as the migrator so RLS cannot
    // make an unerased row look absent. That distinction is the whole reason
    // this assertion runs on a bypassing connection.
    const facts = await migrator.query(
      `select count(*)::int as n from org_profile_facts where org_id = $1`,
      [doomed],
    )
    const evidence = await migrator.query(
      `select count(*)::int as n from org_evidence where org_id = $1`,
      [doomed],
    )
    expect(facts.rows[0].n).toBe(0)
    expect(evidence.rows[0].n).toBe(0)
  })
})
