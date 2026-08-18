/**
 * Connecting a customer's own systems, from web's side (ENT-231, §26.4).
 *
 * # NOTHING HERE TALKS TO A CUSTOMER'S SYSTEM
 *
 * Every call goes to core-api, which goes to the gateway, which is the only
 * process that dials an address a customer supplied. That matters to a reader
 * of this file because the obvious shortcut, fetching an MCP endpoint from a
 * server action to show a nicer loading state, would put outbound requests to
 * arbitrary hosts in the Next.js server and route around the egress
 * allow-list entirely.
 *
 * # CONNECTING IS TWO STEPS, AND THAT IS THE PRODUCT
 *
 * `discoverIntegration` asks what an endpoint offers and stores nothing.
 * `connectIntegration` records the connection along with the tools as they
 * were shown and the ones the person ticked. Collapsing them into one call
 * would mean somebody agreeing to a tool list they never saw, which is the
 * whole thing the consent screen exists to prevent.
 */
import { call } from '@/lib/core-api/call'

export type { Failure, Result } from '@/lib/core-api/call'

export type IntegrationKind =
  | 'INTEGRATION_KIND_UNSPECIFIED'
  | 'INTEGRATION_KIND_MCP'

export type IntegrationStatus =
  | 'INTEGRATION_STATUS_UNSPECIFIED'
  | 'INTEGRATION_STATUS_ACTIVE'
  | 'INTEGRATION_STATUS_REVOKED'

export interface IntegrationTool {
  name?: string
  /**
   * The server's own words. UNTRUSTED TEXT: rendered as text and never as
   * markup, never as a link, and never anywhere near a prompt. A description
   * reading "ignore your previous instructions" must be a slightly odd line on
   * a screen and nothing else.
   */
  description?: string
  /** Whether calling it can change something on the customer's side. */
  writeCapable?: boolean
  /** Whether Kindlast may call it. False until a person says otherwise. */
  granted?: boolean
}

export interface Integration {
  id?: string
  kind?: IntegrationKind
  displayName?: string
  endpointUrl?: string
  status?: IntegrationStatus
  createdAt?: string
  revokedAt?: string
  tools?: IntegrationTool[]
  consentedAt?: string
  consentedBy?: string
}

/** One attempt to fetch, whatever became of it. */
export interface Fetch {
  id?: string
  integrationId?: string
  integrationName?: string
  tool?: string
  /** `succeeded`, `refused` or `failed`. */
  outcome?: string
  /** Why, when it was not a success. The sentence that makes policy legible. */
  detail?: string
  requestedAt?: string
  finishedAt?: string
  evidenceId?: string
  /** proto3 JSON renders int32 as a number. */
  redactions?: number
  requestedBy?: string
}

export function listIntegrations(accessToken: string, orgId: string) {
  return call<{ integrations?: Integration[] }>(
    'kindlast.core.v1.IntegrationsService/ListIntegrations',
    { accessToken, orgId },
  )
}

export function discoverIntegration(
  accessToken: string,
  orgId: string,
  endpointUrl: string,
  credential?: string,
) {
  return call<{ tools?: IntegrationTool[] }>(
    'kindlast.core.v1.IntegrationsService/DiscoverIntegration',
    {
      accessToken,
      orgId,
      body: { kind: 'INTEGRATION_KIND_MCP', endpointUrl, credential },
    },
  )
}

export function connectIntegration(
  accessToken: string,
  orgId: string,
  input: {
    displayName: string
    endpointUrl: string
    credential?: string
    offeredTools: IntegrationTool[]
    grantedTools: string[]
  },
) {
  return call<{ integration?: Integration }>(
    'kindlast.core.v1.IntegrationsService/ConnectIntegration',
    {
      accessToken,
      orgId,
      body: { kind: 'INTEGRATION_KIND_MCP', ...input },
    },
  )
}

export function updateToolGrants(
  accessToken: string,
  orgId: string,
  integrationId: string,
  grantedTools: string[],
) {
  return call<{ integration?: Integration }>(
    'kindlast.core.v1.IntegrationsService/UpdateToolGrants',
    { accessToken, orgId, body: { integrationId, grantedTools } },
  )
}

export function revokeIntegration(
  accessToken: string,
  orgId: string,
  integrationId: string,
) {
  return call<{ integration?: Integration }>(
    'kindlast.core.v1.IntegrationsService/RevokeIntegration',
    { accessToken, orgId, body: { integrationId } },
  )
}

export function listFetches(
  accessToken: string,
  orgId: string,
  integrationId?: string,
) {
  return call<{ fetches?: Fetch[]; nextPageToken?: string }>(
    'kindlast.core.v1.IntegrationsService/ListFetches',
    { accessToken, orgId, body: { integrationId, pageSize: 50 } },
  )
}

/**
 * How a fetch outcome reads to a person.
 *
 * `refused` deliberately does not read as an error. It is what a working
 * control produces, and a customer seeing a red "failed" beside every refusal
 * would conclude the product is broken rather than that it declined.
 */
export const OUTCOME_LABELS: Record<string, string> = {
  succeeded: 'Fetched',
  refused: 'Declined',
  failed: 'Could not reach',
}

/** Whether the connection is still one Kindlast may use. */
export function isActive(integration: Integration): boolean {
  return integration.status === 'INTEGRATION_STATUS_ACTIVE'
}

/**
 * The tools a connection may call, in the order a person reads them.
 *
 * Write-capable ones first, because they are the ones worth checking. A list
 * sorted alphabetically buries the one tool that can change something between
 * nine that cannot.
 */
export function toolsByRisk(tools: IntegrationTool[]): IntegrationTool[] {
  return [...tools].sort((a, b) => {
    if (Boolean(a.writeCapable) !== Boolean(b.writeCapable)) {
      return a.writeCapable ? -1 : 1
    }
    return (a.name ?? '').localeCompare(b.name ?? '')
  })
}
