import { track } from '@vercel/analytics'

/**
 * Thin wrapper over Vercel Analytics custom events (ENT-82).
 *
 * Centralizes upgrade-funnel event names so call sites can't drift, and keeps
 * the analytics dependency mockable in component tests. Vercel's `track` only
 * accepts primitive property values, so we build the payload explicitly and drop
 * any undefined counts rather than spreading them through.
 */

export type UpgradeSource =
  | 'finding_cap'
  | 'finding_detail'
  | 'executor_approve'
  | 'ropa_cap'

export interface UpgradePromptProps {
  source: UpgradeSource
  /** How many items are locked behind the prompt (when applicable). */
  lockedCount?: number
  /** Total items in scope, used for the trigger-context message. */
  totalCount?: number
}

function payload(props: UpgradePromptProps): Record<string, string | number> {
  const out: Record<string, string | number> = { source: props.source }
  if (typeof props.lockedCount === 'number') out.lockedCount = props.lockedCount
  if (typeof props.totalCount === 'number') out.totalCount = props.totalCount
  return out
}

/** The founder saw an upgrade prompt (AC: tracking fires when the prompt shows). */
export function trackUpgradePromptShown(props: UpgradePromptProps): void {
  track('upgrade_prompt_shown', payload(props))
}

/** The founder acted on an upgrade prompt (AC: tracking fires when it converts). */
export function trackUpgradeConverted(props: UpgradePromptProps): void {
  track('upgrade_prompt_converted', payload(props))
}
