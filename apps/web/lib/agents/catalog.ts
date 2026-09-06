/**
 * The agents, as the console is allowed to describe them (ENT-232).
 *
 * §26.5 defines an agent as a skill plus a tool allow-list plus a workflow,
 * addressable by name in the rail. They arrived at different times and one is
 * still missing a half, and this file is where the console keeps track of
 * which is which.
 *
 * Five since ENT-285: Kindy, and the four it routes to. Kindy is not a fifth
 * stage of the pipeline, which is why it sits at the head of the list rather
 * than the end. It is the one a person talks to, and the four underneath it
 * still run in the order they always did.
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
 *   Kindy          runs as a skill (ENT-285): it chooses which agent answers
 *                  and about which finding. Nothing calls it yet, so the
 *                  panel still asks the Analyst directly.
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
    slug: 'kindy',
    name: 'Kindy',
    does: 'Takes what you ask, works out which finding you mean, and answers it.',
    // `partly-working` AS OF ENT-285, AND THE SKILL HALF IS THE HALF THAT
    // LANDED.
    //
    // The rail has been Kindy's card since ENT-222 and said, in its own
    // comment, that the orchestrator did not exist as a skill and that when it
    // landed its status would arrive from this catalogue. This is that entry.
    //
    // What exists is `kindy.orchestrate`: a skill with one tool, a budget
    // shared with whatever it calls, an offered subject set it may not name a
    // finding outside, and a check that refuses a tool the person asking could
    // not have used themselves. It leaves an `agent_runs` row whatever the
    // outcome.
    //
    // What does not exist is the caller. There is no RPC a browser can reach
    // that runs it, so the composer still asks the Analyst directly about the
    // finding you have open (ENT-284) and no answer anybody has had was routed
    // by Kindy. Calling that "Working" would be the exact failure this file was
    // written after, and worse here than for the Messenger or the Hands before
    // it: this is the agent a person thinks they are talking to, so the claim
    // would be about the conversation they just had.
    //
    // It is first because it is the one you talk to and it routes to the other
    // four, not because work flows out of it into them. The pipeline underneath
    // is unchanged and still runs in its own order.
    status: 'partly-working',
    runs: 'Nothing runs it yet. What you type in the panel is still answered directly.',
    effects:
      'It chooses which agent answers and which finding they answer about, and it writes nothing itself. It can only ask about findings you can already see, and it holds no tool that sends, approves or changes a record.',
    remaining:
      'The skill is built and nothing calls it, so no answer you have had came through it. It can also answer only questions so far, because a tool that sends is a different risk from a tool that reads.',
    skills: [
      {
        module: 'kindy',
        name: 'kindy.orchestrate',
        version: '1.0.0',
        // ONE TOOL, AND IT IS ANOTHER AGENT RATHER THAN AN RPC (ENT-285).
        //
        // This list is where a customer checks the claim that the thing they
        // talk to cannot act on their behalf. `ask_analyst` puts their question
        // to the Analyst about one finding, and the Analyst writes nothing
        // either. There is deliberately nothing here that sends, approves,
        // raises a signal or prepares a record: all four exist elsewhere in
        // this catalogue and none is reachable from here.
        //
        // The grammar deliberately lets the model ASK for one of them, so the
        // refusal lands in `agent_runs` as a real event rather than being made
        // inexpressible and therefore invisible.
        tools: ['ask_analyst'],
      },
    ],
  },
  {
    slug: 'watcher',
    name: 'Monitoring',
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
    name: 'Explanations',
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
    name: 'Messages',
    does: 'Tells you when something needs a decision.',
    // WORKING, IN THE COMMIT THAT MADE IT TRUE (ENT-280).
    //
    // ENT-260 shipped the skill and this entry said `partly-working`, because
    // nothing called it and "working" would have been a claim about somebody's
    // mailbox. The caller exists now: the plan carries the instruction, the
    // doorbell workflow runs the draft between plan and send, and the
    // delivery transaction renders drafted words with every link still minted
    // per recipient. A message a person receives has been through it.
    //
    // What keeps the claim honest is what it still cannot do, and the send
    // path re-checks it beside the send: a draft that fails, refuses or
    // carries a link falls back to the template, and the doorbell rings
    // either way.
    status: 'working',
    runs: 'When a finding rings a doorbell, to write the words of the message.',
    effects:
      'It writes the words of a message and hands them over. It cannot decide that a message exists, who it goes to, or where, and it never holds a mail or chat credential of its own.',
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
    name: 'Record keeping',
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
