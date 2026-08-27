import { AgentRail } from '@/components/console/agent-rail'
import { ConsoleSidebar } from '@/components/console/sidebar'
import { MobileHeader } from '@/components/console/mobile-header'
import { MobileTabs } from '@/components/console/mobile-tabs'

/**
 * The console shell (ENT-222), replacing the header-only chrome.
 *
 * Two layouts rather than one layout that bends, because a phone is not a
 * narrow desktop.
 *
 * ON A DESKTOP: three columns. Navigation, the surface, the agents. The rail
 * goes first, below 1280px: it is context rather than content, so losing it
 * costs you the pipeline's status while losing the main column costs you the
 * thing you came for.
 *
 * ON A PHONE, below 768px: a slim header carrying the organisation and the way
 * out, the surface, the agents beneath it, and navigation as a bottom tab bar
 * where a thumb reaches it.
 *
 * The first version simply stacked the sidebar above the content below `md`.
 * That was not a mobile layout, it was a desktop layout with the columns
 * unwrapped: a vertical list of labels, a "Coming next" heading and a sign-out
 * button consumed the entire first screen, and the page you had opened began
 * below the fold. Worth naming because it looked deliberate in the code and
 * only looked wrong in a browser.
 *
 * The rail moves rather than disappearing. On a desktop putting it under the
 * content would bury it beneath a fold nobody reaches; on a phone everything
 * is beneath a fold, so below the content is simply where it lives. "Has
 * anything looked at my compliance yet" should not be a desktop-only question.
 *
 * The three columns are a grid rather than flexbox so the middle column is the
 * only one that flexes. With flex, a long organisation name or a wide table
 * pushes the fixed columns around; with grid the fixed tracks are fixed.
 *
 * Still a plain synchronous component taking props: the layout above is an
 * async server component that reads a session and calls core-api, and folding
 * the chrome into it would mean rendering React to test a tenancy decision.
 */
export function ConsoleShell({
  orgSlug,
  orgName,
  activity,
  kindyAction,
  children,
}: {
  orgSlug: string
  orgName?: string
  /** The rail's Activity items, fetched by the layout. See AgentRail. */
  activity?: import('@/components/console/agent-rail').ActivityItem[]
  /** The composer's server action, injected by the layout. See AgentRail. */
  kindyAction: import('@/components/console/kindy-state').KindyAction
  children: React.ReactNode
}) {
  return (
    // ONE white sheet, edge to edge. This went through two framed cuts on the
    // way here: first the rail floated as a second card and the frame showed
    // through the gap as a dark border, then a single canvas kept a rim the
    // reference turned out not to have. There is no frame now. Navigation and
    // the surface share the sheet, divided by a hairline, and the agents are a
    // soft grey inset panel inside it, which is where the layout's depth
    // lives.
    // The explicit single row, at minmax(0,1fr): without it a long page
    // stretches the implicit row past the viewport and the whole console
    // scrolls, sidebar and all, instead of the middle column alone.
    <div className="flex h-[100dvh] min-h-0 flex-col bg-background md:grid md:grid-cols-[15rem_1fr] md:grid-rows-[minmax(0,1fr)] xl:grid-cols-[15rem_1fr_21.5rem]">
      {/* Hidden on a phone: its job is done by the header and the tab bar. */}
      <div className="hidden md:block md:h-full md:min-h-0">
        <ConsoleSidebar orgSlug={orgSlug} orgName={orgName} />
      </div>

      {/* The scrolling column, and on a phone the whole screen. A flex column
          so the tab bar can sit at the end and stick there.

          A div, not a <main>: every page already renders its own, and nesting
          landmarks leaves a screen reader two "main" regions to choose
          between. */}
      <div className="flex h-full min-h-0 flex-col overflow-y-auto">
        <MobileHeader orgSlug={orgSlug} orgName={orgName} />

        <div className="flex-1">{children}</div>

        {/* Below the content on a phone, where the rail cannot be a column.
            Hidden from md up, because from there it either has its own column
            or has been dropped deliberately. */}
        <div className="md:hidden">
          <AgentRail
            orgSlug={orgSlug}
            variant="mobile"
            activity={activity}
            kindyAction={kindyAction}
          />
        </div>

        <MobileTabs orgSlug={orgSlug} />
      </div>

      {/* The agents, as an inset rather than an island: a grey rounded panel
          inside the sheet, the way the reference draws its right column. */}
      <div className="hidden h-full min-h-0 p-3 pl-0 xl:block">
        {/* A touch deeper than --muted, which on the white sheet read as
            white and lost the panel: the inset has to be visibly a panel
            for the white cards inside it to read as cards. */}
        <div className="h-full min-h-0 overflow-hidden rounded-2xl bg-[oklch(0.951_0.005_84)]">
          <AgentRail
            orgSlug={orgSlug}
            activity={activity}
            kindyAction={kindyAction}
          />
        </div>
      </div>
    </div>
  )
}
