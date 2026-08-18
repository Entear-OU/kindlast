import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'

import { EvidenceList } from '@/components/memory/evidence-list'
import { ProfileFactList } from '@/components/memory/profile-fact-list'
import { readValue, type ProfileFact } from '@/lib/memory/client'

/**
 * What Kindlast knows about you, rendered (ENT-228).
 *
 * The assertions worth having are all about the distinctions this page exists
 * to preserve, because every way of losing one renders a page that looks fine.
 *
 *   * Not recorded is not the same as recorded as unknown.
 *   * An empty list is an answer, not a blank.
 *   * Where a value came from belongs beside the value, not above the page.
 *   * A superseded observation stays visible.
 *
 * A version of this component that collapsed any of those would pass a naive
 * snapshot test and would quietly make the product less honest.
 */

function fact(overrides: Partial<ProfileFact> = {}): ProfileFact {
  return {
    key: 'PROFILE_FACT_KEY_HAS_DPO',
    value: { triState: 'TRI_STATE_UNSURE' },
    source: 'onboarding',
    validFrom: '2026-06-01T10:00:00Z',
    ...overrides,
  }
}

describe('readValue', () => {
  it('renders "Not sure" rather than blanking it', () => {
    // "We do not know whether we have a record of processing activities" is a
    // finding in itself. Rendering it as empty turns an actionable state into
    // a question nobody asked.
    expect(readValue(fact())).toBe('Not sure')
  })

  it('distinguishes a value we never recorded from one recorded as unknown', () => {
    expect(readValue(fact({ value: undefined }))).toBeNull()
    expect(readValue(fact())).toBe('Not sure')
  })

  it('renders an empty list as an answer, not as nothing', () => {
    // "We operate no AI systems" is what somebody said. Showing it as blank
    // would read as a question they skipped, and the two lead to opposite
    // findings.
    expect(
      readValue(
        fact({
          key: 'PROFILE_FACT_KEY_AI_SYSTEMS',
          value: { list: { values: [] } },
        }),
      ),
    ).toBe('None')
  })

  it('joins a list rather than printing an object', () => {
    expect(
      readValue(
        fact({
          key: 'PROFILE_FACT_KEY_EU_JURISDICTIONS',
          value: { list: { values: ['DE', 'EE'] } },
        }),
      ),
    ).toBe('DE, EE')
  })
})

describe('ProfileFactList', () => {
  it('shows where each value came from, beside the value', () => {
    render(<ProfileFactList facts={[fact()]} slug="alpha" />)

    // Provenance shown once at the top would let a reader assume a connected
    // tool vouched for all of it. A profile where one field came from a tool
    // and another from a guess in a form is the normal case.
    expect(screen.getByText(/You told us during setup/)).toBeTruthy()
  })

  it('offers the history of every fact', () => {
    render(<ProfileFactList facts={[fact()]} slug="alpha" />)

    // Without this link, correcting a fact is indistinguishable from our
    // having always thought the new thing.
    const link = screen.getByRole('link', { name: /history/i })
    expect(link.getAttribute('href')).toContain(
      '/settings/memory/PROFILE_FACT_KEY_HAS_DPO',
    )
  })

  it('says "Not recorded" rather than showing an empty cell', () => {
    render(
      <ProfileFactList facts={[fact({ value: undefined })]} slug="alpha" />,
    )
    expect(screen.getByText('Not recorded')).toBeTruthy()
  })
})

describe('EvidenceList', () => {
  it('says how stale a reading was when we took it', () => {
    render(
      <EvidenceList
        evidence={[
          {
            id: '1',
            source: 'integration',
            kind: 'ropa_export',
            observedAt: '2026-03-01T00:00:00Z',
            fetchedAt: '2026-08-01T00:00:00Z',
          },
        ]}
      />,
    )

    // A record of processing activities last edited in March and first read by
    // us in August is a five-month blind spot, and this is the only place a
    // customer would find that out. One timestamp would hide it.
    expect(screen.getByText(/read by us 153 days later/)).toBeTruthy()
  })

  it('says nothing about staleness when the reading was fresh', () => {
    render(
      <EvidenceList
        evidence={[
          {
            id: '1',
            source: 'integration',
            kind: 'ropa_export',
            observedAt: '2026-08-01T09:00:00Z',
            fetchedAt: '2026-08-01T09:00:01Z',
          },
        ]}
      />,
    )

    // Repeating the date twice tells a reader nothing. The gap is the signal,
    // so its absence should be silent rather than noisy.
    expect(screen.queryByText(/read by us/)).toBeNull()
  })

  it('keeps a superseded observation visible and marked', () => {
    render(
      <EvidenceList
        evidence={[
          {
            id: '1',
            source: 'integration',
            kind: 'ropa_export',
            observedAt: '2026-03-01T00:00:00Z',
            fetchedAt: '2026-03-01T00:00:00Z',
            supersededBy: '2',
          },
        ]}
      />,
    )

    // "We used to read this from your helpdesk" is exactly what somebody
    // auditing an older finding is looking for. A list that dropped superseded
    // rows would make the record look tidier than it is.
    expect(screen.getByText('ropa_export')).toBeTruthy()
    expect(screen.getByText(/Superseded by a later reading/)).toBeTruthy()
  })
})
