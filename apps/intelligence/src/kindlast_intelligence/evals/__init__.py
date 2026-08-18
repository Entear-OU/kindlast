"""Evals as a CI gate, measuring the harness rather than the model (ENT-229).

# WHY THIS IS NOT DEEPEVAL OR RAGAS

ENT-229 names both, and both install cleanly, so this is a choice rather than a
limitation. Measured on 2026-08-18 with `uv add` in a throwaway project:
`deepeval==4.1.8` resolves 69 packages into an 88 MB environment and ships
telemetry to `us.i.posthog.com` unless `DEEPEVAL_TELEMETRY_OPT_OUT` is set.
`ragas==0.4.3` resolves into a 564 MB environment carrying langchain-core,
langgraph, langsmith, pandas, pyarrow, scipy, scikit-network, tiktoken and
sqlalchemy.

Size is the smaller objection. The real one is what their metrics measure. Both
libraries' headline metrics (faithfulness, answer relevancy, context precision,
factual correctness) are LLM-as-judge: `ragas.metrics.Faithfulness` is a
`MetricWithLLM` and asserts an llm is set before it will score. That is the
right instrument for "is this RAG answer good", which is a question about the
MODEL. This gate asks whether the HARNESS did its job, and that question has
exact answers: a slug either was among the obligations the run was offered or it
was not, and a run either refused or it did not.

Putting a judge model in front of a set-membership question buys nondeterminism,
a network dependency, and a second model's opinion, in a gate whose entire
purpose is to be trustworthy when the first model is not. It would also make the
gate flaky by design, and a flaky gate is turned off. `scripts/model-smoke.py`
already states the same principle for the same reason: a check that asserts the
right article is testing the model's knowledge and goes red for a reason that is
somebody else's job to catch.

So this suite adds no dependencies at all. It replays recorded model responses
through the real `draft_narrative`, and asserts what the ring did with them.

# WHAT IT MEASURES

Two rates per tier, and the gap between them is the point.

  unguarded  what a naive implementation would have stored: a fabricated
             citation, a truncated claim, prose that never parsed.
  guarded    what the harness actually let through. Zero is the only
             acceptable value.

The weak-versus-strong delta ENT-229 asks for falls straight out of that. The
weak tier fabricates and the strong tier mostly does not, so the unguarded delta
is large. Guarded, both are at zero, so the guarded delta is nothing, and the
difference between the two is how much of the tier gap the harness closes.

The number that keeps this honest is the utility delta: the harness makes a weak
model safe, not good, and it buys that by refusing more of the weak tier's
answers. Reporting only the safety number would make the harness look free. A
change that raises the strong tier's usable rate without raising the weak tier's
widens this gap, and the baseline turns that into a red gate, which is exactly
what "a change that only helps strong models is visible" means.
"""
