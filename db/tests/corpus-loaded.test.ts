/**
 * A stack that is up has the law in it (ENT-266).
 *
 * # WHAT THIS EXISTS TO CATCH
 *
 * A clean `docker compose up` used to come up with `regulatory_documents` and
 * `regulatory_articles` empty. `obligations` had its fifteen rows, because
 * those came from a seed, so nothing looked broken from the database side and
 * everything looked broken from the product side: the Regulation page said no
 * regulation had been loaded into this deployment, and every obligation said
 * the text behind its citation was not here. The corpus was committed under
 * `data/corpus/` the whole time. Nothing loaded it.
 *
 * The fix is a `corpus-load` job in compose, and a job is exactly the kind of
 * thing that can stop running without anybody noticing: it exits, its logs
 * scroll away, and the failure shows up months later as a page that says the
 * text is not in this deployment. So this asserts the outcome rather than the
 * job, from the same suite CI runs immediately after bringing the stack up.
 *
 * # ITS ONE HONEST WEAKNESS
 *
 * `apps/core-api/internal/store/postgres/corpus_drift_test.go` also ingests
 * the corpus, into the same database. On a laptop where the Go suite has run,
 * these rows are there whether or not the compose job works, and this file
 * would pass for a reason it is not testing. In CI the order is the other way
 * round (`bun run test:db` runs before the Go suites), so there the assertion
 * is the one it claims to be. Somebody checking this locally should
 * `down -v` first.
 *
 * # WHY IT READS THE FILES RATHER THAN NAMING THE REGULATIONS
 *
 * Adding a regulation is a file plus a line in `packs.json`, which is the
 * boundary `docs/regulation-packs.md` draws. A test carrying `32016R0679` as a
 * literal would be one more place that boundary leaked into code, and it would
 * pass a stack that loaded the GDPR and silently dropped the AI Act.
 */
import { describe, it, expect, beforeAll, afterAll } from 'vitest'
import type { Client } from 'pg'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'
import { connect, isStackReachable, MIGRATOR_URL } from './helpers/db'

const reachable = await isStackReachable()

const CORPUS_DIR = join(import.meta.dirname, '..', '..', 'data', 'corpus')

type ManifestEntry = { id: string; kind: string; file: string }

/** The CELEX number of every regulation the manifest lists as a document. */
async function celexNumbersInTheCheckout(): Promise<string[]> {
  const manifest = JSON.parse(
    await readFile(join(CORPUS_DIR, 'packs.json'), 'utf8'),
  ) as { packs: ManifestEntry[] }

  const documents = manifest.packs.filter((pack) => pack.kind === 'document')
  expect(
    documents.length,
    'packs.json lists no regulation, so this test could not fail',
  ).toBeGreaterThan(0)

  return Promise.all(
    documents.map(async (pack) => {
      const parsed = JSON.parse(
        await readFile(join(CORPUS_DIR, pack.file), 'utf8'),
      ) as { document: { celexNumber: string } }
      return parsed.document.celexNumber
    }),
  )
}

let migrator: Client

beforeAll(async () => {
  if (!reachable) return
  migrator = await connect(MIGRATOR_URL)
})

afterAll(async () => {
  await migrator?.end()
})

describe.skipIf(!reachable)('the corpus a fresh stack comes up with', () => {
  it('holds every regulation the checkout carries', async () => {
    const expected = await celexNumbersInTheCheckout()

    const { rows } = await migrator.query<{ celex_number: string }>(
      'select celex_number from regulatory_documents',
    )
    const loaded = rows.map((row) => row.celex_number)

    for (const celex of expected) {
      expect(
        loaded,
        `${celex} is in data/corpus/ and not in this deployment, so every ` +
          'finding citing it can name the article and not show it',
      ).toContain(celex)
    }
  })

  it('holds the articles a citation resolves against', async () => {
    // The count is deliberately not asserted against the files. That
    // comparison is the drift guard's job and it does it field by field in
    // Go. What is being asserted here is only that the ingest ran at all,
    // and an empty table is the shape that failure takes.
    const { rows } = await migrator.query<{ articles: string }>(
      'select count(*)::text as articles from regulatory_articles',
    )
    expect(Number(rows[0].articles)).toBeGreaterThan(0)
  })

  it('resolves every obligation to an article that is there', async () => {
    // The product's whole claim, expressed as a query. An obligation whose
    // citation points at nothing renders as "the text behind this citation is
    // not in this deployment's corpus", which is the state ENT-266 found.
    // No join to `regulatory_documents` in the outer query, deliberately.
    // Written as a join, an empty `regulatory_documents` produces no rows and
    // this passes while asserting nothing, which is exactly the corpus-shaped
    // hole it is here to find. As a `not exists` over both tables, a missing
    // document fails it the same way a missing article does.
    const { rows } = await migrator.query<{ slug: string }>(`
      select o.slug
        from obligations o
       where o.citation_kind = 'article'
         and not exists (
               select 1
                 from regulatory_articles a
                 join regulatory_documents d on d.id = a.document_id
                where d.celex_number = o.citation_celex
                  and a.article_number = o.citation_article
             )
       order by o.slug
    `)

    expect(
      rows.map((row) => row.slug),
      'these obligations cite an article this deployment does not hold',
    ).toEqual([])
  })
})
