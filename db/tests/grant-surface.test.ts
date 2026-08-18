/**
 * The grant surface is pinned, so a blanket grant fails a test rather than
 * quietly widening the boundary (ENT-243).
 *
 * WHY THIS SUITE EXISTS
 *
 * `00002` granted `kindlast_app` DML on every table in `public` and set default
 * privileges so every table the migrator created afterwards arrived with the
 * same four commands already attached. A later `grant select, insert` in the
 * migration that creates a table is additive, so it changes nothing: only an
 * explicit `revoke` narrows anything. The result was a schema where the grant
 * a migration appears to make and the grant the role actually holds are two
 * different things, and three migrations plus `db/README.md` described the
 * first while the second was in force.
 *
 * `addressable-but-empty.test.ts` already asks the neighbouring question: does
 * a role hold a grant on a table with no policy at all. That catches a whole
 * table nobody meant to expose. It does not catch the case this suite is for,
 * where a table has policies for two commands and grants for four, because the
 * table passes the whole-table check on the strength of the policies it does
 * have. `audit_log` is exactly that shape: select and insert policies, and a
 * grant covering delete and update as well.
 *
 * THE PROPERTY
 *
 * For every application role, every table, and every command, a grant implies a
 * policy. That is the per-command form of the rule 00015 wrote down when it
 * revoked on `capability_tokens` rather than leaning on a missing policy: a
 * missing grant fails closed at parse time, where a missing policy fails
 * quietly at run time, and the loud version is the one worth having.
 *
 * `kindlast_migrator` is excluded throughout. It owns the schema and is
 * expected to hold everything on everything; narrowing it would stop
 * migrations working.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import { readFile, writeFile } from 'node:fs/promises'
import { connect, isStackReachable, SUPER_URL } from './helpers/db'

const reachable = await isStackReachable()

/** Every role the application layer connects as. The migrator is not one. */
const APPLICATION_ROLES = [
  'kindlast_agent',
  'kindlast_app',
  'kindlast_billing',
  'kindlast_ingest',
  'kindlast_vector_ro',
]

const README = new URL('../README.md', import.meta.url)
const MATRIX_BEGIN = '<!-- begin generated grant matrix -->'
const MATRIX_END = '<!-- end generated grant matrix -->'

let superuser: Client

beforeAll(async () => {
  if (!reachable) return
  superuser = await connect(SUPER_URL)
})

afterAll(async () => {
  if (!reachable) return
  await superuser.end()
})

/**
 * Grants that no policy admits, one row per role, table and command.
 *
 * Both catalogues, and the union is not belt and braces. It is the whole
 * correctness of this function, and the first draft got it wrong:
 *
 *   - `role_table_grants` holds table-level grants and is the only place a
 *     `delete` appears at all. **`delete` is not a column-level privilege in
 *     Postgres**, so `column_privileges` cannot express one. A sweep reading
 *     only that view is structurally blind to every delete grant, which is the
 *     one privilege this whole issue is about. That draft passed, and it could
 *     not have failed.
 *   - `column_privileges` holds the column-level grants, which are grants:
 *     ENT-228 gave the app `update (valid_to)` on `org_profile_facts` and
 *     nothing else, and a column grant with no update policy is the same silent
 *     trap as a table grant with none. It carries no `delete` and no
 *     `truncate`, so it is asked only for the three commands it can hold.
 *
 * Neither view is a superset of the other, and reading one is a test that
 * reports safety it never checked.
 *
 * A policy on PUBLIC (`polroles = '{0}'`, which `pg_policies` renders as the
 * role name `public`) covers every role, and `cmd = 'ALL'` covers every
 * command. Both have to count, or the query invents failures.
 */
async function grantsWithoutAPolicy(): Promise<
  Array<{ role: string; tbl: string; priv: string }>
> {
  const r = await superuser.query(
    `
      with granted as (
        -- Table-level, and the only view carrying DELETE.
        select grantee        as role,
               table_name     as tbl,
               privilege_type as priv
          from information_schema.role_table_grants
         where table_schema = 'public'
           and grantee = any($1::text[])
           and privilege_type in ('SELECT', 'INSERT', 'UPDATE', 'DELETE')
        union
        -- Column-level. This view holds no DELETE, by construction.
        select grantee        as role,
               table_name     as tbl,
               privilege_type as priv
          from information_schema.column_privileges
         where table_schema = 'public'
           and grantee = any($1::text[])
           and privilege_type in ('SELECT', 'INSERT', 'UPDATE')
      ),
      allowed as (
        select tablename as tbl, unnest(roles)::text as role, cmd
          from pg_policies
         where schemaname = 'public'
      )
      select g.role, g.tbl, g.priv
        from granted g
       where not exists (
             select 1
               from allowed a
              where a.tbl = g.tbl
                and (a.role = g.role or a.role = 'public')
                and (a.cmd = g.priv or a.cmd = 'ALL')
           )
       order by g.role, g.tbl, g.priv
    `,
    [APPLICATION_ROLES],
  )
  return r.rows
}

