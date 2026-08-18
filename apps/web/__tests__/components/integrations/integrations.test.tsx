import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'

import { ConnectionList } from '@/components/integrations/connection-list'
import { FetchList } from '@/components/integrations/fetch-list'
import {
  toolsByRisk,
  type Fetch,
  type Integration,
} from '@/lib/integrations/client'

/**
 * What the integrations surface has to show, and what it must not (ENT-231).
 *
 * The distinctions these tests protect, each of which is a thing somebody
 * would reasonably simplify away:
 *
 *   Ungranted tools are LISTED, not hidden. What a connection could have been
 *   given is half the question somebody reviewing it is asking.
 *
 *   A refusal is not an error. It is what a working control produces, and a
 *   log showing only successes would be indistinguishable from a deployment
 *   where the gateway does nothing.
 *
 *   The endpoint is text, never a link. It is a URL a customer typed.
 *
 *   Revoked connections stay in the list, marked.
 */

function integration(overrides: Partial<Integration> = {}): Integration {
  return {
    id: 'conn-1',
    kind: 'INTEGRATION_KIND_MCP',
    displayName: 'Helpdesk',
    endpointUrl: 'https://tools.example.com/mcp',
    status: 'INTEGRATION_STATUS_ACTIVE',
    createdAt: '2026-03-04T09:12:00Z',
    tools: [
      { name: 'search_tickets', writeCapable: false, granted: true },
      { name: 'close_ticket', writeCapable: true, granted: false },
    ],
    ...overrides,
  }
}

function fetched(overrides: Partial<Fetch> = {}): Fetch {
  return {
    id: 'fetch-1',
    integrationId: 'conn-1',
    integrationName: 'Helpdesk',
    tool: 'search_tickets',
    outcome: 'succeeded',
    requestedAt: '2026-03-04T09:12:00Z',
    redactions: 0,
    ...overrides,
  }
}

describe('the connection list', () => {
  it('lists tools that are not granted, so what was declined is visible', () => {
    render(<ConnectionList integrations={[integration()]} />)

    expect(screen.getByText('search_tickets')).toBeInTheDocument()
    // The ungranted one is on the page, and is labelled as not allowed rather
    // than being left out.
    expect(screen.getByText('close_ticket')).toBeInTheDocument()
    expect(screen.getByText('not allowed')).toBeInTheDocument()
  })

  it('says which tools can change data, wherever they appear', () => {
    render(<ConnectionList integrations={[integration()]} />)

    expect(screen.getByText('can change data')).toBeInTheDocument()
    expect(screen.getByText('read only')).toBeInTheDocument()
  })

  it('shows the endpoint as text and never as a link', () => {
    render(<ConnectionList integrations={[integration()]} />)

    expect(
      screen.getByText('https://tools.example.com/mcp'),
    ).toBeInTheDocument()
    // A URL a customer typed, made clickable, would turn a console page into a
    // way to have somebody's browser fetch an arbitrary address.
    expect(
      screen.queryByRole('link', { name: /tools\.example\.com/ }),
    ).toBeNull()
  })

  it('keeps a revoked connection in the list, marked', () => {
    render(
      <ConnectionList
        integrations={[
          integration({
            status: 'INTEGRATION_STATUS_REVOKED',
            revokedAt: '2026-04-01T10:00:00Z',
          }),
        ]}
      />,
    )

    expect(screen.getByText(/Revoked/)).toBeInTheDocument()
    expect(screen.getByText('Helpdesk')).toBeInTheDocument()
  })

  it('never renders a credential, because none is ever sent', () => {
    const { container } = render(
      <ConnectionList integrations={[integration()]} />,
    )
    // The proto carries no credential field in either direction. Asserted
    // rather than assumed, because a field added to the response later would
    // otherwise land on this page without anybody deciding it should.
    expect(container.textContent).not.toMatch(/credential/i)
  })
})

describe('what we fetched', () => {
  it('shows a refusal as declined rather than as an error', () => {
    render(
      <FetchList
        fetches={[
          fetched({
            outcome: 'refused',
            tool: 'close_ticket',
            detail: 'the tool is not granted on this connection',
          }),
        ]}
      />,
    )

    expect(screen.getByText('Declined')).toBeInTheDocument()
    // core-api's own sentence, passed through: it is the specific one and it
    // is what tells a customer which control fired.
    expect(
      screen.getByText('the tool is not granted on this connection'),
    ).toBeInTheDocument()
  })

  it('says how many values were removed, and only when some were', () => {
    render(<FetchList fetches={[fetched({ id: 'a', redactions: 3 })]} />)
    expect(
      screen.getByText('3 values were removed before this was stored'),
    ).toBeInTheDocument()

    render(<FetchList fetches={[fetched({ id: 'b', redactions: 0 })]} />)
    // Zero is not shown: a line saying "0 values removed" beside every fetch
    // is noise that would bury the ones that matter.
    expect(screen.queryByText(/0 values were removed/)).toBeNull()
  })

  it('distinguishes could not reach from declined', () => {
    render(
      <FetchList
        fetches={[
          fetched({
            outcome: 'failed',
            detail: 'the endpoint answered 503 Service Unavailable',
          }),
        ]}
      />,
    )
    expect(screen.getByText('Could not reach')).toBeInTheDocument()
  })
})

describe('tool ordering', () => {
  it('puts write-capable tools first, because they are the ones worth reading', () => {
    const ordered = toolsByRisk([
      { name: 'aaa_read', writeCapable: false },
      { name: 'zzz_write', writeCapable: true },
      { name: 'bbb_read', writeCapable: false },
    ])

    // Alphabetical order would bury the one tool that can change something
    // between two that cannot.
    expect(ordered.map((tool) => tool.name)).toEqual([
      'zzz_write',
      'aaa_read',
      'bbb_read',
    ])
  })
})
