'use client'

import * as React from 'react'
import { useActionState } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  chooseBundledModelAction,
  chooseHostedModelAction,
} from '@/app/(authed)/o/[org]/settings/model/actions'
import { idle, type ActionState } from '@/lib/org/action-state'
import { describeSetting, type ModelSettingView } from '@/lib/model/client'

/**
 * Choosing where this organisation's model runs (ENT-236, §26.6; the toggle in
 * ENT-281).
 *
 * # THE TOGGLE DISCLOSES, IT DOES NOT DECIDE
 *
 * ENT-236 shaped this surface as a confirmation rather than a settings row,
 * and that has not changed. Switching the toggle off reveals the provider
 * fields and reaches no RPC, because revealing a form is not a processing
 * decision. What makes the change is still the submit below it, and that still
 * requires the consequence notice core-api served and an acknowledgement with
 * no default. A toggle that wrote on flip would turn a compliance event into a
 * preference, which is the one thing this file must not do.
 *
 * Switching it back on is different, and deliberately so. Stopping data
 * leaving is the safe direction, so the toggle performs it directly: friction
 * on the safe way back makes the unsafe way look equivalent.
 *
 * # THE CONSEQUENCE IS ABOVE THE FIELDS, NOT UNDER THE BUTTON
 *
 * A warning under a button is one people read after deciding. The sentence
 * itself comes from core-api rather than from this file, so a self-hoster's
 * own client shows the same words and there is no second copy to drift.
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

  /* Revealed starts where the organisation already is. An owner arriving at a
     hosted organisation sees the fields, because the thing they came to do is
     usually change the provider or rotate the key, not read the toggle. */
  const [revealed, setRevealed] = React.useState(hosted)

  /* The identity of the stored decision, used to key the form below.
     The provider, endpoint and model fields are uncontrolled and seeded from
     `defaultValue`, and React keeps a field the person has typed into across a
     rerender. So once a submit succeeds and the page revalidates, the form
     goes on showing what was typed while the record says something else, and
     Base UI reports the default changing underneath it. Remounting on a
     changed setting re-seeds them from what was actually stored.

     Keyed on the setting rather than on a counter so the reverse holds too: a
     REFUSED submit changes nothing here, the key is stable, and nobody retypes
     an endpoint because they missed the acknowledgement. */
  const storedSetting = [
    setting?.provider ?? '',
    setting?.baseUrl ?? '',
    setting?.model ?? '',
    setting?.credentialLastFour ?? '',
    setting?.changedAt ?? '',
  ].join('|')
  const revertFormRef = React.useRef<HTMLFormElement>(null)
  const toggleLabelId = React.useId()

  function onToggle(usesBundledModel: boolean) {
    if (!usesBundledModel) {
      setRevealed(true)
      return
    }
    setRevealed(false)
    /* Only a live hosted choice has anything to revoke. Flipping back before
       submitting anything is somebody changing their mind about a form, and
       writing an audit row for that would put a decision nobody made into a
       regulatory record. */
    if (hosted) revertFormRef.current?.requestSubmit()
  }

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
           described as a property rather than as something missing. No toggle
           either: a switch that can only ever fail tells somebody the
           deployment has a choice it does not have. */
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
          <div className="flex items-start gap-3 rounded-md border border-border p-4">
            <Switch
              id="model-bundled"
              checked={!revealed}
              onCheckedChange={onToggle}
              aria-labelledby={toggleLabelId}
              className="mt-0.5"
            />
            <div className="space-y-1">
              <span
                id={toggleLabelId}
                className="text-sm leading-none font-medium text-foreground"
              >
                Use the model this deployment runs
              </span>
              <p className="text-xs text-muted-foreground">
                On, nothing about this organisation leaves this deployment. Turn
                it off to send this organisation&rsquo;s compliance data to a
                provider you name instead.
              </p>
            </div>
          </div>

          {hosted ? (
            /* The half an owner would otherwise assume, said before they flip
               it rather than after. */
            <form action={revert} ref={revertFormRef} className="max-w-xl">
              <p className="text-sm text-muted-foreground">
                {view.revertNotice}
              </p>
              {revertState.status !== 'idle' ? (
                <p
                  role="status"
                  className={
                    revertState.status === 'error'
                      ? 'mt-2 text-sm text-destructive'
                      : 'mt-2 text-sm text-muted-foreground'
                  }
                >
                  {revertState.message}
                </p>
              ) : null}
            </form>
          ) : null}

          {revealed ? (
            <form
              key={storedSetting}
              action={host}
              className="max-w-xl space-y-4"
            >
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
                {/* No default, and it does not survive the fields being
                    hidden: React unmounts this with the form, so somebody who
                    reveals it again ticks it again. */}
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
                  data to the provider above, and that they become a
                  sub-processor we are responsible for recording.
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
          ) : null}
        </>
      )}
    </div>
  )
}
