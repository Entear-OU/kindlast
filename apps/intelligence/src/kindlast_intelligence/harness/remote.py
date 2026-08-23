"""What it means when the far side says no (ENT-277).

# WHY THIS LIVES IN THE HARNESS AND NOT BESIDE THE CLIENT THAT RAISES IT

`coreapi.py` is the outbound client. The harness is what turns everything that
can happen during a run into an `Outcome` a customer can read, and it already
owns the rest of that vocabulary: `Outcome`, `ToolRefused`, `BudgetExhausted`.

Putting this here keeps the dependency pointing one way. `coreapi` already
imports `AgentRun` and `Outcome` from the harness, so having the harness import
back from `coreapi` would have been a cycle waiting for the first line in an
`__init__.py`. The client raises the error; the harness decides what it means.
"""

from __future__ import annotations

from connectrpc.code import Code
from connectrpc.errors import ConnectError


def code_of(exc: BaseException) -> Code | None:
    """The Connect code an exception carries, if it carries one.

    Anything that is not a `ConnectError` never reached core-api's rules: a
    connection refused, a token that could not be minted, a timeout. Those have
    no code and are failures.
    """
    return exc.code if isinstance(exc, ConnectError) else None


class CoreAPIError(Exception):
    """core-api refused or could not be reached.

    # THE CODE IS KEPT, BECAUSE THE TWO HALVES OF THAT SENTENCE ARE DIFFERENT

    "Refused" and "could not be reached" are one class and must not be one
    outcome. A refusal is core-api applying a rule: the run asked for something
    it may not have, and the honest record says REFUSED. Unreachable is nobody's
    policy and the honest record says FAILED.

    Flattening both into a message string made that distinction unrecoverable,
    and the caller then did the only thing it could, which was to let the
    exception escape and take the whole RPC with it. That is how a run ended
    with no `agent_runs` row at all (ENT-277), which is the one outcome the
    harness is not allowed to produce: every run leaves a record a customer can
    read.
    """

    def __init__(self, message: str, *, code: Code | None = None) -> None:
        super().__init__(message)
        self.code = code

    @property
    def refused(self) -> bool:
        """True when core-api applied a rule, rather than failing to answer.

        `invalid_argument` is the vocabulary, the deduplication key and the
        citation being wrong: the model asked for something outside what the
        contract allows. `permission_denied` and `failed_precondition` are the
        same shape, a rule saying no to a request that was understood.

        Everything else, including no code at all, is a failure. Defaulting the
        unknown case to FAILED rather than REFUSED is deliberate: a refusal is a
        claim that the guardrails worked, and claiming that about an error
        nobody classified would make the record flattering rather than true.
        """
        return self.code in (
            Code.INVALID_ARGUMENT,
            Code.PERMISSION_DENIED,
            Code.FAILED_PRECONDITION,
        )
