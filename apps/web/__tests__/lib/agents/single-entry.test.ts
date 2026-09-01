import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import path from 'node:path'

/**
 * Kindy is the only way to ask an agent anything (ENT-286).
 *
 * # THE PROPERTY
 *
 * Every conversation with an agent enters through Kindy. A console surface may
 * render an agent's answer anywhere the answer belongs, and the Hands' plan
 * belongs directly above the approve button it describes. What it may not do is
 * ASK. One entry point means one place where a subject is anchored (ENT-284),
 * one place where the routing decision is recorded, and one run record per
 * question rather than three shapes of run reachable from three surfaces.
 *
 * REMOVING THE ASK IS NOT REMOVING THE ANSWER, and this file is deliberately
 * blind to where an answer is drawn. `__tests__/lib/findings/registers.test.ts`
 * is what holds the Hands' explanation above the decision panel, and it must go
 * on holding it after ENT-286. A guard that conflated the two would let
 * somebody satisfy this one by deleting the panel.
 *
 * # THIS PASSES TODAY, AND THAT IS THE POINT
 *
 * ENT-286 has not landed. The finding page still asks both agents directly, and
 * those two calls are named in `ALLOWED` below with the issue that removes
 * them. What this catches TODAY is a THIRD direct caller: a new surface that
 * reaches an agent without going through Kindy is red the moment it is written,
 * rather than one more thing for ENT-286 to find.
 *
 * # HOW TO TIGHTEN IT WHEN ENT-286 LANDS
 *
 * Delete the `feed/[id]/actions.ts` entry from `ALLOWED`. You will not have to
 * remember: `every allowed caller is still a caller` fails on an entry that has
 * stopped calling, so an allow-list that outlives the caller it excused is red.
 * That is the property that makes this file self-tightening rather than a note
 * somebody has to act on.
 *
 * # WHAT PROVES THE LIST, WHICH IS TWO SEPARATE LISTS HERE
 *
 * `AGENTS.md`: a test that walks a list proves the members, not the list. This
 * guard reads two lists and neither is written down here.
 *
 * The first is WHICH RPCs COUNT AS ASKING AN AGENT. Hard-coding
 * `AskAboutFinding` and `ExplainApproval` would go blind the day a third one is
 * added, which is exactly what ENT-285 does. So the list comes off the contract
 * instead: every RPC in `proto/` declaring `required_scope = "agents:ask"`. That
 * scope is the authority boundary the interceptor enforces, so a new way to
 * spend a model budget on a customer's words has to declare it, and declaring it
 * enrols the RPC in this guard automatically.
 *
 * The second is WHICH FUNCTIONS REACH THOSE RPCs. Also derived: a transport
 * module is any source file carrying one of those Connect procedure paths as a
 * literal, and its exported functions are the reachable ones. So a wrapper added
 * in a new file is found rather than assumed, and a surface calling
 * `lib/core-api/call` directly with the procedure path becomes a transport
 * module and fails the single-transport assertion.
 *
 * Underneath both is a third list, the file walk. A walk that silently found
 * nothing is the failure mode that would make this whole file a green tick over
 * an empty set, so the walk is asserted non-empty, asserted to be above a floor,
 * and asserted to contain sentinel files that must exist for the console to
 * exist at all.
 *
 * # WHAT ENT-285 WILL DO TO THIS FILE
 *
 * ENT-285 makes Kindy an orchestrator and gives the console an entry RPC of its
 * own. If that RPC declares `agents:ask`, which it should for the reason
 * `conversation.proto` gives, this guard picks it up from the proto and its web
 * wrapper's caller is a caller with no allow-list entry, so ENT-285 goes red
 * here until it adds one. That is the guard working: a new entry point is
 * supposed to be a deliberate line in this file rather than a quiet addition.
 * Add the Kindy path, not a blanket exemption.
 *
 * # PROVEN ABLE TO FAIL
 *
 * Per `AGENTS.md`, a security-shaped test is not trusted until it has been
 * watched going red, and both halves of this one were.
 *
 * A fake direct caller was added at `components/agents/fake-direct-ask.tsx`
 * importing `askAboutFinding`, and `a console surface only reaches an agent
 * through Kindy` failed naming that file. That is the guard catching a new way
 * to ask.
 *
 * Then a bogus entry for `components/console/sidebar.tsx` was added to
 * `ALLOWED`, and `every allowed caller is still a caller` failed naming it.
 * That is the half that makes the allow-list shrink on its own, and it is the
 * one that would have been easy to write in a form that could never fail.
 *
 * Removing each restored green. Do the same to any change you make here.
 */

/** `apps/web`. Three up from `__tests__/lib/agents`. */
const WEB = path.resolve(__dirname, '../../..')

