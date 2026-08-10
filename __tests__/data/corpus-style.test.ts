import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'

/**
 * ENT-187 - house style guard for the regulatory corpus.
 *
 * The corpus is a user-facing copy surface: the Analyst renders these summaries
 * into findings, and the citation text a founder reads to verify a claim comes
 * straight out of these files. ENT-160 cleaned static UI copy and ENT-163
 * covers runtime LLM output, so this third surface had no guard at all and
 * accumulated 558 em dashes and 56 en dashes unnoticed.
 *
 * Style rule, from CLAUDE.md: never use em dashes in copy. En dashes go the
 * same way, except in numeric ranges where a plain hyphen is used instead.
 */

const CORPUS_DIR = path.resolve(__dirname, '../../data/corpus')

const EM_DASH = '—'
const EN_DASH = '–'

const corpusFiles = readdirSync(CORPUS_DIR).filter((f) => f.endsWith('.json'))

/** Every string value in the tree, with a dotted path for readable failures. */
function collectStrings(node: unknown, at = '$'): Array<{ path: string; value: string }> {
  if (typeof node === 'string') return [{ path: at, value: node }]
  if (Array.isArray(node)) return node.flatMap((v, i) => collectStrings(v, `${at}[${i}]`))
  if (node && typeof node === 'object') {
    return Object.entries(node).flatMap(([k, v]) => collectStrings(v, `${at}.${k}`))
  }
  return []
}

describe('regulatory corpus house style (ENT-187)', () => {
  it('ships at least one corpus file, so a rename cannot silently void this suite', () => {
    expect(corpusFiles.length).toBeGreaterThan(0)
  })

  describe.each(corpusFiles)('%s', (file) => {
    const raw = readFileSync(path.join(CORPUS_DIR, file), 'utf8')
    const strings = collectStrings(JSON.parse(raw))

    it('contains no em dashes', () => {
      const offenders = strings
        .filter((s) => s.value.includes(EM_DASH))
        .map((s) => `${s.path}: ${excerpt(s.value, EM_DASH)}`)

      expect(offenders, `${offenders.length} string(s) contain an em dash`).toEqual([])
    })

    it('contains no en dashes', () => {
      const offenders = strings
        .filter((s) => s.value.includes(EN_DASH))
        .map((s) => `${s.path}: ${excerpt(s.value, EN_DASH)}`)

      expect(offenders, `${offenders.length} string(s) contain an en dash`).toEqual([])
    })

    // Guards the fix itself rather than the style rule. A careless
    // find-and-replace across JSON can mangle other non-ASCII characters, and
    // the corpus is full of them: euro signs in fine amounts, accented
    // supervisory authority names, section symbols in citations.
    it('is not damaged by unicode normalisation', () => {
      expect(raw.normalize('NFC')).toEqual(raw)
    })

    it('parses as JSON and has string content', () => {
      expect(strings.length).toBeGreaterThan(0)
    })
  })

  // Scoped to the one file that carries currency, rather than asserted across
  // all five. A blanket check would fail on the four that have no fine amounts
  // and say nothing useful about them.
  it('preserves euro signs in enforcement fine amounts', () => {
    const raw = readFileSync(path.join(CORPUS_DIR, 'enforcement-decisions.json'), 'utf8')
    expect(raw).toContain('€')
  })
})

/** A window around the first offending character, for a legible failure. */
function excerpt(value: string, needle: string): string {
  const i = value.indexOf(needle)
  const start = Math.max(0, i - 40)
  const end = Math.min(value.length, i + 40)
  return `${start > 0 ? '...' : ''}${value.slice(start, end)}${end < value.length ? '...' : ''}`
}
