import { AgentRail } from '@/components/console/agent-rail'
import { ConsoleSidebar } from '@/components/console/sidebar'

/**
 * The console shell (ENT-222), replacing the header-only chrome.
 *
 * Three columns: navigation, the surface, the agents. Every authenticated page
 * renders inside it, so a surface arriving from the rebuild has somewhere to be
 * linked from and something around it.
 *
 * WHAT COLLAPSES, AND IN WHICH ORDER
 *
 * The rail goes first, below 1280px. It is context rather than content: losing
 * it costs you the pipeline's status, while losing the main column costs you
 * the thing you came for. The sidebar follows below 768px, where it becomes a
 * horizontal strip above the content rather than disappearing, because
 * navigation you cannot reach is worse than navigation that takes more room.
 *
 * The three columns are a grid rather than flexbox so the middle column is the
 * only one that flexes. With flex, a long organisation name or a wide table
 * pushes the fixed columns around; with grid, the fixed tracks are fixed and
 * the middle one absorbs everything.
 *
 * Still a plain synchronous component taking props, for the reason the header
 * it replaces was: the layout above is an async server component that reads a
 * session and calls core-api, and folding the chrome into it would mean
 * rendering React to test a tenancy decision.
 */
export function ConsoleShell({
  orgSlug,
  orgName,
  children,
}: {
  orgSlug: string
  orgName?: string
  children: React.ReactNode
}) {
  return (
    <div className="grid h-[100dvh] grid-rows-[auto_1fr] md:grid-cols-[15rem_1fr] md:grid-rows-1 xl:grid-cols-[15rem_1fr_20rem]">
      <div className="md:h-full">
        <ConsoleSidebar orgSlug={orgSlug} orgName={orgName} />
      </div>

      {/* A div, not a <main>. Every page already renders its own, and nesting
          landmarks is invalid and leaves a screen reader with two "main"
          regions to choose between.

          min-h-0 so a long page scrolls inside this column rather than
          stretching the grid and taking the sidebar off screen with it. */}
      <div className="min-h-0 overflow-y-auto">{children}</div>

      {/* Hidden rather than reflowed below xl. Reflowing it under the content
          would put "what the agents are doing" beneath a page fold nobody
          reaches, which is worse than admitting there is no room for it. */}
      <div className="hidden min-h-0 xl:block">
        <AgentRail />
      </div>
    </div>
  )
}