/** The contract, two further up. `apps/web` to `apps` to the repository root. */
const PROTO = path.resolve(WEB, '../../proto')

/**
 * The console's own source, and nothing else.
 *
 * Tests are deliberately outside it. A component test that renders a panel with
 * a stub action is not a surface asking an agent anything, and including
 * `__tests__` would make this guard fire on the suites that prove the panels
 * draw what they are handed.
 */
const SOURCE_ROOTS = ['app', 'components', 'lib']

/**
 * Files that may call an agent, and why each one is excused.
 *
 * Paths are relative to `apps/web` and POSIX-spelled, because they are read by
 * a person deciding whether a new entry belongs rather than by a matcher.
 */
const ALLOWED: Record<string, string> = {
  // THE KINDY PATH. This is the entry point the whole issue is about, and it is
  // the one that stays. It re-resolves the organisation from the slug, picks
  // the subject, and records the exchange.
  'app/(authed)/o/[org]/kindy-actions.ts':
    'Kindy, which is the entry point every other surface loses',

  // THE TWO ENT-286 REMOVES. Both were placed deliberately and the reasoning is
  // in `components/agents/ask-analyst.tsx` and `explain-approval.tsx` rather
  // than in a ticket: the Analyst is safe because a finding anchors its
  // citation check, and the Hands' output belongs beside the button it
  // describes. Neither reason is an argument for a second way to ASK, and both
  // survive Kindy asking on the surface's behalf. DELETE THIS ENTRY WHEN
  // ENT-286 LANDS; the assertion below will insist.
  'app/(authed)/o/[org]/(needs-profile)/feed/[id]/actions.ts':
    'the finding page asks the Analyst and the Hands directly, until ENT-286',
}

/**
 * Enough files that a walk which found almost nothing is a failure.
 *
 * A floor rather than an exact count, because the console grows every week and
 * a test that had to be edited by anyone adding a page would be edited without
 * being read.
 */
const AT_LEAST_THIS_MANY_FILES = 100

/**
 * Files the console cannot lose without this guard needing rewriting anyway.
 *
 * Both survive ENT-286. `ask-analyst.tsx` would be the obvious sentinel and is
 * deliberately not one: it is what the issue deletes, and a sentinel that fails
 * on the change it was written for is a sentinel nobody keeps.
 */
const SENTINELS = [
  'app/(authed)/o/[org]/(needs-profile)/feed/[id]/page.tsx',
  'components/console/kindy-composer.tsx',
]

/** Every `.ts` and `.tsx` under the console's source roots, POSIX-relative to `apps/web`. */
function sourceFiles(): string[] {
  const found: string[] = []

  const walk = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.name === 'node_modules' || entry.name.startsWith('.')) continue
      const full = path.join(dir, entry.name)
      if (entry.isDirectory()) {
        walk(full)
        continue
      }
      if (/\.tsx?$/.test(entry.name)) {
        found.push(path.relative(WEB, full).split(path.sep).join('/'))
      }
    }
  }

  for (const root of SOURCE_ROOTS) walk(path.join(WEB, root))
  return found.sort()
}

/** Every `.proto` under `proto/`. */
function protoFiles(dir: string): string[] {
  const found: string[] = []
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) found.push(...protoFiles(full))
    else if (entry.name.endsWith('.proto')) found.push(full)
  }
  return found
}

/**
 * Every RPC declaring `required_scope = "agents:ask"`, as the Connect procedure
 * path a caller sends.
 *
 * Parsed rather than generated, for the reason the catalogue suite reads Python:
 * running `buf` from the Node suite would give this suite a toolchain, and its
 * promise is that it needs no services. The parse is bounded to a `service`
 * block so an option mentioned in a comment beside a message cannot enrol a
 * method that never declared it.
 */
function askProcedures(): string[] {
  const procedures: string[] = []

  for (const file of protoFiles(PROTO)) {
    const source = readFileSync(file, 'utf8')
    const pkg = source.match(/^package\s+([\w.]+);/m)?.[1]
    if (!pkg) continue

    // Each `service Foo {` down to the `}` in column zero that closes it.
    for (const start of source.matchAll(/^service\s+(\w+)\s*\{/gm)) {
      const from = (start.index ?? 0) + start[0].length
      const end = source.indexOf('\n}', from)
      const block = source.slice(from, end === -1 ? source.length : end)

      // Everything from one `rpc` to the next belongs to that rpc, whatever it
      // is indented by, so the options following a declaration are read against
      // the declaration they follow.
      for (const chunk of block.split(/\brpc\s+/).slice(1)) {
        const method = chunk.match(/^(\w+)/)?.[1]
        if (!method) continue
        if (!/required_scope\)\s*=\s*"agents:ask"/.test(chunk)) continue
        procedures.push(`${pkg}.${start[1]}/${method}`)
      }
    }
  }

  return procedures.sort()
}

