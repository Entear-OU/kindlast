/**
 * Where this organisation's model runs, from web's side (ENT-236, §26.6).
 *
 * # THE CONSOLE HOLDS NO KEY AND WRITES NO WARNING
 *
 * Two properties this file exists to keep, and both look like omissions.
 *
 * The provider key travels from a form to a server action to core-api and is
 * never read back. There is no `apiKey` on `ModelSetting`, no field to
 * pre-fill, and nothing here that could render one, so a page cannot show a
 * key it never received. `credentialLastFour` is what a person recognises the
 * key by.
 *
 * And the sentence describing what turning this on means comes from core-api
 * as `consequenceNotice`. Writing it here would be a second copy that drifts
 * from what the product does, and a self-hoster's own client would show none
 * at all. See the service comment in `model.proto`.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

/** One option this deployment permits. Empty means BYOK is switched off here. */
export interface PermittedProvider {
  name?: string
  /** The host an endpoint must have. A leading dot makes it a suffix. */
  host?: string
}

export interface ModelSetting {
  /** False means the model this deployment runs itself, and nothing leaves. */
  hosted?: boolean
  provider?: string
  baseUrl?: string
  model?: string
  /** The last four characters of the key. NEVER the key. */
  credentialLastFour?: string
  /** proto3 JSON renders a Timestamp as an RFC 3339 string. */
  changedAt?: string
  changedByUserId?: string
}

export interface ModelSettingView {
  setting?: ModelSetting
  permittedProviders?: PermittedProvider[]
  consequenceNotice?: string
  revertNotice?: string
}

export function getModelSetting(accessToken: string, orgId: string) {
  return call<ModelSettingView>(
    'kindlast.core.v1.ModelService/GetModelSetting',
    { accessToken, orgId },
  )
}

export function chooseHostedModel(
  accessToken: string,
  orgId: string,
  input: {
    provider: string
    baseUrl: string
    model: string
    apiKey?: string
    acknowledgeConsequence: boolean
  },
) {
  return call<{ setting?: ModelSetting; auditEntryId?: string }>(
    'kindlast.core.v1.ModelService/UseHostedModel',
    { accessToken, orgId, body: input },
  )
}

export function chooseBundledModel(accessToken: string, orgId: string) {
  return call<{ setting?: ModelSetting; auditEntryId?: string }>(
    'kindlast.core.v1.ModelService/UseBundledModel',
    { accessToken, orgId, body: {} },
  )
}

/**
 * How the current setting reads to a person.
 *
 * The bundled case is deliberately not phrased as an absence. "No provider
 * configured" would read as something missing, when it is the state in which
 * this deployment can run with no outbound internet at all, which is the
 * stronger position and the one a compliance buyer chose the product for.
 */
export function describeSetting(setting: ModelSetting | undefined): string {
  if (!setting?.hosted) {
    return 'This organisation uses the model this deployment runs. Nothing leaves it.'
  }
  const provider = setting.provider || 'a hosted provider'
  const model = setting.model ? ` (${setting.model})` : ''
  return `Findings for this organisation are drafted by ${provider}${model}, outside this deployment.`
}

/** Whether an operator has permitted anything at all. */
export function byokAvailable(view: ModelSettingView | undefined): boolean {
  return (view?.permittedProviders?.length ?? 0) > 0
}
