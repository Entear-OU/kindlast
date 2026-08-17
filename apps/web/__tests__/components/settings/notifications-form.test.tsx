import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { NotificationsForm } from '@/components/settings/notifications-form'

/**
 * Notification preferences (ENT-209).
 *
 * The assertions worth having here are about defaults and about what the form
 * says when a deployment cannot send mail, because both fail silently. A form
 * that defaults every switch to off unsubscribes people who never asked to be,
 * and a form that looks perfectly normal on a deployment with no SMTP produces
 * a person waiting for email that will never come.
 */

vi.mock('@/app/(authed)/o/[org]/settings/actions', () => ({
  updateNotificationsAction: vi.fn(),
}))

const emailAvailable = [{ id: 'email', displayName: 'Email', available: true }]

const emailUnavailable = [
  {
    id: 'email',
    displayName: 'Email',
    available: false,
    unavailableReason:
      'This deployment has no mail server configured, so notifications are queued rather than delivered.',
  },
]

describe('what somebody who has never saved anything sees', () => {
  it('shows them subscribed, because that is the product default', () => {
    // core-api returns defaults rather than an error for a person with no row,
    // and those defaults are subscribed. A form that rendered everything off
    // would misrepresent what the database will actually do.
    render(
      <NotificationsForm
        slug="acme"
        preferences={{}}
        channels={emailAvailable}
      />,
    )

    expect(screen.getByLabelText(/weekly briefing/i)).toBeChecked()
    expect(screen.getByLabelText(/deadline/i)).toBeChecked()
    expect(screen.getByLabelText(/email me about findings from/i)).toHaveValue(
      'medium',
    )
  })

  it('offers a floor rather than an on/off switch', () => {
    // "Tell me everything" and "tell me nothing" are both wrong for a
    // compliance product: the first trains people to ignore the mail, the
    // second is how a critical finding goes unread.
    render(
      <NotificationsForm
        slug="acme"
        preferences={{}}
        channels={emailAvailable}
      />,
    )

    const select = screen.getByLabelText(/email me about findings from/i)
    const values = Array.from(
      select.querySelectorAll('option'),
      (o) => (o as HTMLOptionElement).value,
    )
    expect(values).toEqual(['low', 'medium', 'high', 'critical'])
  })
})

describe('what somebody who has saved settings sees', () => {
  it('renders what is stored rather than the defaults', () => {
    render(
      <NotificationsForm
        slug="acme"
        preferences={{
          email: 'compliance@example.com',
          minSeverityForEmail: 'critical',
          weeklyBriefingEnabled: false,
          deadlineAlertsEnabled: true,
          timezone: 'Europe/Berlin',
          quietHoursStart: '22:00',
          quietHoursEnd: '07:00',
        }}
        channels={emailAvailable}
      />,
    )

    expect(screen.getByLabelText(/send to/i)).toHaveValue(
      'compliance@example.com',
    )
    expect(screen.getByLabelText(/email me about findings from/i)).toHaveValue(
      'critical',
    )
    expect(screen.getByLabelText(/weekly briefing/i)).not.toBeChecked()
    expect(screen.getByLabelText(/deadline/i)).toBeChecked()
    expect(screen.getByLabelText(/timezone/i)).toHaveValue('Europe/Berlin')
    expect(screen.getByLabelText(/quiet from/i)).toHaveValue('22:00')
    expect(screen.getByLabelText(/quiet until/i)).toHaveValue('07:00')
  })
})

describe('when the deployment cannot send email', () => {
  it('says so, and still lets the person record what they want', () => {
    // Greying the form out would lose the setting and explain nothing. The
    // preference is recorded and takes effect the moment an operator
    // configures SMTP, which is exactly what the outbox is for.
    render(
      <NotificationsForm
        slug="acme"
        preferences={{}}
        channels={emailUnavailable}
      />,
    )

    expect(screen.getByRole('status')).toHaveTextContent(/no mail server/i)
    expect(screen.getByLabelText(/weekly briefing/i)).not.toBeDisabled()
    expect(
      screen.getByRole('button', { name: /save notification settings/i }),
    ).toBeInTheDocument()
  })

  it('says nothing when it can', () => {
    render(
      <NotificationsForm
        slug="acme"
        preferences={{}}
        channels={emailAvailable}
      />,
    )
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
  })
})

describe('channels this deployment does not have', () => {
  it('are not rendered at all', () => {
    // §18.3. A settings page offering Telegram on a deployment with no Telegram
    // is indistinguishable from a broken integration, and generates support
    // rather than value. The capabilities endpoint lists only what exists.
    render(
      <NotificationsForm
        slug="acme"
        preferences={{}}
        channels={emailAvailable}
      />,
    )

    expect(screen.queryByText(/telegram/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/slack/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/whatsapp/i)).not.toBeInTheDocument()
  })
})
