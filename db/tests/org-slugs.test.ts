/**
 * Organisation slugs: the derivation, the collision rule, and the one
 * assertion that guards ENT-198's whole reason for existing.
 *
 * Slugs are immutable once minted (§20.1), which is what makes them safe to
 * put in bookmarks and in emailed approval links, and it is also what makes
 * this migration unforgiving: a slug derived from the wrong name is permanent.
 * So the interesting tests here are not "does it lowercase things". They are:
 *
 *   - two organisations that want the same slug both get a working one, and
 *     that has to hold for the case where one name's base already looks like
 *     another name's suffixed form ('Acme 2' vs a second 'Acme')
 *   - a name with nothing routable in it still yields a routable slug, because
 *     an organisation nobody can navigate to is worse than an ugly URL
 *   - the migration REFUSES to run while any personal organisation is still
 *     named after its owner's subject claim, rather than quietly minting a
 *     permanent URL out of an IdP identifier
 *
 * The last one is the acceptance criterion added to the issue on 2026-08-14.
 * It is asserted by watching the migration fail, because an assertion that
 * cannot fail is worse than no assertion (AGENTS.md).
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { randomUUID } from 'node:crypto'
import { Client } from 'pg'
import { connect, isStackReachable, SUPER_URL, MIGRATOR_URL } from './helpers/db'

const reachable = await isStackReachable()

/**
 * The Up half of a migration, with goose's directives stripped.
 *
 * Taking the Up half explicitly rather than the whole file, because from
 * 00003 onwards these migrations have a Down section that really does drop
 * things. Running a file end to end applies it and then reverses it, which
 * fails on the first dependency and would otherwise look like a schema bug.
 */
function migrationSql(file: string): string {
  const raw = readFileSync(resolve(__dirname, '../migrations', file), 'utf8')
  const up = raw.split(/^-- \+goose Down$/m)[0]
  return up
    .split('\n')
    .filter((line) => !line.trimStart().startsWith('-- +goose'))
    .join('\n')
}

/** The schema every case here starts from: everything before slugs existed. */
const PRIOR = ['00001_baseline.sql', '00002_organisations.sql', '00003_identities_and_invitations.sql']

const SLUG_MIGRATION = '00004_org_slugs.sql'

interface Scratch {
  db: Client
  admin: Client
  drop: () => Promise<void>
}

/**
 * A scratch database carried up to the migration under test but not through
 * it, so each case controls what rows exist when slugs are minted.
 */
async function scratchUpToSlugs(): Promise<Scratch> {
  const name = `slug_test_${randomUUID().replace(/-/g, '').slice(0, 12)}`
  const admin = await connect(SUPER_URL)
  await admin.query(`create database ${name} owner kindlast_migrator`)
  const url = new URL(MIGRATOR_URL)
  url.pathname = `/${name}`
  const db = await connect(url.toString())
  for (const file of PRIOR) await db.query(migrationSql(file))
  return {
    db,
    admin,
    drop: async () => {
      await db.end()
      await admin.query(`drop database if exists ${name} (force)`)
      await admin.end()
    },
  }
}

async function newOrg(db: Client, name: string, personalOwner?: string): Promise<string> {
  const id = randomUUID()
  await db.query(
    `insert into organisations (id, name, personal_owner_id) values ($1, $2, $3)`,
    [id, name, personalOwner ?? null],
  )
  return id
}

async function slugOf(db: Client, id: string): Promise<string> {
  const r = await db.query(`select slug from organisations where id = $1`, [id])
  return r.rows[0].slug
}

