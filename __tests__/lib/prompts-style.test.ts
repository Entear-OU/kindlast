import { describe, expect, it } from 'vitest'

import { ANALYST_NARRATIVE_PROMPT } from '@/lib/analyst/narrative'
import { ONBOARDING_SYSTEM_PROMPT } from '@/lib/onboarding/system-prompt'

/**
 * House style over generated copy (ENT-163).
 *
 * ENT-160 stripped em dashes from static copy, but the biggest user-facing
 * copy surface in the product is written at runtime by a model. A founder
 * reading the onboarding chat cannot tell which sentences were hand-authored
 * and which were generated, so the rule has to reach the prompts too.
 *
 * Two things are asserted, and both matter:
 *
 *   1. The prompt says not to use em dashes.
 *   2. The prompt itself contains none. A prompt that bans em dashes in a
 *      sentence that uses one is teaching the wrong thing by example, and the
 *      onboarding prompt used to carry a literal wrap-up line for the model to
 *      imitate.
 */

const EM_DASH = '—'

const PROMPTS: ReadonlyArray<readonly [string, string]> = [
  ['ONBOARDING_SYSTEM_PROMPT', ONBOARDING_SYSTEM_PROMPT],
  ['ANALYST_NARRATIVE_PROMPT', ANALYST_NARRATIVE_PROMPT],
]

describe('LLM prompt copy style (ENT-163)', () => {
  it.each(PROMPTS)('%s contains no em dash', (_name, prompt) => {
    expect(prompt).not.toContain(EM_DASH)
  })

  it.each(PROMPTS)('%s instructs the model to avoid em dashes', (_name, prompt) => {
    expect(prompt.toLowerCase()).toMatch(/em dash/)
  })
})
