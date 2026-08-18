import {
  isActive,
  toolsByRisk,
  type Integration,
  type IntegrationTool,
} from '@/lib/integrations/client'

/**
 * What Kindlast can reach, and what it may do there (ENT-231).
 *
 * # EVERY TOOL IS LISTED, NOT ONLY THE GRANTED ONES
 *
 * A list showing only what Kindlast may call would answer half the question.
 * The other half, and the one somebody reviewing a connection is actually
 * asking, is what it COULD have been given: a helpdesk exposing a
 * `delete_queue` tool that nobody granted is a materially different thing to
 * connect from one that exposes only searches, and a screen that hid the
 * ungranted tools would make the two look identical.
 *
 * # A WRITE-CAPABLE TOOL SAYS SO WHEREVER IT APPEARS
 *
 * Not only where it is granted. The label is on the tool, because the question
 * "what can this connection do" is about the endpoint and the question "what
 * may Kindlast do" is about the grant, and collapsing them is how somebody
 * ticks a box without noticing what it means.
 *
 * # REVOKED CONNECTIONS STAY IN THE LIST
 *
 * Greyed and labelled, never removed. A customer asking what Kindlast has been
 * able to reach is asking about the past as well as the present.
 */
export function ConnectionList({
  integrations,
}: {
  integrations: Integration[]
}) {
  return (
    <ul className="mt-3 divide-y divide-border/60 rounded-xl border border-border/60 bg-background">
      {integrations.map((integration) => (
        <li
          key={integration.id}
          className={isActive(integration) ? 'p-4' : 'p-4 opacity-60'}
        >
          <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
            <p className="text-sm font-medium text-foreground">
              {integration.displayName}
            </p>
            <p className="text-xs text-muted-foreground">
              {isActive(integration)
                ? 'Connected'
                : `Revoked${integration.revokedAt ? ` on ${formatDay(integration.revokedAt)}` : ''}`}
            </p>
          </div>

          {/*
            The endpoint as text, never as a link. It is a URL a customer typed
            and it is shown so they can check it; making it clickable would
            turn a page in the console into a way to have somebody's browser
            fetch an arbitrary address, which is the same class of problem the
            egress allow-list exists for one layer down.
          */}
          <p className="mt-1 break-all font-mono text-xs text-muted-foreground">
            {integration.endpointUrl}
          </p>

          {integration.consentedAt ? (
            <p className="mt-1 text-xs text-muted-foreground">
              Agreed on{' '}
              <time dateTime={integration.consentedAt}>
                {formatDay(integration.consentedAt)}
              </time>
            </p>
          ) : null}

          <ToolSummary tools={integration.tools ?? []} />
        </li>
      ))}
    </ul>
  )
}

function ToolSummary({ tools }: { tools: IntegrationTool[] }) {
  if (tools.length === 0) {
    return (
      <p className="mt-2 text-xs text-muted-foreground">
        This connection exposed no tools.
      </p>
    )
  }

  return (
    <ul className="mt-2 space-y-1">
      {toolsByRisk(tools).map((tool) => (
        <li key={tool.name} className="text-xs">
          <span className="font-mono text-foreground">{tool.name}</span>
          {tool.writeCapable ? (
            <span className="ml-2 text-muted-foreground">can change data</span>
          ) : (
            <span className="ml-2 text-muted-foreground">read only</span>
          )}
          <span className="ml-2 text-muted-foreground">
            {tool.granted ? 'Kindlast may call it' : 'not allowed'}
          </span>
        </li>
      ))}
    </ul>
  )
}

function formatDay(iso: string): string {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) return iso
  return parsed.toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  })
}
