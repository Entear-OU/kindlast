# Intelligence

The agent harness (ENT-218, design doc §1.6, §26). Python, because §10 puts
narrative drafting here and the language split is §1.6's rather than a
preference.

What is here today is the floor everything else stands on: token verification
and the tests that prove it refuses what it must. The agent loop, `skills/`,
the guardrail ring and `DraftNarrative` land on top of it.

## Two properties, and they are the reason this service can exist at all

**It accepts no user tokens, ever.** Not "does not expect". Cannot. Two
independent things enforce it: the audience is `kindlast-intelligence` and a
human's console token carries a different one, and `internal:intelligence` is
required and is granted only to the service principal. Either alone would do.

**It holds no database credential and opens no connection.** Go loads and Go
persists; Python drafts and returns.

The two are the same argument from different ends. Intelligence has no tenancy
GUCs and no RLS session, so it has no way to check whether a human should see
what it is processing. It never needs one, because a human cannot reach it.
Give it a connection and that stops being structural and becomes a promise.

`tests/test_no_database.py` asserts the second one, because it is exactly the
kind of property that holds until somebody adds one import to solve a real
problem quickly.

## Running it

```bash
cd apps/intelligence
uv sync
uv run pytest
```

Nothing else is needed. The token battery runs a real authorization server on
localhost and signs real tokens, so it has no dependency on the compose stack.

## The token battery

`tests/test_token_battery.py` is §13.2's battery, ported from
`libs/chassis/oidc`. Each case spoils exactly one property of a token that
would otherwise be accepted, and asserts **which** check refused it. Asserting
only "denied" would let a broken audience check hide behind a working expiry
check.

Nothing is stubbed. §13.2 is explicit that the verifier is the code most worth
testing and that substituting a mock means every test is really testing the
mock, so the fixture is an actual HTTP server serving an actual JWKS.

### One case is more interesting than the others

`test_the_allow_list_holds_only_asymmetric_algorithms` exists because of what
happened when the battery was checked against deliberate sabotage.

Adding `HS256` to `SIGNING_ALGORITHMS` left **all** the token tests green. The
public-key-as-HMAC forgery was still refused, but by a second, independent
mechanism: the verifier hands PyJWK's key *object* to the decoder, and PyJWT's
HMAC path will not take an RSA public key object as a secret. Two real
defences, which is the belt and braces the Go side describes.

The consequence is uncomfortable and worth stating plainly: the forgery test
passes whether or not the allow-list is correct, so it cannot be the thing that
protects the allow-list. A test that stays green while the property it names is
broken is precisely what `AGENTS.md` warns about. So there is now a direct
assertion on the constant, which does go red the moment somebody widens it.

Every security property here was checked the same way: broken deliberately,
watched go red, restored. The allow-list, the audience check, the scope gate
and the no-database rule.

## The eval gate

```bash
uv run python -m kindlast_intelligence.evals.gate
```

Runs the golden set in `evals/golden/` through the real `draft_narrative` and
fails on two different things: a guardrail that stopped firing, and an aggregate
that moved against `evals/baseline.json`. It runs in the `intelligence` CI job,
needs no model, and takes about a second.

**It measures the harness, not the model.** ENT-229 named DeepEval and Ragas and
both install cleanly, so not using them is a choice. Measured on 2026-08-18 with
`uv add` in a throwaway project: `deepeval==4.1.8` resolves 69 packages into an
88 MB environment and reports telemetry to `us.i.posthog.com` unless
`DEEPEVAL_TELEMETRY_OPT_OUT` is set; `ragas==0.4.3` resolves into 564 MB
carrying langchain, langgraph, langsmith, pandas, pyarrow, scipy and tiktoken.

Size is the smaller objection. Their headline metrics are LLM-as-judge:
`ragas.metrics.Faithfulness` is a `MetricWithLLM` and asserts an llm is set
before it will score. That is the right instrument for "is this RAG answer
good", which is a question about the model. This gate asks whether the harness
did its job, and that question has exact answers: a slug either was among the
obligations the run was offered or it was not. Putting a judge model in front of
a set-membership question buys nondeterminism and a network dependency in a gate
whose whole purpose is to be trustworthy when the first model is not.

### The weak-versus-strong number

Each case carries a `weak` and a `strong` recorded response, and the gate reports
two rates per tier: what a naive implementation would have stored, and what the
ring actually let through. The weak tier fabricates and the strong tier does
not, so the first gap is wide; the second is zero, because nothing gets through
either way. The difference between them is how much of the tier gap the harness
closes, and it is the headline number.

The utility delta beside it is what the harness does **not** close. It makes a
weak model safe, not good, and it pays for that in refusals, so the weak tier
produces fewer usable answers. A change that raises the strong tier's usable rate
without raising the weak tier's widens that gap and turns the gate red, which is
what ENT-229 means by a change that only helps strong models being visible.

Refresh the recorded responses against a real model with:

```bash
uv run python -m kindlast_intelligence.evals.record --tier weak
```

It captures responses and deliberately does not touch the expectations. A
recorder that rewrote the expected outcome to match what just happened would
turn every regression into a fixture update.

## House style is enforced, not requested (ENT-163)

`AGENTS.md` forbids em dashes and en dashes in anything a customer reads.
ENT-160 added a line to the Analyst's system prompt asking the model not to use
them, and narratives kept arriving with them, which is the entire lesson: a
prompt is a request, and `AGENTS.md` is explicit that the model may ask while
only code refuses.

So `harness/prose.py` scans the drafted narrative for U+2014 and U+2013 and
refuses the run when it finds one, naming the character, its position and the
words around it so the record says something a person can act on. The prompt
still asks, because a model that complies costs nothing to run; the difference
is that compliance is now checked rather than hoped for.

Two deliberate limits. The hyphen-minus is not touched, since the rule allows
`plain-language` and `2-4 hours`, and a critic that refused those would refuse
most correct narratives and would be switched off within a week. And it refuses
rather than rewriting: a substituted character would be an edit to a claim about
the law made by no author, and it would make a weak model score in the golden set
as a compliant one.

## Throughput is a guardrail too

The ring counts tokens, model calls, tool calls and recursion, which are cost
controls and are the right ones when inference is a hosted API. ENT-235 made it
local, so the scarce thing is a slot on one `llama-server` and a run can satisfy
every cost limit while holding that slot for eleven minutes.

So `Budget` carries two clocks. `max_queue_seconds` is how long an answer is
still worth having, checked once at admission and before the model is called.
`max_seconds` is how long the work itself may take, and its clock starts when the
work does. Conflating them is the tempting mistake: one clock started at enqueue
lets the queue spend the model's budget, and one started at dispatch cannot
explain why the customer waited.

`FairQueue` is the bounded queue in front of it, rotating between organisations
so one tenant's sweep cannot take every slot. It is not a security boundary (RLS
is, and this cannot see a row), but on a shared deployment starving another
tenant's console request is a tenancy problem even though no data crosses.

## What it does not have yet

The queue is a data structure with no worker loop around it, and nothing wires it
into the service yet: `DraftNarrative` still runs its work inline, so the
admission limits protect a caller that passes `queued_at` and nobody else. The
`llama-server` replicas and the slot-aware balancer ENT-238 asks for are a
`deploy/` change, and the sizing guidance it wants belongs in `docs/`. Neither is
here.

The wall-clock limit stops further work rather than interrupting a call in
flight, which is the honest limit of enforcing it in-process.
