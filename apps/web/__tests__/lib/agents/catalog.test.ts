import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'

import { AGENTS, STATUS_LABEL, agentBySlug } from '@/lib/agents/catalog'

/**
 * The agent catalogue (ENT-232).
 *
 * The console names four agents. One of them exists as a skill, one runs as
 * fixed rules, and two have not been built. This suite exists so the console
 * cannot quietly stop saying which is which.
 *
 * WHY A TEST READS PYTHON FROM A TYPESCRIPT SUITE
 *
 * The skill is declared once, in `apps/intelligence`, and the console repeats
 * its name, version and tool list to a customer. There is no RPC that would
 * let the console ask (see the PR body: `agent_runs` has a write path and no
 * read path), so the repetition is unavoidable and the drift is what has to be
 * caught instead.
 *
 * Bump `VERSION` in `analyst.py` and this suite goes red. That is the whole
 * point: `agent_runs` records which version answered, so a console showing a
 * different one is telling a customer the wrong thing about a record they may
 * be about to check.
 *
 * The two-way module check matters more than it looks. It fails if the console
 * claims a skill that does not exist, which is the failure this issue was most
 * at risk of, AND it fails if somebody adds `messenger.py` without telling the
 * console, which is the failure the quarter after.
 */

const SKILLS_DIR = path.resolve(
  __dirname,
  '../../../../intelligence/src/kindlast_intelligence/skills',
)

/** The four, in the order work flows through them. */
const PIPELINE_ORDER = ['watcher', 'analyst', 'messenger', 'hands']

const EM_DASH = '—'
const EN_DASH = '–'

/** `NAME = "analyst.narrative"` and friends, read out of the module. */
function pythonString(source: string, name: string): string {
  const match = source.match(new RegExp(`^${name} = "([^"]*)"`, 'm'))
  if (!match) throw new Error(`no ${name} in the skill module`)
  return match[1]
}

/**
 * `ALLOWED_TOOLS: tuple[str, ...] = ()`, or the same with entries in it.
 *
 * Parsed rather than imported because running Python from Vitest would need a
 * toolchain in the Node suite, and this suite's promise is that it needs no
 * services. A regex is enough for a declaration whose whole purpose is to be
 * read at a glance.
 */
function pythonTuple(source: string, name: string): string[] {
  const match = source.match(new RegExp(`^${name}[^=]*= \\(([^)]*)\\)`, 'm'))
  if (!match) throw new Error(`no ${name} in the skill module`)
  return [...match[1].matchAll(/"([^"]*)"/g)].map((m) => m[1])
}

describe('the agent catalogue (ENT-232)', () => {
  it('names the four agents in the order work flows through them', () => {
    // Order is meaning here, not layout. The rail draws them as a pipeline, so
    // a catalogue in a different order would draw the Messenger before the
    // Analyst and describe a product that does not exist.
    expect(AGENTS.map((a) => a.slug)).toEqual(PIPELINE_ORDER)
  })

  it('gives every agent a status the console has words for', () => {
    for (const agent of AGENTS) {
      expect(STATUS_LABEL[agent.status]).toBeTruthy()
    }
  })

  // The honesty assertion, and the reason this file exists. ENT-161 happened
  // because a dashboard said everything was fine about work nothing had done.
  // Exactly one of the four is a working skill today; two are not built at all.
  it('claims two working agents, not four', () => {
    // The Watcher joined the Analyst in ENT-258, and the count is asserted
    // rather than the absence of a count for the reason this test was written
    // for: the page used to make one claim about all four, and the failure
    // that hides is a placeholder reading like a feature. A third agent
    // becoming "working" should have to change this line and say why in the
    // commit that does.
    expect(
      AGENTS.filter((a) => a.status === 'working').map((a) => a.slug),
    ).toEqual(['watcher', 'analyst'])
    expect(
      AGENTS.filter((a) => a.status === 'not-built').map((a) => a.slug),
    ).toEqual(['messenger', 'hands'])
    // Nothing is partly working now. The state still exists and is still
    // rendered, because it is the honest answer for the next agent that gets
    // half built, and an unused state is cheaper than the pressure to call
    // something finished for want of a label.
    expect(AGENTS.filter((a) => a.status === 'partly-working')).toHaveLength(0)
  })

  it('says what remains for every agent that is not finished', () => {
    for (const agent of AGENTS) {
      if (agent.status === 'working') continue
      // An unfinished agent with nothing written about what is missing is how
      // a placeholder starts reading like a feature.
      expect(agent.remaining, `${agent.slug} says what remains`).toBeTruthy()
    }
  })

  it('resolves an agent by its exact slug, and nothing else', () => {
    expect(agentBySlug('analyst')?.name).toBe('The Analyst')
    // No normalisation and no nearest match, for the reason the citation
    // validator gives: helping turns "this does not exist" into "this nearly
    // exists", and the page behind a wrong slug should be a 404.
    expect(agentBySlug('Analyst')).toBeUndefined()
    expect(agentBySlug('the-analyst')).toBeUndefined()
    expect(agentBySlug('')).toBeUndefined()
  })

  it('writes no em dashes or en dashes in anything a person reads', () => {
    for (const agent of AGENTS) {
      for (const [field, value] of Object.entries(agent)) {
        if (typeof value !== 'string') continue
        expect(value, `${agent.slug}.${field}`).not.toContain(EM_DASH)
        expect(value, `${agent.slug}.${field}`).not.toContain(EN_DASH)
      }
    }
    for (const label of Object.values(STATUS_LABEL)) {
      expect(label).not.toContain(EM_DASH)
      expect(label).not.toContain(EN_DASH)
    }
  })
})

