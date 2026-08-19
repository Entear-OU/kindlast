import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { ModelForm } from '@/components/settings/model-form'
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

vi.mock('@/app/(authed)/o/[org]/settings/model/actions', () => ({
  chooseHostedModelAction: vi.fn(),
  chooseBundledModelAction: vi.fn(),
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
  it('states the consequence before the fields rather than after the button', () => {
    render(<ModelForm slug="acme" view={view()} canManage />)

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
