import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ConsoleShell } from '@/components/console/console-shell'

/**
 * ENT-151 — the console rail's Billing destination. The rail is otherwise
 * covered implicitly by the feed/records pages; here we pin the new Billing
 * entry: it links to /billing, and renders as the active (non-link) item when
 * the page marks it so.
 */
describe('ConsoleShell rail — Billing (ENT-151)', () => {
  it('links Billing to /billing when not the active rail', () => {
    render(
      <ConsoleShell activeRail="alerts" title="Agent feed">
        <div>body</div>
      </ConsoleShell>,
    )
    const billing = screen.getByRole('link', { name: 'Billing' })
    expect(billing).toHaveAttribute('href', '/billing')
  })

  it('marks Billing active (aria-current, no link) on the billing page', () => {
    render(
      <ConsoleShell activeRail="billing" title="Billing">
        <div>body</div>
      </ConsoleShell>,
    )
    // Active rail items render as a span with aria-current, not a link.
    expect(screen.queryByRole('link', { name: 'Billing' })).not.toBeInTheDocument()
    const active = screen.getByLabelText('Billing')
    expect(active).toHaveAttribute('aria-current', 'page')
  })

  it('keeps the other real destinations linked', () => {
    render(
      <ConsoleShell activeRail="billing" title="Billing">
        <div>body</div>
      </ConsoleShell>,
    )
    expect(screen.getByRole('link', { name: 'Records' })).toHaveAttribute('href', '/records/ropa')
    expect(screen.getByRole('link', { name: 'Alerts' })).toHaveAttribute('href', '/feed')
  })
})
