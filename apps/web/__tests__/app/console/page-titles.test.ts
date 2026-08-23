import { readFileSync, readdirSync } from 'node:fs'
import { join, resolve } from 'node:path'

import type { Metadata } from 'next'
import { describe, it, expect } from 'vitest'

/**
 * Every console page names its section (ENT-269).
 *
 * The bug was that nothing under `/o/{slug}/` set a title, so all of it
 * inherited the root layout's marketing one and eleven open tabs read
 * "Kindlast: AI-Powered GDPR & AI Act Compliance" eleven times. The tab strip
 * was the last place in the console where which organisation you were looking
 * at was ambiguous, which is the exact thing §20.1 puts the slug in the URL to
 * prevent.
 *
 * # WHY THIS WALKS THE FILESYSTEM AND NOT A LIST
 *
 * A guard that reads from a hand-written list of pages proves the members of
 * the list, never the list itself, and the list is the half that rots: the
 * page added next quarter is simply absent from it and the guard stays green
 * while the bug comes back. That failure has a name here, ENT-245, where the
 * scope-declaration test walked a registry a service had never been added to.
 *
 * So the set of pages comes from disk. `import.meta.glob` is resolved by Vite
 * at transform time, and the `readdirSync` walk below cross-checks it, because
 * a glob that silently matched nothing would make every assertion vacuous and
 * this file would report safety it was not providing.
 */
const CONSOLE_PREFIX = '../../../app/(authed)/o/[org]/'

// Vitest runs with the workspace root as the working directory. Resolved from
// there rather than from `import.meta.url`, which the jsdom transform does not
// leave as a file URL.
const APP_ROOT = resolve(process.cwd(), 'app')
const CONSOLE_ROOT = join(APP_ROOT, '(authed)', 'o', '[org]')

// Deliberately broader than the console and filtered in JS. Glob syntax reads
// `(authed)` and `[org]` as pattern operators rather than as the literal
// directory names they are, and a pattern that matches nothing is the failure
// this test is least able to notice.
const modules = import.meta.glob('../../../app/**/page.tsx')

const consolePages = Object.keys(modules)
  .filter((key) => key.startsWith(CONSOLE_PREFIX))
  .sort()

function pagesOnDisk(dir: string, prefix = ''): string[] {
  const found: string[] = []

  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const rel = prefix ? `${prefix}/${entry.name}` : entry.name
    if (entry.isDirectory())
      found.push(...pagesOnDisk(join(dir, entry.name), rel))
    else if (entry.name === 'page.tsx') found.push(rel)
  }

  return found
}

async function titleOf(key: string): Promise<unknown> {
  const mod = (await modules[key]()) as { metadata?: Metadata }
  return mod.metadata?.title
}

describe('the set of console pages under test', () => {
  it('is every page.tsx on disk, so a new page cannot escape the guard', () => {
    expect(consolePages.map((key) => key.slice(CONSOLE_PREFIX.length))).toEqual(
      pagesOnDisk(CONSOLE_ROOT).sort(),
    )
  })

  it('is not empty', () => {
    // Without this the two lists agree at zero and every assertion below
    // passes by having nothing to assert about.
    expect(consolePages.length).toBeGreaterThan(0)
  })
})

describe('console page titles', () => {
  it.each(consolePages)('%s names its section', async (key) => {
    const title = await titleOf(key)

    // A plain string rather than a Metadata title object, on purpose. The
    // organisation and the product name are supplied by the template in
    // `[org]/layout.tsx`, so a page states one thing: which section it is.
    expect(typeof title).toBe('string')
    expect(title).not.toBe('')
  })

  it.each(consolePages)(
    '%s leaves the product name to the layout',
    async (key) => {
      // Otherwise the composed title reads "Org, Feed, Kindlast, Kindlast".
      expect(await titleOf(key)).not.toMatch(/Kindlast/)
    },
  )

  it.each(consolePages)(
    '%s uses no dashes, per the house style',
    async (key) => {
      expect(await titleOf(key)).not.toMatch(/[–—]/)
    },
  )

  it('gives no two pages the same title', async () => {
    // The complaint in ENT-269 was that every tab read the same. Titles that
    // are merely present but duplicated would leave a consultant back where
    // they started within one organisation.
    const titles = await Promise.all(consolePages.map(titleOf))
    expect(new Set(titles).size).toBe(titles.length)
  })
})

describe('what ENT-269 deliberately did not touch', () => {
  it('leaves the root layout carrying the marketing title', () => {
    // Read rather than imported: the root layout calls `next/font/google` at
    // module scope, which wants Next's compiler rather than Vite's.
    const source = readFileSync(join(APP_ROOT, 'layout.tsx'), 'utf8')
    expect(source).toContain(
      "title: 'Kindlast: AI-Powered GDPR & AI Act Compliance'",
    )
  })

  it('leaves the sign-in page setting its own title', () => {
    const source = readFileSync(
      join(APP_ROOT, '(auth)', 'sign-in', 'page.tsx'),
      'utf8',
    )
    expect(source).toContain("title: 'Sign in'")
  })
})