describe.skipIf(!reachable)('a grant implies a policy', () => {
  it('holds for every application role, table and command', async () => {
    const orphans = await grantsWithoutAPolicy()
    expect(
      orphans.map((o) => `${o.role} ${o.tbl} ${o.priv}`),
      'these roles hold a command no policy admits, so the table is empty to ' +
        'them rather than closed to them. Revoke the grant, or add the policy ' +
        'that was intended. See the header of this file.',
    ).toEqual([])
  })
})

/**
 * Every control `db/README.md` states as an absent grant, one row per claim.
 *
 * The sweep above would already catch each of these, but it catches them as
 * one anonymous row inside a list. These are the claims the documentation
 * makes in prose, and a prose claim about a security boundary should turn a
 * test red whose name says which sentence stopped being true. `audit_log` is
 * the one the issue names: `db/README.md` credits the append-only property to
 * the missing delete grant, and that sentence is the load-bearing one behind
 * the promise that nobody, including us, can quietly make a decision
 * disappear.
 *
 * Keep this list and the table in `db/README.md` in step. A row here with no
 * row there is a control nobody can find, and a row there with no row here is
 * a claim nothing checks, which is the failure ENT-243 exists to fix.
 */
const DOCUMENTED_CONTROLS: Array<{
  table: string
  role: string
  absent: string[]
}> = [
  { table: 'audit_log', role: 'kindlast_app', absent: ['DELETE', 'UPDATE'] },
  {
    table: 'transactional_outbox',
    role: 'kindlast_app',
    absent: ['DELETE', 'UPDATE'],
  },
  {
    table: 'notification_outbox',
    role: 'kindlast_app',
    absent: ['DELETE', 'INSERT', 'UPDATE'],
  },
  {
    table: 'findings',
    role: 'kindlast_app',
    absent: ['DELETE', 'INSERT'],
  },
  {
    table: 'watcher_findings',
    role: 'kindlast_app',
    absent: ['DELETE', 'INSERT', 'UPDATE'],
  },
  {
    table: 'product_review_flags',
    role: 'kindlast_app',
    absent: ['DELETE', 'UPDATE'],
  },
  {
    table: 'subscriptions',
    role: 'kindlast_app',
    absent: ['DELETE', 'INSERT', 'UPDATE'],
  },
  {
    table: 'dsar_trail_entries',
    role: 'kindlast_app',
    absent: ['DELETE', 'UPDATE'],
  },
  { table: 'user_identities', role: 'kindlast_app', absent: ['DELETE'] },
  {
    table: 'notification_preferences',
    role: 'kindlast_app',
    absent: ['DELETE'],
  },
  {
    table: 'deadline_alert_log',
    role: 'kindlast_app',
    absent: ['DELETE', 'INSERT', 'UPDATE'],
  },
  {
    table: 'weekly_briefing_log',
    role: 'kindlast_app',
    absent: ['DELETE', 'INSERT', 'UPDATE'],
  },
  {
    table: 'goose_db_version',
    role: 'kindlast_app',
    absent: ['DELETE', 'INSERT', 'SELECT', 'UPDATE'],
  },
  {
    table: 'capability_tokens',
    role: 'kindlast_app',
    absent: ['DELETE', 'INSERT', 'SELECT', 'UPDATE'],
  },
  {
    table: 'billing_webhook_events',
    role: 'kindlast_app',
    absent: ['DELETE', 'INSERT', 'SELECT', 'UPDATE'],
  },
  {
    table: 'org_profile_facts',
    role: 'kindlast_app',
    absent: ['DELETE', 'UPDATE'],
  },
  { table: 'org_evidence', role: 'kindlast_app', absent: ['DELETE', 'UPDATE'] },
  { table: 'audit_evidence', role: 'kindlast_app', absent: ['DELETE', 'UPDATE'] },
]

describe.skipIf(!reachable)('the documented controls are privileges', () => {
  it.each(DOCUMENTED_CONTROLS)(
    '$role holds none of $absent on $table',
    async ({ table, role, absent }) => {
      // Table-level only. A column-level `update (valid_to)` is the ENT-228
      // shape and is deliberately not a table grant, so `org_profile_facts`
      // appears here claiming no table update while holding one column of it.
      const r = await superuser.query(
        `select privilege_type
           from information_schema.role_table_grants
          where table_schema = 'public'
            and table_name = $1
            and grantee = $2
            and privilege_type = any($3::text[])
          order by 1`,
        [table, role, absent],
      )
      expect(
        r.rows.map((row) => row.privilege_type as string),
        `${role} holds a command on ${table} that db/README.md says it does ` +
          'not. Either the grant came back, or the documentation needs to ' +
          'stop claiming a control that is not there.',
      ).toEqual([])
    },
  )
})

