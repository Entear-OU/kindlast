import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

import { ModelForm } from '@/components/settings/model-form'
import {
  chooseBundledModelAction,
  chooseHostedModelAction,
} from '@/app/(authed)/o/[org]/settings/model/actions'
import type { ModelSettingView } from '@/lib/model/client'

/**
 * Where this organisation's model runs (ENT-236, §26.6).
 *
 * The assertions worth having here are the ones that fail silently in a
 * browser. Every one of these renders a plausible page whichever way it goes,
 * and the wrong way is a compliance claim rather than a layout bug:
 *
 *   A key rendered back into a field looks like a helpful pre-fill.
 *   A missing consequence notice looks like a tidy form.
 *   A viewer shown the controls looks like a working page until they submit.
 *   A deployment permitting nothing shown an empty select looks like a bug in
 *   the product rather than a property of the deployment.
 */

/* Both stand in for server actions, and a server action on this surface
   always resolves to an ActionState. Returning undefined here would make the
   component read `.status` off nothing, which is a property of the mock rather
   than of the code under test. */
vi.mock('@/app/(authed)/o/[org]/settings/model/actions', () => ({
  chooseHostedModelAction: vi.fn(async () => ({
    status: 'idle' as const,
    message: '',
  })),
  chooseBundledModelAction: vi.fn(async () => ({
    status: 'idle' as const,
    message: '',
  })),
}))

const NOTICE =
  'Findings, compliance profile facts and DSAR content for this organisation ' +
  'will leave this deployment and be processed by the provider you name.'

const REVERT =
  'Turning this off stops anything further leaving this deployment and ' +
  'destroys the stored key. It cannot reach content the provider has already ' +
  'processed.'

function view(overrides: Partial<ModelSettingView> = {}): ModelSettingView {
  return {
    setting: { hosted: false },
    permittedProviders: [{ name: 'openai', host: 'api.openai.com' }],
    consequenceNotice: NOTICE,
    revertNotice: REVERT,
    ...overrides,
  }
}

describe('ModelForm', () => {
  it('states the consequence before the fields rather than after the button', async () => {
    const user = userEvent.setup()
    render(<ModelForm slug="acme" view={view()} canManage />)

    // The fields are disclosed by the toggle (ENT-281), so reveal them first.
    // What is being asserted is unchanged: whoever sees the fields has already
    // been handed the sentence describing what they do.
    await user.click(
      screen.getByRole('switch', { name: /model this deployment runs/i }),
    )

    expect(screen.getByText(new RegExp(NOTICE.slice(0, 40), 'i'))).toBeTruthy()

    // The acknowledgement is unticked, and there is no defaultChecked anywhere.
    // A pre-ticked box would make the confirmation a formality, which is the
    // whole difference between a compliance event and a settings write.
    const acknowledge = screen.getByLabelText(/I understand this sends/i)
    expect((acknowledge as HTMLInputElement).checked).toBe(false)
  })

  it('shows the last four characters of a stored key and never a key', () => {
    render(
      <ModelForm
        slug="acme"
        canManage
        view={view({
          setting: {
            hosted: true,
            provider: 'openai',
            baseUrl: 'https://api.openai.com',
            model: 'gpt-oss-120b',
            credentialLastFour: '1234',
          },
        })}
      />,
    )

    expect(screen.getByText(/ends 1234/)).toBeTruthy()

    // The field is empty, because there is nothing to fill it from: no RPC on
    // this surface returns a key. Asserting it stays empty is what would catch
    // somebody adding a `apiKey` field to the read model and wiring it here.
    const key = screen.getByLabelText(/API key/i) as HTMLInputElement
    expect(key.value).toBe('')
    expect(key.type).toBe('password')
  })

  it('describes a deployment that permits nothing as a property, not an error', () => {
    render(
      <ModelForm
        slug="acme"
        canManage
        view={view({ permittedProviders: [] })}
      />,
    )

    expect(screen.getByText(/permits no hosted model providers/i)).toBeTruthy()
    // No form to submit, so a person cannot be sent round a loop that always
    // ends in a refusal from core-api.
    expect(screen.queryByLabelText(/API key/i)).toBeNull()
  })

  it('lets a viewer see where the data goes and not change it', () => {
    render(
      <ModelForm
        slug="acme"
        canManage={false}
        view={view({
          setting: {
            hosted: true,
            provider: 'openai',
            baseUrl: 'https://api.openai.com',
            model: 'gpt-oss-120b',
          },
        })}
      />,
    )

    expect(screen.getByText(/drafted by openai/i)).toBeTruthy()
    expect(screen.getByText(/Only an owner can change/i)).toBeTruthy()
    expect(screen.queryByLabelText(/API key/i)).toBeNull()
  })

  it('says what turning it off cannot undo', () => {
    render(
      <ModelForm
        slug="acme"
        canManage
        view={view({
          setting: { hosted: true, provider: 'openai', model: 'gpt-oss-120b' },
        })}
      />,
    )

    // The half an owner would otherwise assume. A product that let somebody
    // read the off switch as a recall would be helping them tell a regulator
    // something untrue.
    expect(
      screen.getByText(/cannot reach content the provider has already/i),
    ).toBeTruthy()
  })
})

