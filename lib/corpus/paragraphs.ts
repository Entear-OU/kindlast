/**
 * Offline enrichment helper for ENT-95: walk a regulation article body and
 * split it into addressable sub-paragraph rows.
 *
 * The legislative shape this parser knows about:
 *
 *   * Plain prose (Article 4 of the AI Act) — no numbering, one row with
 *     label "1".
 *   * Numbered paragraphs at the top level — "1. …", "2. …".
 *   * Letter sub-points inside a numbered paragraph — "(a) …", "(b) …".
 *     The emitted label is the parent number plus the letter in parens,
 *     so the Analyst can cite "Article 6(1)(a)" directly.
 *   * Trailing unnumbered continuation paragraphs (e.g. Article 6(3)'s
 *     "Notwithstanding the first subparagraph …") — appended to the most
 *     recent top-level row, not to the most recent letter sub-point.
 *     Legislatively those are "second subparagraphs" of the parent
 *     number; appending keeps cite-by-number queries returning the full
 *     normative content for that paragraph.
 *
 * Out of scope here:
 *
 *   * Roman-numeral sub-sub-points "(i)", "(ii)" — none of the MVP-critical
 *     AI Act articles use them. If a later regulation does, add a third
 *     parser branch.
 *   * Annexes — they have their own structure and table (ENT-96).
 *
 * The parser is intentionally pure + deterministic so the enrichment
 * script can re-run any time and produce identical output.
 */

export type ParsedParagraph = {
  label: string
  body: string
  ordering: number
}

const TOP_LEVEL_RE = /^(\d+)\.\s+([\s\S]+)$/
const LETTER_RE = /^\(([a-z]+)\)\s+([\s\S]+)$/

export function splitParagraphs(rawBody: string): ParsedParagraph[] {
  const blocks = rawBody
    .trim()
    .split(/\n{2,}/)
    .map((b) => b.trim())
    .filter(Boolean)

  if (blocks.length === 0) return []

  // Three shape branches:
  //
  //   * Has top-level "N." prefix anywhere → numbered paragraphs (Articles 6, 9,
  //     26, 50 of the AI Act). Letter sub-points are prefixed with the parent
  //     number — "1(a)", "1(b)".
  //   * No top-level but at least one "(a)" block → lead-in + letter list
  //     (Article 16 of the AI Act: "Providers … shall: (a) … (b) …"). The
  //     lead-in is grammatically incomplete on its own — drop it from the
  //     paragraph list (article.body still preserves it). Letter rows get
  //     bare "(a)", "(b)" labels, matching the OJ citation form
  //     "Article 16, point (a)".
  //   * Neither — plain prose (Article 4 of the AI Act: a single sentence on
  //     AI literacy). Emit the whole body as one row labelled "1".
  const hasTopLevel = blocks.some((b) => TOP_LEVEL_RE.test(b))
  const hasLetters = blocks.some((b) => LETTER_RE.test(b))

  if (!hasTopLevel && !hasLetters) {
    return [{ label: '1', body: rawBody.trim(), ordering: 1 }]
  }

  if (!hasTopLevel && hasLetters) {
    const out: ParsedParagraph[] = []
    for (const block of blocks) {
      const letterMatch = block.match(LETTER_RE)
      if (letterMatch) {
        out.push({
          label: `(${letterMatch[1]})`,
          body: letterMatch[2]!.trimEnd(),
          ordering: out.length + 1,
        })
      } else if (out.length > 0) {
        // Continuation block after a letter row — attach to the most recent.
        const last = out[out.length - 1]!
        last.body = `${last.body}\n\n${block.trimEnd()}`
      }
      // Lead-in (before any letter) is intentionally dropped — see comment above.
    }
    return out
  }

  const out: ParsedParagraph[] = []
  let currentTopLevelLabel: string | null = null

  for (const block of blocks) {
    const topMatch = block.match(TOP_LEVEL_RE)
    if (topMatch) {
      currentTopLevelLabel = topMatch[1]!
      out.push({
        label: currentTopLevelLabel,
        body: topMatch[2]!.trimEnd(),
        ordering: out.length + 1,
      })
      continue
    }

    const letterMatch = block.match(LETTER_RE)
    if (letterMatch && currentTopLevelLabel) {
      out.push({
        label: `${currentTopLevelLabel}(${letterMatch[1]})`,
        body: letterMatch[2]!.trimEnd(),
        ordering: out.length + 1,
      })
      continue
    }

    // Continuation block. Find the most recent top-level row (skip letter
    // sub-points) and append. See module-level note for the rationale.
    const topRow = findLastTopLevel(out)
    if (topRow) {
      topRow.body = `${topRow.body}\n\n${block.trimEnd()}`
    } else {
      // No top-level row exists yet — defensive fallback so an unexpected
      // shape doesn't drop content silently.
      out.push({ label: '0', body: block.trimEnd(), ordering: out.length + 1 })
    }
  }

  return out
}

function findLastTopLevel(rows: ParsedParagraph[]): ParsedParagraph | undefined {
  for (let i = rows.length - 1; i >= 0; i--) {
    if (!rows[i]!.label.includes('(')) return rows[i]
  }
  return undefined
}
