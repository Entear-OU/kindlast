/**
 * The application role cannot change the schema (ENT-192 acceptance
 * criterion): no CREATE TABLE, no ALTER TABLE, no TRUNCATE.
 *
 * kindlast_app holds table-level DML grants and nothing else. DDL is the
 * migrator's job, and TRUNCATE is a distinct table privilege that is
 * deliberately not granted (it skips RLS and fires no per-row anything, so an
 * application bug reaching TRUNCATE must be a hard permission error, not a
 * tenant-wide wipe).
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import { connect, setTenant, isStackReachable, APP_URL } from './helpers/db'
import { randomUUID } from 'node:crypto'

const reachable = await isStackReachable()

let app: Client

beforeAll(async () => {
  if (!reachable) return
  app = await connect(APP_URL)
  // GUCs set to prove these are privilege failures, not policy failures.
  await setTenant(app, randomUUID(), randomUUID())
})

afterAll(async () => {
  if (!reachable) return
  await app.end()
})

describe.skipIf(!reachable)('kindlast_app cannot touch the schema', () => {
  it('cannot CREATE TABLE', async () => {
    await expect(
      app.query(`create table smuggled_table (id int)`),
    ).rejects.toThrow(/permission denied/)
  })

  it('cannot ALTER TABLE', async () => {
    await expect(
      app.query(`alter table findings add column smuggled int`),
    ).rejects.toThrow(/must be owner|permission denied/)
  })

  it('cannot TRUNCATE', async () => {
    await expect(app.query(`truncate table findings`)).rejects.toThrow(
      /permission denied/,
    )
  })

  it('cannot DROP TABLE', async () => {
    await expect(app.query(`drop table findings`)).rejects.toThrow(
      /must be owner|permission denied/,
    )
  })

  it('cannot CREATE ROLE or grant itself anything', async () => {
    await expect(app.query(`create role smuggled_role`)).rejects.toThrow(
      /permission denied/,
    )
    await expect(
      app.query(`alter role kindlast_app bypassrls`),
    ).rejects.toThrow(/permission denied/)
  })
})