/**
 * The toggle, and what it is allowed to decide (ENT-281).
 *
 * ENT-236 shaped this surface as a confirmation rather than a settings row,
 * and the toggle added here governs DISCLOSURE ONLY. The tests below are the
 * ones that would catch it quietly becoming the decision instead: a reveal
 * that calls core-api, an acknowledgement that survives being hidden, a
 * viewer who can operate it, or a deployment permitting nothing that offers a
 * switch leading nowhere.
 */
describe('ModelForm, the local-model toggle', () => {
  it('is on, and hides the provider fields, when the deployment runs the model', () => {
    render(<ModelForm slug="acme" view={view()} canManage />)

    const toggle = screen.getByRole('switch', {
      name: /model this deployment runs/i,
    })
    expect(toggle.getAttribute('aria-checked')).toBe('true')

    // Nothing to fill in until somebody asks for it. A form rendered behind an
    // "on" toggle is one a person can submit without ever having turned the
    // toggle off, which would make the disclosure decorative.
    expect(screen.queryByLabelText(/API key/i)).toBeNull()
    expect(screen.queryByLabelText(/I understand this sends/i)).toBeNull()
  })

  it('reveals the fields when switched off, and calls nothing to do it', async () => {
    const user = userEvent.setup()
    render(<ModelForm slug="acme" view={view()} canManage />)

    await user.click(
      screen.getByRole('switch', { name: /model this deployment runs/i }),
    )

    expect(screen.getByLabelText(/API key/i)).toBeTruthy()
    expect(screen.getByLabelText(/I understand this sends/i)).toBeTruthy()
    // The consequence still arrives above the fields, from core-api.
    expect(screen.getByText(new RegExp(NOTICE.slice(0, 40), 'i'))).toBeTruthy()

    // THE POINT OF THE WHOLE ISSUE. Revealing a form is not a processing
    // decision, so it reaches no RPC. If this ever fails, the toggle has
    // become the switch rather than the disclosure.
    expect(chooseHostedModelAction).not.toHaveBeenCalled()
    expect(chooseBundledModelAction).not.toHaveBeenCalled()
  })

  it('is off, with the fields already showing, when a provider is serving', () => {
    render(
      <ModelForm
        slug="acme"
        canManage
        view={view({
          setting: {
            hosted: true,
            provider: 'openai',
            baseUrl: 'https://api.openai.com',
            model: 'gpt-oss-120b',
          },
        })}
      />,
    )

    const toggle = screen.getByRole('switch', {
      name: /model this deployment runs/i,
    })
    expect(toggle.getAttribute('aria-checked')).toBe('false')
    expect(screen.getByLabelText(/API key/i)).toBeTruthy()
  })

  it('reverts to the bundled model when switched back on while hosted', async () => {
    const user = userEvent.setup()
    render(
      <ModelForm
        slug="acme"
        canManage
        view={view({
          setting: { hosted: true, provider: 'openai', model: 'gpt-oss-120b' },
        })}
      />,
    )

    await user.click(
      screen.getByRole('switch', { name: /model this deployment runs/i }),
    )

    // Stopping data leaving is the safe direction and carries no confirmation
    // by deliberate design, so the toggle may perform it directly.
    expect(chooseBundledModelAction).toHaveBeenCalled()
    expect(chooseHostedModelAction).not.toHaveBeenCalled()
  })

  it('offers no toggle when the deployment permits no provider', () => {
    render(
      <ModelForm
        slug="acme"
        canManage
        view={view({ permittedProviders: [] })}
      />,
    )

    // A switch that can only ever fail is worse than none: it tells somebody
    // the deployment has a choice it does not have.
    expect(screen.queryByRole('switch')).toBeNull()
    expect(screen.getByText(/permits no hosted model providers/i)).toBeTruthy()
  })

  it('does not let a member who is not an owner operate it', () => {
    render(<ModelForm slug="acme" canManage={false} view={view()} />)

    const toggle = screen.queryByRole('switch')
    if (toggle) expect(toggle).toBeDisabled()
    expect(screen.getByText(/Only an owner can change/i)).toBeTruthy()
  })
})