describe.skipIf(!reachable)('organisation slugs', () => {
  let s: Scratch

  // Named so each assertion reads against a name rather than a uuid.
  const ids: Record<string, string> = {}

  beforeAll(async () => {
    s = await scratchUpToSlugs()

    // created_at drives the collision ordering, so these are inserted in the
    // order their slugs should be handed out.
    ids.acme = await newOrg(s.db, 'Acme Ltd.')
    ids.plain = await newOrg(s.db, 'Acme')
    ids.second = await newOrg(s.db, 'Acme')
    ids.third = await newOrg(s.db, 'ACME!!!')
    // The case a window function gets wrong: this name's own base is the
    // suffixed form the row above is about to be given.
    ids.lookalike = await newOrg(s.db, 'Acme 2')
    ids.symbols = await newOrg(s.db, '///')
    ids.long = await newOrg(s.db, 'A'.repeat(120))
    ids.longToo = await newOrg(s.db, 'a'.repeat(120))

    await s.db.query(migrationSql(SLUG_MIGRATION))
  })

  afterAll(async () => {
    await s?.drop()
  })

  it('derives the slug from the name', async () => {
    expect(await slugOf(s.db, ids.acme)).toBe('acme-ltd')
  })

  it('gives every organisation a slug, and no two the same', async () => {
    const r = await s.db.query(`select slug from organisations`)
    const slugs = r.rows.map((row) => row.slug)
    expect(slugs.every((slug) => typeof slug === 'string' && slug.length > 0)).toBe(true)
    expect(new Set(slugs).size).toBe(slugs.length)
  })

  it('resolves a collision with a numeric suffix, in creation order', async () => {
    expect(await slugOf(s.db, ids.plain)).toBe('acme')
    expect(await slugOf(s.db, ids.second)).toBe('acme-2')
    expect(await slugOf(s.db, ids.third)).toBe('acme-3')
  })

  it('does not hand a name the suffixed slug another name already took', async () => {
    // 'Acme 2' derives 'acme-2', which by then belongs to the second 'Acme'.
    // Both must still resolve, and to different organisations.
    const lookalike = await slugOf(s.db, ids.lookalike)
    expect(lookalike).not.toBe(await slugOf(s.db, ids.second))
    expect(lookalike.startsWith('acme-2')).toBe(true)
  })

  it('still produces a routable slug for a name with nothing routable in it', async () => {
    const slug = await slugOf(s.db, ids.symbols)
    expect(slug).toMatch(/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/)
  })

  it('caps the slug at 63 characters, suffix included', async () => {
    const first = await slugOf(s.db, ids.long)
    const second = await slugOf(s.db, ids.longToo)
    expect(first.length).toBe(63)
    expect(second.length).toBeLessThanOrEqual(63)
    expect(second).not.toBe(first)
  })

  it('enforces the shape in the database, not only in the derivation', async () => {
    // Uppercase, a leading hyphen and a trailing hyphen are each refused, so a
    // future writer that skips the derivation cannot mint an unroutable slug.
    for (const bad of ['Acme', '-acme', 'acme-', 'ac me', '']) {
      await expect(
        s.db.query(`insert into organisations (id, name, slug) values ($1, $2, $3)`, [
          randomUUID(),
          'shape check',
          bad,
        ]),
        `slug ${JSON.stringify(bad)} should be refused`,
      ).rejects.toThrow()
    }
  })

  it('refuses a duplicate slug', async () => {
    await expect(
      s.db.query(`insert into organisations (id, name, slug) values ($1, $2, $3)`, [
        randomUUID(),
        'duplicate',
        'acme',
      ]),
    ).rejects.toThrow()
  })

  it('leaves the slug alone when the organisation is renamed', async () => {
    // The immutability that bookmarks and emailed approval links depend on.
    // Nothing in the schema recomputes it, and this is what says so.
    await s.db.query(`update organisations set name = 'Renamed Entirely' where id = $1`, [
      ids.acme,
    ])
    expect(await slugOf(s.db, ids.acme)).toBe('acme-ltd')
  })
})

/**
 * The ENT-198 backfill criterion, asserted by watching it fail.
 *
 * Before PR #114 a personal organisation was named with the owner's raw
 * Zitadel subject. A slug minted from one of those is a permanent URL built
 * out of an IdP identifier, and immutability means there is no fixing it
 * afterwards. So the migration refuses rather than proceeding, and the remedy
 * is the lazy rename at next sign-in that #114 shipped.
 */
describe.skipIf(!reachable)('the subject-named organisation guard', () => {
  let s: Scratch

  beforeAll(async () => {
    s = await scratchUpToSlugs()
  })

  afterAll(async () => {
    await s?.drop()
  })

  it('refuses to mint slugs while a personal organisation is named after its owner subject', async () => {
    const owner = randomUUID()
    const subject = '386250729179840515'
    await s.db.query(
      `insert into user_identities (user_id, issuer, subject) values ($1, $2, $3)`,
      [owner, 'http://localhost:8300', subject],
    )
    // Named with the raw subject, exactly as the pre-#114 code left it.
    await newOrg(s.db, subject, owner)

    await expect(s.db.query(migrationSql(SLUG_MIGRATION))).rejects.toThrow(
      /named after .*subject/i,
    )
  })

  it('proceeds once that organisation has been renamed', async () => {
    // The lazy rename having run is the only difference. Same database, so
    // this also proves the refusal above was about the name and nothing else.
    await s.db.query(`update organisations set name = 'Ada Lovelace' where name = $1`, [
      '386250729179840515',
    ])
    await s.db.query(migrationSql(SLUG_MIGRATION))

    const r = await s.db.query(`select slug from organisations`)
    expect(r.rows.map((row) => row.slug)).toEqual(['ada-lovelace'])
  })
})
