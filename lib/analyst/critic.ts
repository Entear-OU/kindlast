/**
 * The Analyst-side critic (ENT-60).
 *
 * A deterministic guardrail over the LLM-generated finding narrative. The
 * generator (`lib/analyst/narrative.ts`) produces a plain-language description
 * and a proposed action; this critic decides whether the action is good enough
 * to persist. "Good" means specific, verb-led, and singular — "Draft a Data
 * Processing Agreement with Stripe", not "Review your vendor agreements". A
 * rejected action is regenerated (or falls back to the baseline) rather than
 * shown to the founder, so the feed only ever surfaces one-click-approvable
 * items.
 *
 * Why a hand-written critic rather than a second model pass: the rules are
 * mechanical (banned openers, single sentence, no hedging) and the AC requires
 * a *unit test* that bad outputs are rejected — which means the gate must be
 * deterministic. The model's job is to write well; the critic's job is to be
 * predictable.
 */

export interface Critique {
  ok: boolean
  /** Machine-readable reason codes; empty when ok. */
  reasons: string[]
}

// Imperative openers that signal generic advice rather than a concrete step.
// The canonical AC reject ("Review your vendor agreements") starts with one.
const GENERIC_VERBS = new Set([
  'review', 'consider', 'ensure', 'check', 'evaluate', 'assess', 'manage',
  'handle', 'improve', 'maintain', 'monitor', 'understand', 'explore',
  'examine', 'address', 'look', 'think', 'determine',
])

// Strong, single-write action verbs the generator is asked to lead with. An
// action whose first word isn't here (and isn't a generic verb) reads as
// non-imperative or off-pattern and is regenerated. The list grows as the
// catalogue's actions do.
const ACTION_VERBS = new Set([
  'draft', 'appoint', 'send', 'create', 'publish', 'sign', 'add', 'record',
  'notify', 'complete', 'document', 'register', 'configure', 'respond',
  'submit', 'conduct', 'designate', 'delete', 'restrict', 'erase', 'rectify',
  'update', 'prepare', 'schedule', 'file', 'log', 'assign', 'map', 'classify',
  'encrypt', 'anonymise', 'anonymize', 'deactivate', 'remove', 'contact',
  'request', 'obtain', 'execute', 'add', 'set', 'enable', 'disable',
])

// Vague objects/qualifiers that hollow out an otherwise verb-led action.
const GENERIC_PHRASES = [
  'your vendor agreements', 'your processes', 'your policies', 'your procedures',
  'your documentation', 'update your', 'as appropriate', 'where applicable',
  'if necessary', 'if needed', 'as needed',
]

const HEDGES = [
  'maybe', 'you might', 'you could', 'should consider', 'perhaps', 'try to',
  'as appropriate', 'where applicable', 'if necessary',
]

const LEGAL_JARGON = [
  'pursuant to', 'hereinafter', 'aforementioned', 'notwithstanding',
  'shall be deemed', 'inter alia', 'thereof', 'herein', 'the controller shall',
  'the data controller shall',
]

const ACTION_MIN = 15
const ACTION_MAX = 200

/**
 * House style bans the em dash in user-facing copy (ENT-160, ENT-163). The
 * prompts ask for it, but a prompt is a request and this is a guarantee: a
 * narrative that slips one through is regenerated rather than shipped. Hyphens
 * in compound words ("plain-language") are untouched.
 */
const EM_DASH = '—'

/** Number of sentence-final boundaries actually followed by a new sentence. */
function sentenceCount(text: string): number {
  const trimmed = text.trim()
  if (!trimmed) return 0
  // A boundary is terminal punctuation followed by whitespace + more content
  // (so "Stripe Inc." mid-action doesn't read as two sentences), plus the
  // final sentence itself.
  const internal = (trimmed.match(/[.!?]+\s+\S/g) ?? []).length
  return internal + 1
}

function firstWord(text: string): string {
  return (text.trim().split(/\s+/)[0] ?? '').toLowerCase().replace(/[^a-z]/g, '')
}

/**
 * Critique a proposed action. Returns `{ ok: true, reasons: [] }` only when the
 * action is specific, verb-led, and a single step.
 */
export function critiqueProposedAction(action: string): Critique {
  const reasons: string[] = []
  const text = action.trim()
  const lower = text.toLowerCase()
  const length = text.length

  if (length < ACTION_MIN) reasons.push('too_short')
  if (length > ACTION_MAX) reasons.push('too_long')

  const first = firstWord(text)
  if (GENERIC_VERBS.has(first)) {
    reasons.push('generic_verb')
  } else if (!ACTION_VERBS.has(first)) {
    reasons.push('not_verb_led')
  }

  if (GENERIC_PHRASES.some((p) => lower.includes(p))) reasons.push('generic_phrase')
  if (HEDGES.some((h) => lower.includes(h))) reasons.push('hedging')

  if (sentenceCount(text) > 1) reasons.push('multiple_sentences')
  if ([' and then ', ' and also ', '; ', ' then '].some((c) => lower.includes(c))) {
    reasons.push('multiple_actions')
  }

  if (text.includes(EM_DASH)) reasons.push('em_dash')

  return { ok: reasons.length === 0, reasons: [...new Set(reasons)] }
}

const DESC_MIN = 20
const DESC_MAX = 400

/**
 * Critique a description: plain language (no archaic legalese), one to two
 * sentences. Lighter than the action critique — the description explains, the
 * action is what gets approved.
 */
export function critiqueDescription(description: string): Critique {
  const reasons: string[] = []
  const text = description.trim()
  const lower = text.toLowerCase()

  if (text.length < DESC_MIN) reasons.push('too_short')
  if (text.length > DESC_MAX) reasons.push('too_long')

  const sentences = sentenceCount(text)
  if (sentences < 1 || sentences > 2) reasons.push('sentence_count')

  if (LEGAL_JARGON.some((j) => lower.includes(j))) reasons.push('legal_jargon')

  if (text.includes(EM_DASH)) reasons.push('em_dash')

  return { ok: reasons.length === 0, reasons: [...new Set(reasons)] }
}
