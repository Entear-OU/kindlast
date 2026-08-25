/**
 * The four agents, as the console is allowed to describe them (ENT-232).
 *
 * §26.5 defines an agent as a skill plus a tool allow-list plus a workflow,
 * addressable by name in the rail. They arrived at different times and one is
 * still missing a half, and this file is where the console keeps track of
 * which is which.
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
 *   The Messenger  runs as a skill (ENT-260): it writes the words of a
 *                  message and hands them to the dispatch path, and it cannot
 *                  send. Nothing asks it for a draft yet.
 *   The Hands      runs as a skill (ENT-261) a person can reach (ENT-278):
 *                  it explains what approving will do and prepares the
 *                  record, and it cannot approve.
 *
 * # AND WHY A HALF-BUILT AGENT IS HERE AT ALL
 *
 * Because leaving it out would answer a different question. A person looking
 * at this rail wants to know what the product will and will not do for them,
 * and "the thing that would email you is built and nothing calls it yet" is
 * an answer. What would be dishonest is drawing it as though it were
 * finished, which is why the Messenger does not say "Working" and says what
 * is missing.
 *
 * The conditional wording this section used to describe went with ENT-260:
 * the Messenger no longer "would" draft, it does. What it still cannot do is
 * cause a message to exist, and that is a sentence about its authority rather
 * than about how far along it is.
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
  /**
   * Every skill this agent runs, or nothing.
   *
   * A LIST BECAUSE THE ANALYST HAS TWO NOW (ENT-270), and singular is what it
   * would have become at the first agent that grew a second one anyway. The
   * Analyst narrates a finding on a job and answers a question about one in the
   * console, and those are two modules, two versions and two rows in
   * `agent_runs`. Collapsing them into whichever came first would put a version
   * number on this page that no run of the other skill ever recorded.
   *
   * Absent rather than empty when there is no skill at all. An empty tool list
   * is a statement the Analyst makes on purpose; an empty SKILL list would
   * claim a guardrail with nothing behind it, which is the opposite claim.
   * Every agent has a skill since ENT-260, so nothing takes this branch today
   * and the distinction is kept for the next agent added before its skill.
   */
  skills?: readonly AgentSkill[]
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
    does: 'Looks at your profile, what you have connected, and what has changed, and raises what is worth your attention.',
    status: 'working',
    runs: 'When a sweep is triggered for your organisation, and once a day for every organisation.',
    effects:
      'Raises signals, and that is the only thing it can write. It cannot write a finding, change a record or send anything.',
    // `working` AS OF ENT-258 PR 3, AND WHAT CHANGED (ENT-232 asks the status
    // to say so).
    //
    // Two things run now where one ran before. The three fixed detectors are
    // unchanged and still run first; the agent runs after them, is shown what
    // they raised, and adds what no fixed rule was written to look for.
    //
    // The evidence for saying "Working" rather than "Working, in part" is
    // `scripts/watcher-comparison.py`, which runs in CI against a real model on
    // a real stack every time this repository is changed. It does not assert
    // that the agent covers the detectors, because it is told not to repeat
    // them; it asserts that their signals survive it untouched, that nothing it
    // writes is outside the vocabulary or cites an obligation it was not
    // offered, and that no finding is written.
    //
    // WHAT IS STILL MISSING IS THE READ PATH, WHICH IS THE ANALYST'S PROBLEM
    // TOO. Every run leaves an `agent_runs` record and the console cannot yet
    // show you one, so `remaining` says that rather than claiming nothing is
    // left.
    remaining:
      'The console cannot yet show you the runs it has made, so you can see what it raised but not how it decided.',
    skills: [
      {
        module: 'watcher',
        name: 'watcher.sweep',
        // 1.1.0 is ENT-274: `read_evidence` joined the allow-list. 1.2.0 is
        // ENT-279: `request_fetch` joined it. Minor bumps because the skill
        // answers the same question and was given more to answer it from, and
        // `agent_runs` records which version answered, so a run from before
        // each saw less than a run after it.
        version: '1.2.0',
        // THREE TOOLS, AND THE PAGE SHOWS THEM FOR THE REASON THE LIST EXISTS.
        //
        // A reader looking at "what can this thing do to my data" gets the whole
        // answer: it can read what one of your connected tools already reported,
        // it can ask for one granted read-only tool to be fetched again, and it
        // can raise a signal. There is nothing here that writes a finding,
        // changes a record or sends anything. That is the separation the
        // product rests on, and showing the list is how a customer checks it
        // rather than taking our word for it.
        //
        // `read_evidence` reads observations a fetch already deposited.
        // `request_fetch` asks; it never dials. core-api decides whether the
        // ask stands, the fetch runs later through the same gateway a
        // scheduled fetch uses, and the Watcher is answered with an
        // acknowledgement rather than a payload (ENT-279). The agent holds no
        // credential and no way to reach one, before or after this tool.
        tools: ['raise_signal', 'read_evidence', 'request_fetch'],
      },
    ],
  },
  {
    slug: 'analyst',
    name: 'The Analyst',
    does: 'Explains why a finding applies to you, beside the article it cites, and answers what you ask about it.',
    status: 'working',
    runs: 'On the sweep, to explain a new finding, and whenever you ask it a question about one.',
    effects:
      'Records how it worked, and nothing else. What it drafts comes back for something else to store, so it cannot write a finding itself.',
    // The write path exists (`RecordAgentRun`); the read path does not, so the
    // console has nothing to ask for. Proposed in the ENT-232 PR body.
    //
    // ENT-270 narrowed this without closing it. An answer to a question carries
    // its own run back in the same response, so "how this was produced" is
    // showable for the exchange you just had. Every earlier run is still
    // unreachable, which is what this sentence is still about.
    remaining:
      'The console can show you the run behind an answer it just gave you, and cannot yet show you any run it made before that.',
    skills: [
      {
        module: 'analyst',
        name: 'analyst.narrative',
        // 2.0.0 is ENT-248: the output split, so the skill explains applicability
        // to this organisation and no longer states the law. A major bump because
        // the field a caller reads was renamed, and because a run recorded under
        // 1.0.0 was answering a materially different question.
        version: '2.0.0',
        tools: [],
      },
      {
        // ENT-270. The rail's first real conversation: one question about one
        // finding, offered that finding's obligation and nothing else, so an
        // answer citing any other article is refused even when the article
        // exists. A separate module rather than a mode of the one above, because
        // it is asked a different question and its answers are recorded under a
        // different name.
        module: 'conversation',
        name: 'analyst.answer',
        version: '1.0.0',
        tools: [],
      },
    ],
  },
  {
    slug: 'messenger',
    name: 'The Messenger',
    does: 'Tells you when something needs a decision.',
    // PARTLY WORKING, AND THE MISSING HALF IS NOT THE CONSOLE (ENT-260).
    //
    // The skill runs, under a budget, an allow-list and three critics, and
    // leaves an `agent_runs` row whether it succeeded, refused or failed. What
    // does not exist yet is the caller: core-api does not yet build the
    // context, the doorbell workflow does not yet run the draft, and the
    // delivery transaction still renders the template. So no message anybody
    // receives has been through it.
    //
    // Calling that "working" would be the exact failure this file exists
    // after, a dashboard claiming something about work nobody can look at, and
    // it would be worse here than it was for the half-built Hands: this
    // agent's whole output is copy a person is supposed to receive, so
    // "working" would be a claim about their mailbox.
    status: 'partly-working',
    runs: 'Nothing calls it yet. The message you get today is still written by a template.',
    effects:
      'It writes the words of a message and hands them over. It cannot decide that a message exists, who it goes to, or where, and it never holds a mail or chat credential of its own.',
    remaining:
      'Nothing asks it for a draft yet, so the messages you receive are still the templated ones.',
    skills: [
      {
        module: 'messenger',
        name: 'messenger.draft',
        version: '1.0.0',
        // ONE TOOL, AND ITS NAME IS THE WHOLE CLAIM (ENT-260).
        //
        // `queue_message` hands a drafted subject and body to the dispatch
        // path. There is nothing here that sends, nothing that names a
        // recipient and nothing that chooses a channel, and those are not
        // omitted from a longer list: they exist nowhere the Python service
        // can reach. It holds no SMTP client and no chat token, and the
        // grammar deliberately lets the model ASK for `send_email` so that the
        // refusal is a real event landing in `agent_runs` rather than
        // something made inexpressible and therefore invisible.
        tools: ['queue_message'],
      },
    ],
  },
  {
    slug: 'hands',
    name: 'The Hands',
    does: 'Explains what approving will do, then prepares the record.',
    // `working` AS OF ENT-278, AND THE LABEL MOVED IN THE COMMIT THAT EARNED IT.
    //
    // ENT-261 built the skill and left it on `internal:ingest`, which a browser
    // never holds, so it ran under a budget and an allow-list and left
    // `agent_runs` rows nobody could cause and nobody could read. This entry
    // said "Working, in part" for exactly that reason, and that was the honest
    // answer rather than a hedge: a dashboard claiming something about work
    // nobody can look at is the failure this whole file was written after.
    //
    // What changed is that a person can now ask it. The finding page shows what
    // approving will do, for a finding whose approval creates a record, above
    // the decision it is about, marked as the Hands' and with the run behind
    // it. `ApprovalService.ExplainApproval` is the door, on `agents:ask`.
    //
    // WHAT IS STILL MISSING IS THE READ PATH, WHICH THE WATCHER AND THE ANALYST
    // ARE MISSING TOO. A person shown an explanation is shown the id of the run
    // that produced it and has nowhere to open it, so `remaining` says that
    // rather than claiming nothing is left.
    status: 'working',
    runs: 'When you ask it, on a finding whose approval would create a record.',
    effects:
      'It prepares a record only after you approved it. The approval stays yours, and it never decides.',
    remaining:
      'The console cannot yet show you the runs it has made, so you can read what it prepared but not open the record of how.',
    skills: [
      {
        module: 'hands',
        name: 'hands.prepare',
        version: '1.0.0',
        // ONE TOOL, AND IT IS NOT THE ONE THAT APPROVES (ENT-261).
        //
        // The whole claim this agent makes is "never decides", so the list a
        // customer reads here is where they check it. `prepare_record` fills
        // register columns from facts the organisation already recorded.
        // Approving is `findings:act`, which only a person's token carries.
        //
        // The grammar deliberately lets the model ASK for `approve_finding`,
        // so the refusal is a real event that lands in `agent_runs` rather
        // than something made impossible to express and therefore invisible.
        tools: ['prepare_record'],
      },
    ],
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
