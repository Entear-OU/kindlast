'use client'

import { CheckCircle2Icon, ShieldAlertIcon, XCircleIcon } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import type { PostureSummary, PostureSeverity } from '@/lib/onboarding/posture-summary'

/**
 * Posture summary card (ENT-46).
 *
 * Renders the green/red posture lists plus the highest-priority draft
 * finding inline at the end of onboarding. The Approve button is a
 * deliberate stub in this PR: there is no `findings` (or equivalent)
 * table yet, and adding one would expand scope past this ticket. When
 * the founder clicks Approve we surface a confirmation toast and flip
 * local state so the card stops offering the action — but a page reload
 * resets the approved state. The follow-up that introduces durable
 * finding rows will:
 *
 *   (a) hydrate `initialApproved` from the row's `status`, and
 *   (b) replace the toast-only handler with a server action.
 *
 * The prop shape is already plumbed so that follow-up can be a narrow
 * change rather than a refactor.
 */

const severityBadgeVariant: Record<PostureSeverity, 'destructive' | 'default' | 'secondary'> = {
  critical: 'destructive',
  high: 'destructive',
  medium: 'default',
  low: 'secondary',
}

export function PostureSummaryCard({
  summary,
  initialApproved = false,
}: {
  summary: PostureSummary
  initialApproved?: boolean
}) {
  const [approved, setApproved] = useState(initialApproved)

  function handleApprove() {
    setApproved(true)
    toast.success('Action queued', {
      description:
        "We'll surface this in your dashboard once your agent feed goes live.",
    })
  }

  return (
    <Card className="border-foreground/10 bg-card">
      <CardHeader>
        <CardTitle className="text-lg font-semibold">
          Your initial compliance posture
        </CardTitle>
        <CardDescription>
          Based on what you shared — here&apos;s where you stand and what to do
          first.
        </CardDescription>
      </CardHeader>

      <CardContent className="flex flex-col gap-6">
        <div className="grid gap-6 sm:grid-cols-2">
          <section aria-labelledby="posture-covered-heading">
            <h3
              id="posture-covered-heading"
              className="mb-3 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground"
            >
              Covered
            </h3>
            <ul className="flex flex-col gap-2.5">
              {summary.covered.map((item) => (
                <li
                  key={item.key}
                  className="flex items-start gap-2.5 text-sm"
                >
                  <CheckCircle2Icon
                    className="mt-0.5 size-4 shrink-0 text-emerald-600"
                    aria-label="Covered"
                  />
                  <div className="flex flex-col">
                    <span className="font-medium text-foreground">
                      {item.label}
                    </span>
                    {item.detail && (
                      <span className="text-xs text-muted-foreground">
                        {item.detail}
                      </span>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          </section>

          <section aria-labelledby="posture-missing-heading">
            <h3
              id="posture-missing-heading"
              className="mb-3 text-xs font-semibold uppercase tracking-[0.16em] text-muted-foreground"
            >
              Missing
            </h3>
            {summary.missing.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                No gaps surfaced from your answers.
              </p>
            ) : (
              <ul className="flex flex-col gap-2.5">
                {summary.missing.map((item) => (
                  <li
                    key={item.key}
                    className="flex items-start gap-2.5 text-sm"
                  >
                    <XCircleIcon
                      className="mt-0.5 size-4 shrink-0 text-rose-600"
                      aria-label="Missing"
                    />
                    <div className="flex flex-col">
                      <span className="font-medium text-foreground">
                        {item.label}
                      </span>
                      {item.detail && (
                        <span className="text-xs text-muted-foreground">
                          {item.detail}
                        </span>
                      )}
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </section>
        </div>

        <section
          aria-labelledby="posture-top-action-heading"
          className="rounded-lg border border-foreground/10 bg-muted/40 p-4"
        >
          <div className="flex items-start gap-3">
            <ShieldAlertIcon
              className="mt-0.5 size-4 shrink-0 text-foreground/70"
              aria-hidden
            />
            <div className="flex flex-1 flex-col gap-2">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">
                  Highest-priority action
                </span>
                <Badge variant={severityBadgeVariant[summary.topAction.severity]}>
                  {summary.topAction.severity}
                </Badge>
                <Badge variant="outline">{summary.topAction.regulation}</Badge>
              </div>
              <h3
                id="posture-top-action-heading"
                className="font-semibold text-foreground"
              >
                {summary.topAction.title}
              </h3>
              <p className="text-sm text-muted-foreground">
                {summary.topAction.description}
              </p>
              <div className="mt-1 flex items-center gap-2">
                {approved ? (
                  <Badge
                    variant="default"
                    className="bg-emerald-600 text-white"
                  >
                    Approved
                  </Badge>
                ) : (
                  <Button
                    type="button"
                    size="sm"
                    onClick={handleApprove}
                  >
                    Approve
                  </Button>
                )}
              </div>
            </div>
          </div>
        </section>
      </CardContent>
    </Card>
  )
}
