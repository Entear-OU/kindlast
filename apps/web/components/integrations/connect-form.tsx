'use client'

import { useActionState } from 'react'

import { toolsByRisk, type IntegrationTool } from '@/lib/integrations/client'

/**
 * Connecting an endpoint, in two steps (ENT-231).
 *
 * # THE CONSENT SCREEN IS THE SECOND STEP AND CANNOT BE SKIPPED
 *
 * The first submit asks the endpoint what it offers and stores nothing. Only
 * then does the form list every tool with what it can do, and only what is
 * ticked here is ever callable. Collapsing this into one submit would mean
 * somebody agreeing to a tool list they never saw, which is the whole thing
 * this screen exists to prevent.
 *
 * # NOTHING IS TICKED FOR THEM
 *
 * Not the read-only tools either, although that would be defensible and would
 * save a click. A screen that arrives with boxes already ticked is a screen
 * people click past, and the value of this one is entirely in it being read.
 *
 * # A WRITE-CAPABLE TOOL SAYS SO NEXT TO ITS BOX
 *
 * In the label, not in a footnote. The distinction between "this reads your
 * helpdesk" and "this can close tickets in your helpdesk" is the only thing on
 * this screen a person genuinely has to weigh.
 */
export interface ConnectState {
  status: 'idle' | 'ok' | 'error'
  message: string
  /** Filled in once discovery has run: the tools, as they will be shown. */
  endpointUrl?: string
  displayName?: string
  tools?: IntegrationTool[]
}

export const connectIdle: ConnectState = { status: 'idle', message: '' }

export function ConnectForm({
  slug,
  connect,
}: {
  slug: string
  connect: (
    slug: string,
    previous: ConnectState,
    form: FormData,
  ) => Promise<ConnectState>
}) {
  const [state, action, pending] = useActionState(
    connect.bind(null, slug),
    connectIdle,
  )

  const discovered = state.tools ?? []

  return (
    <form action={action} className="mt-3 space-y-4">
      {/*
        `step` is what tells the action which half to run. It is a form field
        rather than two separate actions because the second half needs what the
        first found, and React's form state is the only thing carrying it
        across the round trip.
      */}
      <input
        type="hidden"
        name="step"
        value={discovered.length > 0 ? 'connect' : 'discover'}
      />

      <div className="space-y-2">
        <label
          htmlFor="displayName"
          className="block text-xs font-medium text-foreground"
        >
          What to call it
        </label>
        <input
          id="displayName"
          name="displayName"
          defaultValue={state.displayName ?? ''}
          readOnly={discovered.length > 0}
          required
          className="w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm"
          placeholder="Helpdesk"
        />
      </div>

      <div className="space-y-2">
        <label
          htmlFor="endpointUrl"
          className="block text-xs font-medium text-foreground"
        >
          The MCP endpoint your team runs
        </label>
        <input
          id="endpointUrl"
          name="endpointUrl"
          type="url"
          defaultValue={state.endpointUrl ?? ''}
          readOnly={discovered.length > 0}
          required
          className="w-full rounded-lg border border-border/60 bg-background px-3 py-2 font-mono text-sm"
          placeholder="https://tools.example.com/mcp"
        />
        <p className="text-xs text-muted-foreground">
          Only hosts your operator has permitted can be reached. Anything else
          is refused before a request leaves.
        </p>
      </div>

      <div className="space-y-2">
        <label
          htmlFor="credential"
          className="block text-xs font-medium text-foreground"
        >
          A token, if the endpoint needs one
        </label>
        <input
          id="credential"
          name="credential"
          type="password"
          autoComplete="off"
          className="w-full rounded-lg border border-border/60 bg-background px-3 py-2 text-sm"
        />
        <p className="text-xs text-muted-foreground">
          Encrypted before it is stored, and never shown again. Leave it empty
          for an endpoint on your own network that needs none.
        </p>
      </div>

      {discovered.length > 0 ? (
        <fieldset className="space-y-2 rounded-xl border border-border/60 p-4">
          <legend className="px-1 text-xs font-medium text-foreground">
            What Kindlast may call
          </legend>
          <p className="text-xs text-muted-foreground">
            This endpoint offers {discovered.length}{' '}
            {discovered.length === 1 ? 'tool' : 'tools'}. Kindlast will call
            only the ones you tick.
          </p>

          {/*
            The tools travel back as JSON so what is stored as consent is what
            was actually on the screen, rather than the result of a second
            discovery that could return a different list.

            A tampered field here changes the LABEL a tool is stored under and
            not what it may do: the gateway reads the endpoint's own annotation
            again before every call and takes the stricter of the two, so a
            write tool relabelled read-only is still refused.
          */}
          <input
            type="hidden"
            name="offeredTools"
            value={JSON.stringify(discovered)}
          />

          <ul className="space-y-2">
            {toolsByRisk(discovered).map((tool) => (
              <li key={tool.name} className="flex items-start gap-3">
                <input
                  id={`tool-${tool.name}`}
                  type="checkbox"
                  name="grantedTools"
                  value={tool.name}
                  className="mt-1"
                />
                <label
                  htmlFor={`tool-${tool.name}`}
                  className="text-xs text-foreground"
                >
                  <span className="font-mono">{tool.name}</span>
                  <span className="ml-2 text-muted-foreground">
                    {tool.writeCapable
                      ? 'can change data in that system'
                      : 'read only'}
                  </span>
                  {tool.description ? (
                    <span className="mt-0.5 block text-muted-foreground">
                      {tool.description}
                    </span>
                  ) : null}
                </label>
              </li>
            ))}
          </ul>
        </fieldset>
      ) : null}

      <button
        type="submit"
        disabled={pending}
        className="rounded-lg border border-border/60 px-3 py-2 text-sm font-medium text-foreground disabled:opacity-60"
      >
        {discovered.length > 0 ? 'Connect' : 'See what it offers'}
      </button>

      {state.status !== 'idle' && state.message ? (
        <p
          role="status"
          className={
            state.status === 'error'
              ? 'text-xs text-destructive'
              : 'text-xs text-muted-foreground'
          }
        >
          {state.message}
        </p>
      ) : null}
    </form>
  )
}
