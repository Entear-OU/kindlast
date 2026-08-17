/**
 * The billing webhook's write path (ENT-210, migration 00017).
 *
 * WHAT THIS SUITE IS GUARDING
 *
 * An unauthenticated endpoint that changes what a customer is entitled to. The
 * question ENT-210 asks is not whether it works but what it is allowed to
 * touch, and the answer has to survive somebody later reaching for a
 * convenient role.
 *
 * Four properties:
 *
 *   1. The application still cannot write a subscription. A request handler
 *      that could would let a caller grant their own organisation a plan, and
 *      the failure is invisible because the caller sees exactly what they
 *      wanted.
 *   2. The dedup ledger cannot be rewritten. The whole idempotency property is
 *      that seeing an event id twice means the second is a replay, so an actor
 *      that can delete a row can replay anything.
 *   3. A cancelled subscription cannot be deleted, only marked. A deleted row
 *      reads as a customer who never paid.
 *   4. The billing role holds nothing adjacent. If it can read findings or
 *      memberships it is kindlast_agent with extra steps, and the reason for a
 *      fifth role evaporates.
 *
 * None of these fails loudly if it regresses. Each one produces a system that
 * works perfectly for everyone acting in good faith.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { randomUUID } from 'node:crypto'
import type { Client } from 'pg'
import {
  connect,
  setTenant,
  isStackReachable,
  MIGRATOR_URL,
  APP_URL,
} from './helpers/db'

const reachable = await isStackReachable()

// The webhook's own role. A fifth role rather than kindlast_agent, because
// granting the agent subscription writes would make it a role that can invent a
// finding AND grant itself a paid plan: a new capability rather than a wider
// read. See 00017's header.
const BILLING_URL =
  process.env.PG_BILLING_URL ??
  'postgres://kindlast_billing:billing-dev-password@127.0.0.1:5433/kindlast'

const AGENT_URL =
  process.env.PG_AGENT_URL ??
  'postgres://kindlast_agent:agent-dev-password@127.0.0.1:5433/kindlast'

const org = randomUUID()
const ada = randomUUID()
const eventID = `evt_${randomUUID()}`
const customerID = `cus_${randomUUID().slice(0, 12)}`

let migrator: Client
let app: Client
let billing: Client
let agent: Client

async function refused(c: Client, sql: string, params: unknown[] = []) {
  await c.query('begin')
  try {
    await c.query(sql, params)
    return null
  } catch (error) {
    return error as Error & { code?: string }
  } finally {
    await c.query('rollback')
  }
}

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
  app = await connect(APP_URL)
  billing = await connect(BILLING_URL)
  agent = await connect(AGENT_URL)

  await migrator.query(
    `insert into organisations (id, name, slug) values ($1, $2, org_slug($2))`,
    [org, `Billing Fixture ${org.slice(0, 8)}`],
  )
  await migrator.query(
    `insert into memberships (org_id, user_id, role) values ($1, $2, 'owner')`,
    [org, ada],
  )
})

afterAll(async () => {
  if (!reachable) return
  await migrator.query(
    `delete from billing_webhook_events where event_id = $1`,
    [eventID],
  )
  await migrator.query(`delete from subscriptions where org_id = $1`, [org])
  await migrator.query(`delete from memberships where org_id = $1`, [org])
  await migrator.query(`delete from organisations where id = $1`, [org])
  await migrator.end()
  await app.end()
  await billing.end()
  await agent.end()
})

describe.skipIf(!reachable)(
  'the webhook can record what the provider says',
  () => {
    it('writes a subscription for the organisation it resolved, and only that one', async () => {
      // The real handler shape, in order, because the ordering is the security
      // property rather than an implementation detail.
      //
      // A provider event says "customer cus_x changed". Which organisation that
      // is is the answer rather than the question, so the handler cannot set the
      // org GUC before it has looked. It resolves first through an unscoped
      // select, then sets the GUC, then writes under org-equality policies.
      await billing.query(`set local role none`).catch(() => {})

      // 1. Seed the customer mapping as the migrator, standing in for an earlier
      //    checkout that created the row.
      await migrator.query(
        `insert into subscriptions (org_id, plan, status, provider, provider_customer_id)
       values ($1, 'free', 'active', 'stripe', $2)`,
        [org, customerID],
      )

      // 2. Resolve, with no GUC set. This is the one unscoped read the role has.
      const found = await billing.query(
        `select org_id from subscriptions where provider_customer_id = $1`,
        [customerID],
      )
      expect(
        found.rows,
        'the webhook could not resolve its customer',
      ).toHaveLength(1)
      expect(found.rows[0].org_id).toBe(org)

      // 3. Scope to what it resolved, then write.
      await billing.query(
        `select set_config('app.current_org_id', $1, false)`,
        [org],
      )
      const updated = await billing.query(
        `update subscriptions set plan = 'pro' where org_id = $1 returning plan`,
        [org],
      )
      expect(updated.rows[0].plan).toBe('pro')
    })

    it('cannot write an organisation other than the one it scoped to', async () => {
      // The half that makes the unscoped lookup safe. Having resolved one
      // customer, the same connection in the same transaction cannot then write
      // somebody else's row, so a handler bug or a malicious payload cannot turn
      // one event into an upgrade for a different tenant.
      await billing.query(
        `select set_config('app.current_org_id', $1, false)`,
        [org],
      )

      const other = await migrator.query(
        `select org_id from subscriptions where org_id <> $1 limit 1`,
        [org],
      )
      if (other.rows.length === 0) {
        throw new Error(
          'no other organisation seeded; this test would prove nothing',
        )
      }

      const updated = await billing.query(
        `update subscriptions set plan = 'pro' where org_id = $1`,
        [other.rows[0].org_id],
      )
      // Zero rows rather than an error: an org-equality policy filters rather
      // than raises, which is why this asserts the row count.
      expect(
        updated.rowCount,
        'the webhook wrote an organisation it had not scoped to',
      ).toBe(0)
    })

    it('records an event id so a replay can be recognised', async () => {
      await billing.query(
        `insert into billing_webhook_events (event_id) values ($1)`,
        [eventID],
      )

      // The primary key is the idempotency mechanism. A retried delivery hits
      // this and the handler stops, rather than applying an upgrade twice.
      const error = await refused(
        billing,
        `insert into billing_webhook_events (event_id) values ($1)`,
        [eventID],
      )
      expect(error, 'the same event id was accepted twice').not.toBeNull()
      expect(error?.code).toBe('23505')
    })

    it('can move a subscription to canceled', async () => {
      await billing.query(
        `select set_config('app.current_org_id', $1, false)`,
        [org],
      )
      const r = await billing.query(
        `update subscriptions set status = 'canceled' where org_id = $1 returning status`,
        [org],
      )
      expect(r.rows[0].status).toBe('canceled')

      // Put it back for the reads below.
      await billing.query(
        `update subscriptions set status = 'active' where org_id = $1`,
        [org],
      )
    })
  },
)

describe.skipIf(!reachable)('and cannot do the things it must not', () => {
  it('cannot delete a dedup row, because that would permit a replay', async () => {
    const error = await refused(
      billing,
      `delete from billing_webhook_events where event_id = $1`,
      [eventID],
    )
    expect(
      error,
      'the dedup ledger is deletable, so replays are possible',
    ).not.toBeNull()
    expect(error?.code).toBe('42501')
  })

  it('cannot rewrite a dedup row either', async () => {
    const error = await refused(
      billing,
      `update billing_webhook_events set processed_at = now() where event_id = $1`,
      [eventID],
    )
    expect(error).not.toBeNull()
    expect(error?.code).toBe('42501')
  })

  it('cannot delete a subscription, only mark it', async () => {
    // A deleted row reads as a customer who never paid. Billing history is part
    // of the record.
    const error = await refused(
      billing,
      `delete from subscriptions where org_id = $1`,
      [org],
    )
    expect(error, 'a subscription was deletable').not.toBeNull()
    expect(error?.code).toBe('42501')
  })

  it('still holds nothing on organisations or memberships', async () => {
    // The whole argument for a fifth role. If it can read these, it is the
    // agent with extra steps, and the blast-radius reasoning in 00017's header
    // is worthless.
    for (const table of [
      'organisations',
      'memberships',
      'user_identities',
      'findings',
    ]) {
      const error = await refused(billing, `select 1 from ${table} limit 1`)
      expect(error, `the billing role can read ${table}`).not.toBeNull()
      expect(error?.code).toBe('42501')
    }
  })
})

describe.skipIf(!reachable)(
  'and the application still cannot grant itself a plan',
  () => {
    it('cannot insert a subscription', async () => {
      // The failure this prevents is invisible: a handler that could write here
      // would let a caller upgrade their own organisation and everything would
      // look correct to them.
      await setTenant(app, org, ada)
      const error = await refused(
        app,
        `insert into subscriptions (org_id, plan, status) values ($1, 'pro', 'active')`,
        [randomUUID()],
      )
      expect(error, 'the application created a subscription').not.toBeNull()
    })

    it('cannot upgrade an existing one', async () => {
      await setTenant(app, org, ada)
      const updated = await app.query(
        `update subscriptions set plan = 'pro' where org_id = $1`,
        [org],
      )
      // No update policy for kindlast_app, so this matches zero rows rather than
      // raising. Asserted on the row count for exactly that reason: a test
      // looking only for an exception would pass against a table that granted it.
      expect(updated.rowCount, 'the application updated a subscription').toBe(0)
    })

    it('reads its own organisation plan and no other', async () => {
      await setTenant(app, org, ada)
      const mine = await app.query(
        `select plan from subscriptions where org_id = $1`,
        [org],
      )
      expect(mine.rows).toHaveLength(1)

      const others = await app.query(
        `select count(*)::int as n from subscriptions where org_id <> $1`,
        [org],
      )
      expect(others.rows[0].n).toBe(0)
    })

    it('cannot see the dedup ledger at all', async () => {
      // Zero policies and no grant. Provider conversation state is infrastructure
      // rather than customer data, and an application that could read it learns
      // when other customers changed plan.
      await setTenant(app, org, ada)
      const error = await refused(
        app,
        `select 1 from billing_webhook_events limit 1`,
      )
      expect(
        error,
        'the application can read webhook dedup state',
      ).not.toBeNull()
    })
  },
)
