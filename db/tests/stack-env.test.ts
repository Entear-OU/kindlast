/**
 * The derivation that gives each worktree its own compose stack (ENT-250).
 *
 * WHY THIS FILE IS IN THE DATABASE SUITE. It touches no database and needs no
 * stack, so it is the odd one out here. It lives here anyway because this
 * suite is the thing the derivation exists to protect: `bun run test:db`
 * asserting a schema is only meaningful if it asserted the schema its own
 * branch migrated, and before ENT-250 it could not know. The alternative home,
 * `apps/web/__tests__`, would put a repository-level shell script inside the
 * Next.js app for no reason beyond which vitest project happened to be nearer.
 *
 * THREE PROPERTIES, and the first is the one most likely to be broken by
 * accident later:
 *
 *   1. A single checkout is unchanged. Project `kindlast`, postgres on 5433,
 *      Zitadel on 8300, the edge on 8000. Every instruction in README.md,
 *      docs/self-hosting.md and the Postman collection depends on this, and
 *      nothing else in the repository asserts it.
 *   2. Two worktrees get disjoint ports and different project names, without
 *      talking to each other.
 *   3. One worktree gets the same answer every time, so a stack outlives the
 *      shell that started it and `down` finds what `up` created.
 */
import { describe, it, expect } from 'vitest'
import { execFileSync } from 'node:child_process'
import path from 'node:path'

const SCRIPT = path.resolve(__dirname, '../../scripts/stack-env.sh')

/** The script's `export` output, parsed into a plain object. */
function stackEnv(...args: string[]): Record<string, string> {
  const stdout = execFileSync(SCRIPT, args, { encoding: 'utf8' })
  const env: Record<string, string> = {}
  for (const line of stdout.split('\n')) {
    const match = /^export ([A-Z_0-9]+)='(.*)'$/.exec(line)
    if (match) env[match[1]] = match[2]
  }
  return env
}

const PORT_NAMES = [
  'KINDLAST_PG_APP_PORT',
  'KINDLAST_AUTH_PORT',
  'KINDLAST_MAILPIT_PORT',
  'KINDLAST_REDIS_PORT',
  'KINDLAST_EDGE_PORT',
  'KINDLAST_MODEL_PORT',
  'KINDLAST_INTELLIGENCE_PORT',
  'KINDLAST_TEMPORAL_UI_PORT',
]

describe('scripts/stack-env.sh', () => {
  it('leaves a single checkout on exactly the documented ports', () => {
    const env = stackEnv('--default')

    expect(env.COMPOSE_PROJECT_NAME).toBe('kindlast')
    expect(env.KINDLAST_PG_APP_PORT).toBe('5433')
    expect(env.KINDLAST_AUTH_PORT).toBe('8300')
    expect(env.KINDLAST_MAILPIT_PORT).toBe('8025')
    expect(env.KINDLAST_REDIS_PORT).toBe('6379')
    expect(env.KINDLAST_EDGE_PORT).toBe('8000')
    expect(env.KINDLAST_MODEL_PORT).toBe('8081')
    expect(env.KINDLAST_INTELLIGENCE_PORT).toBe('8090')
    expect(env.KINDLAST_TEMPORAL_UI_PORT).toBe('8233')
  })

  it('leaves the default checkout pointing at ./models, not an absolute path', () => {
    // The weights are a 2.7 GB bind mount. A single checkout must keep the
    // relative path compose already defaults to, or the mount silently moves.
    expect(stackEnv('--default').KINDLAST_MODEL_DIR).toBe('./models')
  })

  it('derives the DSNs the suites read from the same port', () => {
    const env = stackEnv('--derive', '--root', '/tmp/kindlast-worktree-alpha')
    const port = env.KINDLAST_PG_APP_PORT

    expect(env.PG_PORT).toBe(port)
    for (const name of [
      'PG_SUPER_URL',
      'PG_MIGRATOR_URL',
      'PG_APP_URL',
      'PG_AGENT_URL',
      'PG_BILLING_URL',
      'PG_INGEST_URL',
    ]) {
      expect(env[name]).toContain(`127.0.0.1:${port}/kindlast`)
    }
    expect(env.REDIS_ADDR).toBe(`127.0.0.1:${env.KINDLAST_REDIS_PORT}`)
  })

  it('gives two worktrees different projects and no shared port', () => {
    const a = stackEnv('--derive', '--root', '/tmp/kindlast-worktree-alpha')
    const b = stackEnv('--derive', '--root', '/tmp/kindlast-worktree-beta')

    expect(a.COMPOSE_PROJECT_NAME).not.toBe(b.COMPOSE_PROJECT_NAME)

    const portsA = PORT_NAMES.map((n) => a[n])
    const portsB = PORT_NAMES.map((n) => b[n])
    expect(new Set([...portsA, ...portsB]).size).toBe(
      portsA.length + portsB.length,
    )
  })

  it("keeps every worktree's ports out of the other's block and out of the defaults", () => {
    const a = stackEnv('--derive', '--root', '/tmp/kindlast-worktree-alpha')
    const defaults = stackEnv('--default')

    for (const name of PORT_NAMES) {
      const port = Number(a[name])
      // Above every default in compose.yaml and below the range the kernel
      // hands out for outbound connections.
      expect(port).toBeGreaterThanOrEqual(20800)
      expect(port).toBeLessThan(32768)
      expect(a[name]).not.toBe(defaults[name])
    }
  })

  it('answers the same for the same worktree every time', () => {
    const first = stackEnv('--derive', '--root', '/tmp/kindlast-worktree-alpha')
    const again = stackEnv('--derive', '--root', '/tmp/kindlast-worktree-alpha')

    expect(again).toEqual(first)
  })

  it('honours an explicit slot, so a hash collision has a way out', () => {
    const forced = execFileSync(
      SCRIPT,
      ['--derive', '--root', '/tmp/kindlast-worktree-alpha'],
      { encoding: 'utf8', env: { ...process.env, KINDLAST_STACK_SLOT: '7' } },
    )
    // Slot 7 is the seventh block of eight from 20800.
    expect(forced).toContain("export KINDLAST_PG_APP_PORT='20848'")
  })

  it('does not export the slot it resolved, so evaluating it is not sticky', () => {
    // KINDLAST_STACK_SLOT is an input: it forces a slot when a hash collides.
    // Echoing the resolved slot back out under the same name would mean a
    // shell that evaluated this in one worktree, then moved to another and
    // evaluated it again, got the FIRST worktree's ports under the second
    // one's project name. This test exists because that is what it did: the
    // disjointness case above went red the first time the suite was run from
    // an evaluated shell, which is the shell every developer will be in.
    const out = execFileSync(
      SCRIPT,
      ['--derive', '--root', '/tmp/kindlast-worktree-alpha'],
      {
        encoding: 'utf8',
      },
    )
    expect(out).not.toContain('KINDLAST_STACK_SLOT')
  })
})