describe('what the console claims about skills (ENT-232)', () => {
  it('claims a skill for exactly the skills that exist', () => {
    const onDisk = readdirSync(SKILLS_DIR)
      .filter((f) => f.endsWith('.py') && f !== '__init__.py')
      .map((f) => f.replace(/\.py$/, ''))
      .sort()

    const claimed = AGENTS.flatMap((a) =>
      (a.skills ?? []).map((s) => s.module),
    ).sort()

    expect(claimed).toEqual(onDisk)
  })

  // ONE CASE PER SKILL, GENERATED FROM THE CATALOGUE ITSELF (ENT-270).
  //
  // It used to be two hand-written cases, one per skill, which is a shape that
  // silently stops covering the thing it is for: adding `conversation.py` and
  // its catalogue entry would have left the new skill's version unguarded while
  // the suite stayed green, and the version is the field that matters most
  // here. `agent_runs` records which version answered, so a console showing a
  // different one is telling a customer the wrong thing about a record they may
  // be about to check.
  //
  // The list this walks is proved by the two-way module check above, which is
  // the question AGENTS.md says to ask whenever a guard reads from a list.
  for (const agent of AGENTS) {
    for (const skill of agent.skills ?? []) {
      it(`repeats ${skill.name} exactly as the skill declares it`, () => {
        const source = readFileSync(
          path.join(SKILLS_DIR, `${skill.module}.py`),
          'utf8',
        )

        expect(skill.name).toBe(pythonString(source, 'NAME'))
        expect(skill.version).toBe(pythonString(source, 'VERSION'))
        // The allow-list is what a customer reads to decide what an agent can
        // do to their data. A page claiming a narrower list than the skill
        // actually holds would be the worst kind of wrong.
        expect(skill.tools).toEqual(pythonTuple(source, 'ALLOWED_TOOLS'))
      })
    }
  }

  it('gives the Analyst both of its skills', () => {
    // Named rather than counted, because the interesting failure is one of the
    // two quietly disappearing: a count would pass if somebody replaced the
    // narrative skill with the answer skill, which is exactly the drift the
    // module check cannot see once both modules exist on disk.
    expect(agentBySlug('analyst')?.skills?.map((s) => s.name)).toEqual([
      'analyst.narrative',
      'analyst.answer',
    ])
  })

  it('leaves the skill list absent for an agent with none', () => {
    // Absent rather than empty. An empty TOOL list is a statement the Analyst
    // makes on purpose (it is given its inputs and then answers); an empty
    // SKILL list for the Messenger would claim a guardrail with nothing behind
    // it, which is the opposite claim.
    for (const agent of AGENTS) {
      if (agent.skills) continue
      expect(agent.skills, `${agent.slug} claims no skill`).toBeUndefined()
    }
  })
})
