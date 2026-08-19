'use client'

import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  chooseBundledModelAction,
  chooseHostedModelAction,
} from '@/app/(authed)/o/[org]/settings/model/actions'
import { idle, type ActionState } from '@/lib/org/action-state'
import { describeSetting, type ModelSettingView } from '@/lib/model/client'

/**
 * Choosing where this organisation's model runs (ENT-236, §26.6).
 *
 * # THIS IS A CONFIRMATION SCREEN, NOT A SETTINGS ROW
 *
 * Everything about the layout follows from the toggle being a compliance event.
 * The consequence is above the fields rather than below the button, because a
 * warning under a button is one people read after deciding. The checkbox has no
 * default and the submit button says what it does rather than "Save". And the
 * sentence itself comes from core-api rather than from this file, so a
 * self-hoster's own client shows the same words and there is no second copy to
 * drift out of date.
 *
 * # THE KEY IS WRITE ONLY, AND THE FIELD SAYS SO
 *
 * There is nothing to pre-fill it with: no RPC on this surface returns a key,
 * so the console could not show one if it wanted to. What it shows instead is
 * the last four characters, which is enough to recognise which key is in place
 * and not enough to be one.
 */
export function ModelForm({
  slug,
  view,
  canManage,
}: {
  slug: string
  view: ModelSettingView
  canManage: boolean
}) {
  const [hostState, host] = useActionState<ActionState, FormData>(
    chooseHostedModelAction.bind(null, slug),
    idle,
  )
  const [revertState, revert] = useActionState<ActionState, FormData>(
    chooseBundledModelAction.bind(null, slug),
    idle,
  )

  const setting = view.setting
  const providers = view.permittedProviders ?? []
  const hosted = Boolean(setting?.hosted)

  return (
    <div className="space-y-8">
      <div className="rounded-md border border-border p-4">
        <p className="text-sm text-foreground">{describeSetting(setting)}</p>
        {hosted ? (
          <dl className="mt-3 space-y-1 text-xs text-muted-foreground">
            <div className="flex gap-2">
              <dt className="w-24 shrink-0">Endpoint</dt>
              <dd className="break-all">{setting?.baseUrl}</dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-24 shrink-0">Key</dt>
              <dd>
                {setting?.credentialLastFour
                  ? `ends ${setting.credentialLastFour}`
                  : 'none stored'}
              </dd>
            </div>
            <div className="flex gap-2">
              <dt className="w-24 shrink-0">In effect since</dt>
              <dd>{setting?.changedAt ?? 'unknown'}</dd>
            </div>
          </dl>
        ) : null}
      </div>

      {providers.length === 0 ? (
        /* Not an error and not an empty state. A deployment permitting no
           provider is one that can run with no outbound internet at all, which
           is the position a compliance buyer chose this product for, so it is
           described as a property rather than as something missing. */
        <p className="text-sm text-muted-foreground">
          This deployment permits no hosted model providers. Everything is
          processed by the model it runs itself, and an operator would have to
          change that in its configuration before anybody here could.
        </p>
      ) : !canManage ? (
        <p className="text-sm text-muted-foreground">
          Only an owner can change where this organisation is processed.
        </p>
      ) : (
        <>
          <form action={host} className="max-w-xl space-y-4">
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-4">
              <h3 className="text-sm font-medium text-foreground">
                What changes if you do this
              </h3>
              <p className="mt-2 text-sm text-muted-foreground">
                {view.consequenceNotice}
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="model-provider">Provider</Label>
              <select
                id="model-provider"
                name="provider"
                required
                defaultValue={setting?.provider ?? ''}
                className="h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm"
              >
                <option value="" disabled>
                  Choose a provider
                </option>
                {providers.map((provider) => (
                  <option key={provider.name} value={provider.name}>
                    {provider.name} ({provider.host})
                  </option>
                ))}
              </select>
              <p className="text-xs text-muted-foreground">
                Only providers this deployment permits are listed, and the
                endpoint below has to be on the host shown beside the name.
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="model-base-url">Endpoint</Label>
              <Input
                id="model-base-url"
                name="baseUrl"
                type="url"
                placeholder="https://api.example.com"
                defaultValue={setting?.baseUrl ?? ''}
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="model-name">Model</Label>
              <Input
                id="model-name"
                name="model"
                placeholder="the model name the provider knows"
                defaultValue={setting?.model ?? ''}
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="model-api-key">API key</Label>
              <Input
                id="model-api-key"
                name="apiKey"
                type="password"
                autoComplete="off"
                placeholder={
                  setting?.credentialLastFour
                    ? `ends ${setting.credentialLastFour}, enter a new key to replace it`
                    : 'the key this provider issued you'
                }
              />
              <p className="text-xs text-muted-foreground">
                Stored encrypted and never shown again. Leave it blank only if
                your provider needs no key.
              </p>
            </div>

            <div className="flex items-start gap-2">
              <input
                id="model-acknowledge"
                name="acknowledge"
                type="checkbox"
                className="mt-1"
              />
              <Label
                htmlFor="model-acknowledge"
                className="text-sm font-normal leading-snug"
              >
                I understand this sends this organisation&rsquo;s compliance
                data to the provider above, and that they become a sub-processor
                we are responsible for recording.
              </Label>
            </div>

            <Button type="submit" variant="destructive">
              {hosted ? 'Change provider' : 'Send our data to this provider'}
            </Button>

            {hostState.status !== 'idle' ? (
              <p
                role="status"
                className={
                  hostState.status === 'error'
                    ? 'text-sm text-destructive'
                    : 'text-sm text-muted-foreground'
                }
              >
                {hostState.message}
              </p>
            ) : null}
          </form>

          {hosted ? (
            <form
              action={revert}
              className="max-w-xl space-y-3 border-t border-border pt-6"
            >
              <h3 className="text-sm font-medium text-foreground">
                Go back to the model this deployment runs
              </h3>
              <p className="text-sm text-muted-foreground">
                {view.revertNotice}
              </p>
              {/* No confirmation checkbox on the way back. Stopping data
                  leaving is not a decision anybody needs protecting from, and
                  friction on the safe direction makes the unsafe one look
                  equivalent. */}
              <Button type="submit" variant="outline">
                Stop sending our data out
              </Button>

              {revertState.status !== 'idle' ? (
                <p
                  role="status"
                  className={
                    revertState.status === 'error'
                      ? 'text-sm text-destructive'
                      : 'text-sm text-muted-foreground'
                  }
                >
                  {revertState.message}
                </p>
              ) : null}
            </form>
          ) : null}
        </>
      )}
    </div>
  )
}