/**
 * Re-seeding the fields after the setting actually changes (ENT-281).
 *
 * The provider, endpoint and model inputs are uncontrolled and seeded from
 * `defaultValue`. React keeps an uncontrolled input's value across a rerender,
 * so once a submit succeeds and the page revalidates, the form goes on showing
 * what was typed rather than what was stored, and Base UI warns that a default
 * changed after initialisation. On a surface whose whole job is saying where
 * the data goes, a field that disagrees with the record is the wrong bug to
 * have.
 */
describe('ModelForm, after the stored setting changes', () => {
  it('re-seeds a field the person had typed into, and warns about nothing', async () => {
    const user = userEvent.setup()
    // Base UI reports a default changing under an uncontrolled field through
    // console.error, which is what surfaced in the dev overlay. Asserting the
    // silence is what keeps the fix rather than the symptom.
    const errors: unknown[][] = []
    const spy = vi
      .spyOn(console, 'error')
      .mockImplementation((...args: unknown[]) => {
        errors.push(args)
      })

    try {
      const { rerender } = render(
        <ModelForm slug="acme" view={view()} canManage />,
      )

      await user.click(
        screen.getByRole('switch', { name: /model this deployment runs/i }),
      )
      // Typing is what makes the field dirty, and a dirty uncontrolled input
      // keeps its value across a rerender. Without it this test passes against
      // the bug.
      await user.type(
        screen.getByLabelText(/Endpoint/i),
        'https://typo.example',
      )

      // What a successful UseHostedModel plus a revalidate looks like here.
      rerender(
        <ModelForm
          slug="acme"
          canManage
          view={view({
            setting: {
              hosted: true,
              provider: 'openai',
              baseUrl: 'https://api.openai.com',
              model: 'gpt-oss-120b',
              credentialLastFour: '1234',
              changedAt: '2026-09-01T12:00:00Z',
            },
          })}
        />,
      )

      // The field agrees with the record. A page whose whole job is saying
      // where the data goes must not show an endpoint that is not the stored
      // one.
      expect(
        (screen.getByLabelText(/Endpoint/i) as HTMLInputElement).value,
      ).toBe('https://api.openai.com')

      const complaints = errors
        .map((args) => String(args[0] ?? ''))
        .filter((m) => /changing the default value state|uncontrolled/i.test(m))
      expect(complaints).toEqual([])
    } finally {
      spy.mockRestore()
    }
  })

  it('keeps what was typed when the setting did not change', async () => {
    const user = userEvent.setup()
    const { rerender } = render(
      <ModelForm slug="acme" view={view()} canManage />,
    )

    await user.click(
      screen.getByRole('switch', { name: /model this deployment runs/i }),
    )
    await user.type(
      screen.getByLabelText(/Endpoint/i),
      'https://api.openai.com',
    )

    // A refused submit leaves the setting alone, and retyping an endpoint
    // because the acknowledgement was missed is its own small insult.
    rerender(<ModelForm slug="acme" view={view()} canManage />)

    expect((screen.getByLabelText(/Endpoint/i) as HTMLInputElement).value).toBe(
      'https://api.openai.com',
    )
  })
})