const FILES = sourceFiles()
const PROCEDURES = askProcedures()

/** Files carrying an ask procedure path as a literal: the wire, wherever it is. */
const TRANSPORT = FILES.filter((file) => {
  const source = readFileSync(path.join(WEB, file), 'utf8')
  return PROCEDURES.some((procedure) => source.includes(procedure))
})

/**
 * The exported functions of those files, which is what a surface would call.
 *
 * Every export of a transport module counts, not only the one whose body holds
 * the literal. That over-collects if such a module ever exports something
 * unrelated, and over-collecting makes this guard stricter rather than blinder,
 * which is the direction to err in. Keep a transport module to its wrappers and
 * the question does not arise.
 */
const REACHABLE = TRANSPORT.flatMap((file) =>
  [
    ...readFileSync(path.join(WEB, file), 'utf8').matchAll(
      /^export\s+(?:async\s+)?function\s+(\w+)/gm,
    ),
  ].map((match) => match[1]),
).sort()

/** Files naming one of those functions, other than the module that defines it. */
const CALLERS = FILES.filter((file) => {
  if (TRANSPORT.includes(file)) return false
  const source = readFileSync(path.join(WEB, file), 'utf8')
  return REACHABLE.some((name) => new RegExp(`\\b${name}\\b`).test(source))
}).sort()

describe('what counts as asking an agent (ENT-286)', () => {
  it('reads the ask RPCs off the contract rather than a list here', () => {
    // The non-vacuity assertion, and the one that matters most in this file. A
    // regex that stopped matching would leave `PROCEDURES` empty, every
    // assertion below would pass over nothing, and the guard would report a
    // property it was no longer checking.
    expect(PROCEDURES.length).toBeGreaterThan(0)

    // The two that exist today, named so a parser matching something else
    // entirely is caught. This is not the list under test: it is proof that the
    // derivation of the list works.
    expect(PROCEDURES).toContain(
      'kindlast.core.v1.ConversationService/AskAboutFinding',
    )
    expect(PROCEDURES).toContain(
      'kindlast.core.v1.ApprovalService/ExplainApproval',
    )
  })

  it('finds the wrappers rather than assuming where they live', () => {
    expect(TRANSPORT.length).toBeGreaterThan(0)
    expect(REACHABLE.length).toBeGreaterThan(0)

    // Every ask RPC is spoken from exactly one module. Two would mean a surface
    // had gone around the wrapper and called the transport directly, which is
    // the bypass that would make the caller list below meaningless.
    for (const procedure of PROCEDURES) {
      const speaks = TRANSPORT.filter((file) =>
        readFileSync(path.join(WEB, file), 'utf8').includes(procedure),
      )
      expect(
        speaks,
        `${procedure} should be sent from exactly one module`,
      ).toHaveLength(1)
    }
  })

  it('walks the console rather than a list of files somebody maintains', () => {
    // If this walk breaks, nothing else in this file means anything.
    expect(FILES.length).toBeGreaterThan(AT_LEAST_THIS_MANY_FILES)
    for (const sentinel of SENTINELS) expect(FILES).toContain(sentinel)
  })
})

describe('Kindy is the only way to ask (ENT-286)', () => {
  it('a console surface only reaches an agent through Kindy', () => {
    const direct = CALLERS.filter((file) => !(file in ALLOWED))

    // The whole point. A new surface that asks an agent directly names itself
    // here, in the commit that writes it, rather than being found later by
    // somebody auditing the console.
    expect(
      direct,
      'these files ask an agent without going through Kindy; route the ask through the composer, or add the file to ALLOWED with the reason',
    ).toEqual([])
  })

  it('every allowed caller is still a caller', () => {
    // What makes this file tighten itself. When ENT-286 removes the finding
    // page's own asks, its entry stops being a caller and this fails until the
    // entry is deleted, so the allow-list cannot quietly outlive the exception
    // it was written for.
    const stale = Object.keys(ALLOWED).filter((file) => !CALLERS.includes(file))

    expect(
      stale,
      'these files no longer ask an agent; delete their ALLOWED entries',
    ).toEqual([])
  })

  it('the Kindy path is one of them', () => {
    // Asserted separately from the allow-list because an empty console would
    // satisfy both assertions above. Kindy must actually reach an agent, or
    // "the only way to ask" is a sentence about a product where nobody can ask
    // anything.
    expect(CALLERS).toContain('app/(authed)/o/[org]/kindy-actions.ts')
  })
})
