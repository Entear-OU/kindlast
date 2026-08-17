/**
 * A deployment that bills nobody caps nobody (migration 00013).
 *
 * THE BUG THIS GUARDS, WHICH ONLY EXISTS ON THE DEFAULT CONFIGURATION
 *
 * `KINDLAST_BILLING_ENABLED` is false unless set, which is the self-hosted
 * default. core-api honoured it, so the console showed no limit.
 * `ropa_manual_activity_limit()` did not, so it kept returning three.
 *
 * The customer therefore saw no cap, added three activities, and had the fourth
 * refused with a message about a plan their deployment does not sell and they
 * cannot buy. Neither half was wrong on its own, which is why nothing caught it:
 * the bug lived in the disagreement.
 *
 * So both halves are asserted here, against the real function, with the GUC set
 * each way.
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

/** Alpha is on `pro`, Beta on `free`, per deploy/seed. */
const PRO_ORG = 'a0000000-0000-4000-8000-000000000001'
const PRO_USER = 'a0000000-0000-4000-8000-0000000000aa'
const FREE_ORG = 'b0000000-0000-4000-8000-000000000001'
const FREE_USER = 'b0000000-0000-4000-8000-0000000000ba'

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
 * Reads the cap with the billing GUC set to a given value.
 *
 * `null` leaves it unset entirely, which is the case a deployment that never
 * configured anything actually hits, and the one that must not cap.
 */
async function limitWith(
  billing: string | null,
  org: string,
  user: string,
): Promise<number | null> {
  await db.query('begin')
  try {
    await setTenant(db, org, user)
    if (billing !== null) {
      await db.query('select set_config($1, $2, true)', [
        'app.billing_enabled',
        billing,
      ])
    }
    const { rows } = await db.query(
      'select public.ropa_manual_activity_limit() as limit',
    )
    return rows[0].limit
  } finally {
    await db.query('rollback')
  }
}

describe.skipIf(!reachable)(
  'the manual ROPA cap follows the billing flag',
  () => {
    // The self-hosted default, and the case the bug was in. Nothing set it, so
    // nothing is capped.
    it('does not cap when nothing set the flag', async () => {
      expect(await limitWith(null, FREE_ORG, FREE_USER)).toBeNull()
    })

    it('does not cap when billing is explicitly off', async () => {
      expect(await limitWith('off', FREE_ORG, FREE_USER)).toBeNull()
    })

    // The other half. Turning billing on must still cap a free organisation, or
    // this fix would have quietly removed the plan limit for everybody.
    it('caps a free organisation when billing is on', async () => {
      expect(await limitWith('on', FREE_ORG, FREE_USER)).toBe(3)
    })

    // And a paid one is still uncapped, which was true before and must stay true.
    it('does not cap a pro organisation when billing is on', async () => {
      expect(await limitWith('on', PRO_ORG, PRO_USER)).toBeNull()
    })

    // The store writes `on`, but a value set by hand in psql should behave the
    // same way rather than silently reading as off.
    it.each(['true', '1'])('accepts %s as on', async (value) => {
      expect(await limitWith(value, FREE_ORG, FREE_USER)).toBe(3)
    })

    // Anything unrecognised is off, not on. A typo must not switch a cap on for a
    // deployment that sells nothing.
    it('treats an unrecognised value as off', async () => {
      expect(await limitWith('yes-please', FREE_ORG, FREE_USER)).toBeNull()
    })
  },
)
