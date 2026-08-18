/**
 * The four agents, as the console is allowed to describe them (ENT-232).
 *
 * §26.5 defines an agent as a skill plus a tool allow-list plus a workflow,
 * addressable by name in the rail. Three of the four are not that yet, and this
 * file is where the console keeps track of which is which.
 *
 * # WHY THIS IS NOT FOUR CHEERFUL CARDS
 *
 * The rail before this change said "Not scheduled yet" under all four, which
 * was true the day it was written. ENT-218 then shipped the Analyst as a real
 * skill on a real harness and the rail went on saying it, because a single
 * sentence about four different things is wrong the moment one of them moves.
 *
 * The tempting fix is the opposite one: name all four, give them all a status
 * dot, and let the reader assume. `AGENTS.md` opens by saying that anything
 * fabricating a claim is worse than nothing, because the product's value is
 * that a human can check it. An agent rail is a claim about what has looked at
 * your compliance. So each of the four carries what is true of it alone:
 *
 *   The Watcher    runs, but as fixed rules rather than a skill. No tool list,
 *                  no run record.
 *   The Analyst    runs as a skill, under a budget and a citation validator,
 *                  and leaves an `agent_runs` row. Nothing calls it on a
 *                  schedule.
 *   The Messenger  does not exist.
 *   The Hands      does not exist.
 *
 * # AND WHY THE UNBUILT TWO ARE HERE AT ALL
 *
 * Because leaving them out would answer a different question. A person looking
 * at this rail wants to know what the product will and will not do for them,
 * and "we have not built the thing that would email you" is an answer. What
 * would be dishonest is drawing them as though they were waiting on a
 * schedule. Their copy is written in the conditional for exactly that reason:
 * the Messenger "would" send, because it cannot.
 *
 * # WHERE THE SKILL FACTS COME FROM
 *
 * `apps/intelligence/src/kindlast_intelligence/skills/analyst.py`, which is the
 * single declaration of that skill's name, version and allow-list. Repeated
 * here because there is no RPC to ask over: `agent_runs` has a write path
 * (`IngestService.RecordAgentRun`) and no read path, so the console cannot
 * fetch this and cannot show a customer the runs it has made. The repetition is
 * guarded rather than trusted: `__tests__/lib/agents/catalog.test.ts` reads the
 * Python and fails when the two disagree.
 */

/**
 * How far along an agent is, in terms of what a person gets from it.
 *
 * Three states rather than a boolean, because the Watcher is the interesting
 * case: it does look at your compliance, and it is not a skill, and collapsing
 * those into "working" or "not working" loses the half that matters when
 * somebody asks why there is no record of what it did.
 */
export type AgentStatus = 'working' | 'partly-working' | 'not-built'

/** The words the console uses for each. One place, so the rail and the page agree. */
export const STATUS_LABEL: Record<AgentStatus, string> = {
  working: 'Working',
  'partly-working': 'Working, in part',
  'not-built': 'Not built yet',
}

/** What an agent's skill declares about itself. */
export interface AgentSkill {
  /**
   * The module under `skills/`. Load-bearing rather than documentation: the
   * catalogue test uses it to assert that the console claims a skill for
   * exactly the skills that exist on disk, in both directions.
   */
  module: string
  /** `NAME`, which is what `agent_runs.skill` stores. */
  name: string
  /** `VERSION`. Recorded per run, so a run is reproducible. */
  version: string
  /**
   * `ALLOWED_TOOLS`. Empty is a statement, not a gap: a skill given everything
   * it needs up front dispatches nothing, and a request for a tool it was
   * never offered is refused rather than retried.
   */
  tools: readonly string[]
}

export interface Agent {
  /** Addressable by name: `/o/{org}/agents/{slug}`. */
  slug: string
  /** As the product names it publicly. */
  name: string
  does: string
  status: AgentStatus
  /** What actually starts it today. Present tense for the two that run. */
  runs: string
  /** What it may change. The bound, in a person's words rather than a scope name. */
  effects: string
  /** What is missing. Absent only for an agent with nothing missing. */
  remaining?: string
  /** Absent unless a skill exists. Never an empty object standing in for one. */
  skill?: AgentSkill
}

/**
 * In the order work flows through them, which is the order the rail draws.
 *
 * Not alphabetical and not by how finished they are: the rail is a pipeline,
 * and reordering it would describe a product where the Messenger speaks before
 * the Analyst has decided anything.
 */
export const AGENTS: readonly Agent[] = [
  {
    slug: 'watcher',
    name: 'The Watcher',
    does: 'Reads your profile against the obligations that apply to you.',
    status: 'partly-working',
    // `SweepService.RunSweep` is the only thing that starts it. The pg_cron
    // schedules went with Supabase in ENT-200 and the schedules return with
    // Temporal at build-order step 8.
    runs: 'When a sweep is triggered for your organisation. Nothing triggers one on a schedule yet.',
    effects:
      'Writes evidence and signals. It never changes a record and never sends anything.',
    // ENT-231 gives it the integrations gateway, which is the point at which
    // deciding what to look at becomes a decision rather than a fixed query.
    remaining:
      'It follows fixed rules today rather than a skill, so it has no tool list and leaves no run record you can read.',
  },
  {
    slug: 'analyst',
    name: 'The Analyst',
    does: 'Turns what was found into a finding that cites the article.',
    status: 'working',
    runs: 'When something asks it for a narrative. Nothing asks it on a schedule yet.',
    effects:
      'Records how it worked, and nothing else. What it drafts comes back for something else to store, so it cannot write a finding itself.',
    // The write path exists (`RecordAgentRun`); the read path does not, so the
    // console has nothing to ask for. Proposed in the ENT-232 PR body.
    remaining: 'The console cannot yet show you the runs it has made.',
    skill: {
      module: 'analyst',
      name: 'analyst.narrative',
      version: '1.0.0',
      tools: [],
    },
  },
  {
    slug: 'messenger',
    name: 'The Messenger',
    does: 'Tells you when something needs a decision.',
    status: 'not-built',
    runs: 'Never yet.',
    effects:
      'It would draft a message and send it only through a channel you had verified. It would never hold a mail or chat credential of its own.',
    // ENT-219 (the transactional outbox and its dispatcher) and ENT-209
    // (preferences and the dispatch paths) come first. Without them there is
    // nowhere for a send to go that could be bounded.
    remaining:
      'The outbox and the verified channels it would send through do not exist yet.',
  },
  {
    slug: 'hands',
    name: 'The Hands',
    does: 'Explains what approving will do, then prepares the record.',
    status: 'not-built',
    runs: 'Never yet.',
    effects:
      'It would prepare a record only after you approved it. The approval stays yours, and it never decides.',
    // ENT-225 phase 1 moves record creation out of plpgsql into Go, which is
    // what turns "create the record" into a step something could prepare.
    remaining:
      'Creating a record is still a database function rather than a step it could prepare and show you.',
  },
]

/**
 * The agent behind a URL segment, or nothing.
 *
 * Exact match, deliberately. No lowercasing, no trimming, no nearest
 * neighbour: the citation validator gives the reason in full, and it applies
 * here too. Helping turns "this does not exist" into "this nearly exists", and
 * a URL naming an agent we do not have should be a 404 rather than a page
 * about a different one.
 */
export function agentBySlug(slug: string): Agent | undefined {
  return AGENTS.find((agent) => agent.slug === slug)
}
