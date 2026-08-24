import type { Agent, AgentSkill } from '@/lib/agents/catalog'
import { STATUS_LABEL } from '@/lib/agents/catalog'
import { AgentStatusDot } from '@/components/agents/agent-status'

/**
 * One agent, answering the two questions somebody clicking its name has
 * (ENT-232).
 *
 * Is it running, and what is it allowed to touch. Everything here is one of
 * those two, and nothing here is a description of how good it is.
 *
 * WHY THE TOOL LIST IS RENDERED EVEN WHEN IT IS EMPTY
 *
 * The Analyst declares `ALLOWED_TOOLS = ()`, and that is a decision rather than
 * an oversight: it is handed its inputs and then answers, so there is nothing
 * for it to go and fetch mid-run, and a request for any tool is refused rather
 * than retried. Rendering nothing where the list would be would read as "we did
 * not look", which is a weaker claim than the true one.
 *
 * An agent with no skill gets no list at all, which is a different thing again.
 * The Messenger's allow-list is not empty, it is absent, and drawing an empty
 * one would advertise a guardrail with nothing behind it.
 *
 * WHY THERE CAN BE MORE THAN ONE SKILL
 *
 * The Analyst has two since ENT-270: one narrates a finding on a job, the other
 * answers a question about one in the console. They carry different names and
 * different versions and `agent_runs` records which of them answered, so
 * showing only the first would put a version on this page that no run of the
 * second ever recorded.
 *
 * A plain synchronous component taking the agent as a prop. The page above it
 * resolves a session and an organisation, and folding this into it would mean
 * rendering React to test a tenancy decision.
 */
export function AgentProfile({ agent }: { agent: Agent }) {
  return (
    <article className="space-y-8">
      <header>
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          <AgentStatusDot status={agent.status} />
          {STATUS_LABEL[agent.status]}
        </p>
        <h1 className="mt-2 text-2xl font-semibold tracking-[-0.02em] text-foreground">
          {agent.name}
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">{agent.does}</p>
      </header>

      <Fact label="When it runs">{agent.runs}</Fact>
      <Fact label="What it can change">{agent.effects}</Fact>

      {agent.skills?.length ? (
        <section>
          <SectionHeading>
            {agent.skills.length === 1
              ? 'The skill it runs'
              : 'The skills it runs'}
          </SectionHeading>
          <div className="space-y-6">
            {agent.skills.map((skill) => (
              <SkillFacts key={skill.module} skill={skill} />
            ))}
          </div>
        </section>
      ) : null}

      {agent.remaining ? (
        <section>
          <SectionHeading>What is missing</SectionHeading>
          <p className="mt-3 text-sm text-muted-foreground">
            {agent.remaining}
          </p>
        </section>
      ) : null}
    </article>
  )
}

/** One skill: what it is called, what version, and what it may call. */
function SkillFacts({ skill }: { skill: AgentSkill }) {
  return (
    <div>
      {/* Name and version together, because that pair is what a run records. A
          version on its own says nothing and a name on its own is not something
          anybody could reproduce a run from. */}
      <p className="mt-3 font-mono text-[13px] text-foreground">{skill.name}</p>
      <p className="mt-1 text-xs text-muted-foreground">
        Version {skill.version}. Every run records both, so a finding can be
        traced back to what was asked and how.
      </p>

      <div
        data-testid="tool-allow-list"
        className="mt-4 rounded-lg border border-border/60 bg-muted/40 p-4"
      >
        <p className="text-[13px] font-medium text-foreground">
          What it may call
        </p>
        {skill.tools.length === 0 ? (
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            No tools. It is given everything it needs before it starts, then
            answers. Anything else it asked for would be refused and written
            into the record of the run.
          </p>
        ) : (
          <ul className="mt-2 space-y-1">
            {skill.tools.map((tool) => (
              <li
                key={tool}
                className="font-mono text-xs text-muted-foreground"
              >
                {tool}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function SectionHeading({ children }: { children: React.ReactNode }) {
  return (
    <h2 className="text-xs font-medium tracking-[0.08em] text-muted-foreground uppercase">
      {children}
    </h2>
  )
}

function Fact({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <section>
      <SectionHeading>{label}</SectionHeading>
      <p className="mt-3 text-sm text-muted-foreground">{children}</p>
    </section>
  )
}
