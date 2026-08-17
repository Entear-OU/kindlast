/**
 * The statutory clock starts when the request arrived, not when it was logged
 * (ENT-224, migration 00012).
 *
 * WHY THIS IS THE TEST THAT MATTERS
 *
 * The bug it guards is not a crash. `log_dsar` recorded a perfectly valid DSAR
 * with a perfectly valid deadline; the deadline was just a month from the wrong
 * day. Nothing fails, nothing warns, and the error favours the organisation:
 * the longer they take to log a request, the more time they appear to have.
 *
 * That is the shape of mistake a compliance product must not make, because a
 * customer relying on the date would miss an Article 12(3) deadline while the
 * console told them they were on track.
 *
 * 00010 fixed exactly this on the executor path. This is the same fix on the
 * path a human uses, which 00010 could not reach because it is in the
 * function's signature rather than its body.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import {
  connect,
  isStackReachable,
  MIGRATOR_URL,
  setTenant,
} from './helpers/db'

const reachable = await isStackReachable()

const ORG = 'a0000000-0000-4000-8000-000000000001'
const USER = 'a0000000-0000-4000-8000-0000000000aa'

let db: Client

beforeAll(async () => {
  if (!reachable) return
  db = await connect(MIGRATOR_URL)
})

afterAll(async () => {
  if (!reachable) return
  await db.end()
})

/**
 * Runs one call inside a transaction that is rolled back.
 *
 * Nothing leaks, and the deliberate failures cannot poison the next assertion:
 * a raise aborts the whole transaction, so each case needs its own.
 */
async function logDsar(receivedAt: string | null): Promise<{
  received: Date
  due: Date
}> {
  await db.query('begin')
  try {
    await setTenant(db, ORG, USER)

    // Two statements, not one. Calling the function inside a `where id = ...`
    // clause returns nothing: the statement's snapshot of `dsars` is taken
    // before the function inserts, so the new row is invisible to the very
    // scan meant to find it. Cost twenty minutes the first time.
    const created = await db.query(
      `select public.log_dsar('A. Requester', 'erasure', 'Privacy team', $1::timestamptz) as id`,
      [receivedAt],
    )
    const { rows } = await db.query(
      `select received_at, response_due_at from dsars where id = $1`,
      [created.rows[0].id],
    )
    return { received: rows[0].received_at, due: rows[0].response_due_at }
  } finally {
    await db.query('rollback')
  }
}

async function logDsarExpectingFailure(receivedAt: string): Promise<string> {
  await db.query('begin')
  try {
    await setTenant(db, ORG, USER)
    await db.query(
      `select public.log_dsar('A. Requester', 'erasure', 'Privacy team', $1::timestamptz)`,
      [receivedAt],
    )
    return ''
  } catch (error) {
    return (error as Error).message
  } finally {
    await db.query('rollback')
  }
}

const DAY = 24 * 60 * 60 * 1000

describe.skipIf(!reachable)('the DSAR clock runs from receipt', () => {
  // The whole point. A request that arrived three weeks ago has nine days left,
  // not thirty.
  it('dates the deadline from when the request arrived, not from today', async () => {
    const arrived = new Date(Date.now() - 21 * DAY)
    const { received, due } = await logDsar(arrived.toISOString())

    expect(Math.abs(received.getTime() - arrived.getTime())).toBeLessThan(1000)

    const daysLeft = Math.round((due.getTime() - Date.now()) / DAY)
    expect(daysLeft).toBe(9)
  })

  // Null is allowed and means today, unlike the executor path where a missing
  // date is a producer bug. Somebody logging a request that came in this
  // morning has nothing to type.
  it('treats an absent date as today', async () => {
    const { received, due } = await logDsar(null)

    expect(Math.abs(received.getTime() - Date.now())).toBeLessThan(60_000)
    expect(Math.round((due.getTime() - received.getTime()) / DAY)).toBe(30)
  })

  // Refused rather than clamped. Clamping would accept a typo and record a
  // deadline nobody chose, which is the silent generosity this fix exists to
  // remove.
  it('refuses a future date rather than quietly accepting it', async () => {
    const tomorrow = new Date(Date.now() + DAY)
    const message = await logDsarExpectingFailure(tomorrow.toISOString())

    expect(message).toMatch(/in the future/i)
    expect(message).toMatch(/log_dsar/)
  })

  // The old three-argument form is dropped rather than left as an overload,
  // because a caller passing three arguments would otherwise bind to whichever
  // Postgres preferred, and the old one still has the bug in it.
  it('no longer offers the three-argument form that stamped today', async () => {
    await db.query('begin')
    try {
      await setTenant(db, ORG, USER)
      let failed = false
      try {
        await db.query(
          `select public.log_dsar('A. Requester', 'erasure', 'Privacy team')`,
        )
      } catch {
        failed = true
      }
      // Four arguments with the fourth defaulted still accepts three at the
      // call site, so this must NOT fail: what matters is that only one
      // function exists to bind to.
      expect(failed).toBe(false)

      const { rows } = await db.query(`
        select count(*)::int as overloads
        from pg_proc p
        join pg_namespace n on n.oid = p.pronamespace
        where n.nspname = 'public' and p.proname = 'log_dsar'
      `)
      expect(rows[0].overloads).toBe(1)
    } finally {
      await db.query('rollback')
    }
  })
})
