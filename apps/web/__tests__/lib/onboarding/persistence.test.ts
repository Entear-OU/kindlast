import { describe, it, expect } from 'vitest'
import type { UIMessage } from 'ai'

import {
  messagesToPersist,
  textFromUIMessage,
} from '@/lib/onboarding/persistence'

/**
 * Unit coverage for the pure helpers in `lib/onboarding/persistence.ts`.
 *
 * The DB-backed `appendMessages` / `loadTranscript` / `getOrCreateActiveSession`
 * are exercised by `tests/integration/onboarding-persistence.test.ts` against
 * the local Supabase stack — those need RLS in the loop. The helpers covered
 * here are pure functions whose behaviour can be asserted in isolation.
 */

const ui = (role: 'user' | 'assistant', text: string, id = `m-${role}`): UIMessage =>
  ({
    id,
    role,
    parts: [{ type: 'text', text }],
  }) as UIMessage

describe('messagesToPersist (ENT-87)', () => {
  it('flattens text parts into {role, content}', () => {
    const result = messagesToPersist([
      ui('user', 'we sell SaaS'),
      ui('assistant', 'Got it — which countries?'),
    ])
    expect(result).toEqual([
      { role: 'user', content: 'we sell SaaS' },
      { role: 'assistant', content: 'Got it — which countries?' },
    ])
  })

  it('skips assistant turns whose flattened text is empty', () => {
    // Simulates a `streamText` failure: `onFinish` fires with parts=[] or
    // text-parts whose contents are empty strings. Persisting that pollutes
    // the next prompt's context with `""` as the assistant's last turn.
    const result = messagesToPersist([
      ui('user', 'we sell SaaS'),
      { id: 'm-empty', role: 'assistant', parts: [] } as UIMessage,
    ])
    expect(result).toEqual([{ role: 'user', content: 'we sell SaaS' }])
  })

  it('skips assistant turns whose text parts contain only whitespace', () => {
    const result = messagesToPersist([
      ui('user', 'we sell SaaS'),
      ui('assistant', '   \n  '),
    ])
    expect(result).toEqual([{ role: 'user', content: 'we sell SaaS' }])
  })

  it('preserves user turns even when content is empty', () => {
    // The user did interact with the form; preserving the row lets a retry
    // resume from the same input. (Today the chat client trims and rejects
    // empty submits — this guard keeps the policy explicit at the persister.)
    const result = messagesToPersist([{ id: 'm-u-empty', role: 'user', parts: [] } as UIMessage])
    expect(result).toEqual([{ role: 'user', content: '' }])
  })

  it('returns [] for an empty input', () => {
    expect(messagesToPersist([])).toEqual([])
  })
})

describe('textFromUIMessage', () => {
  it('joins multiple text parts', () => {
    const message = {
      id: 'm1',
      role: 'assistant',
      parts: [
        { type: 'text', text: 'Hello ' },
        { type: 'text', text: 'world.' },
      ],
    } as UIMessage
    expect(textFromUIMessage(message)).toBe('Hello world.')
  })

  it('ignores non-text parts', () => {
    const message = {
      id: 'm1',
      role: 'assistant',
      parts: [
        { type: 'text', text: 'Hello' },
        { type: 'step-start' } as never,
      ],
    } as UIMessage
    expect(textFromUIMessage(message)).toBe('Hello')
  })
})