/**
 * Tables start closed (the architecture ruling on ENT-243, 2026-08-17).
 *
 * A default privilege is the reason this class of drift recurs by
 * construction: every new table inherits DML and each migration has to
 * remember to revoke. 00015 remembered and 00014 did not. With no default
 * privilege at all, a table has to ask for what it needs.
 */
describe.skipIf(!reachable)('no application role has a default privilege', () => {
  it('so a new table arrives with nothing attached', async () => {
    const r = await superuser.query(
      `select pg_get_userbyid(d.defaclrole) as grantor,
              n.nspname                     as schema,
              d.defaclacl::text             as acl
         from pg_default_acl d
         join pg_namespace n on n.oid = d.defaclnamespace
        where d.defaclacl::text ~ 'kindlast_(app|agent|billing|ingest|vector_ro)'`,
    )
    expect(
      r.rows,
      'a default privilege grants an application role something on every ' +
        'future table, which is how the grant surface widens without anybody ' +
        'writing a grant. Each migration grants what its table needs.',
    ).toEqual([])
  })
})

/**
 * The matrix in `db/README.md` is generated, and this is what makes that true.
 *
 * The failure this issue documents was not that somebody wrote a wrong
 * sentence. It was that a prose claim about grants could drift from the grants
 * with nothing noticing. A hand-written table can always make a claim the
 * database contradicts; one that a test regenerates and compares cannot.
 *
 * To refresh it after a migration changes a grant:
 *
 *     UPDATE_GRANT_MATRIX=1 bun run test:db
 *
 * and commit the diff to `db/README.md` alongside the migration that caused it.
 */
async function grantMatrix(): Promise<string> {
  const r = await superuser.query(
    `
      with tbl as (
        select grantee as role, table_name as tbl, privilege_type as priv
          from information_schema.role_table_grants
         where table_schema = 'public'
           and grantee = any($1::text[])
           and privilege_type in ('SELECT', 'INSERT', 'UPDATE', 'DELETE')
      ),
      col as (
        select grantee as role, table_name as tbl, privilege_type as priv,
               column_name
          from information_schema.column_privileges
         where table_schema = 'public'
           and grantee = any($1::text[])
           and privilege_type in ('SELECT', 'INSERT', 'UPDATE', 'DELETE')
      ),
      -- A command held on some columns but not as a table grant is a
      -- column-level grant, and the columns are the point of it.
      narrowed as (
        select c.role, c.tbl, c.priv,
               string_agg(c.column_name, ', ' order by c.column_name) as cols
          from col c
         where not exists (
               select 1 from tbl t
                where t.role = c.role and t.tbl = c.tbl and t.priv = c.priv
             )
         group by c.role, c.tbl, c.priv
      ),
      merged as (
        select role, tbl, lower(priv) as label from tbl
        union all
        select role, tbl, lower(priv) || ' (' || cols || ')' from narrowed
      )
      select role, tbl,
             string_agg(label, ', ' order by label) as commands
        from merged
       group by role, tbl
       order by role, tbl
    `,
    [APPLICATION_ROLES],
  )

  const header = ['| Role | Table | Commands |', '|---|---|---|']
  const body = r.rows.map(
    (row) => `| \`${row.role}\` | \`${row.tbl}\` | ${row.commands} |`,
  )
  return [...header, ...body].join('\n')
}

describe.skipIf(!reachable)('db/README.md states the grants the database holds', () => {
  it('carries a matrix generated from the catalogue, not typed by hand', async () => {
    const generated = await grantMatrix()
    const readme = await readFile(README, 'utf8')

    const begin = readme.indexOf(MATRIX_BEGIN)
    const end = readme.indexOf(MATRIX_END)
    expect(
      begin >= 0 && end > begin,
      `db/README.md has no generated grant matrix. It is delimited by ` +
        `${MATRIX_BEGIN} and ${MATRIX_END}.`,
    ).toBe(true)

    const committed = readme
      .slice(begin + MATRIX_BEGIN.length, end)
      .trim()

    if (process.env.UPDATE_GRANT_MATRIX === '1' && committed !== generated) {
      await writeFile(
        README,
        readme.slice(0, begin + MATRIX_BEGIN.length) +
          '\n\n' +
          generated +
          '\n\n' +
          readme.slice(end),
        'utf8',
      )
      return
    }

    expect(
      committed,
      'the grant matrix in db/README.md no longer matches the database. Run ' +
        'UPDATE_GRANT_MATRIX=1 bun run test:db and commit the result with the ' +
        'migration that changed a grant.',
    ).toBe(generated)
  })
})
