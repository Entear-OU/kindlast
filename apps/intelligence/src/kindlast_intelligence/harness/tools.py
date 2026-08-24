"""Tool dispatch: what a skill may call, and what happens when it asks for more.

# INPUTS ARE NOT TOOLS (§26.2)

A skill declares both and they live in different places. Inputs are what it
needs before it starts, fetched by the caller and passed in, because there is
no decision in fetching them. Tools are what the MODEL decides to call during
the loop, which is the thing that makes a skill agentic and the thing that
needs a guardrail.

The Analyst v0 has inputs and no tools, so nothing in this file runs for it.
That is deliberate rather than a gap: the seam exists so the Watcher and the
rail do not arrive needing the loop redesigned, and it is exercised by a test
skill rather than by pretending the Analyst uses it.

# EVERY DISPATCH IS RECORDED, INCLUDING THE REFUSED ONES

A tool call the harness refused is a fact about the run, and a record showing
only the calls that succeeded would describe a better-behaved run than the one
that happened. §26 wants a record a customer can read; "it asked for something
it was not allowed" is exactly the kind of thing they would want to see.
"""

from __future__ import annotations

from typing import Callable, Protocol

from pydantic import BaseModel, ConfigDict, Field

from .budget import Budget


class ToolRefused(Exception):
    """The skill asked for a tool it may not use.

    NOT retried, and not answered with an error the model can react to.
    §26.3 is explicit: an unknown tool is refused rather than retried, because
    a model that can discover the allow-list by probing it has been handed a
    way to negotiate with its own guardrail.
    """

    def __init__(self, tool: str, allowed: tuple[str, ...]) -> None:
        super().__init__(
            f"tool {tool!r} is not in this skill's allow-list {list(allowed)}"
        )
        self.tool = tool
        self.allowed = allowed


class ToolDeclined(Exception):
    """The far side said no to THIS call, rather than to this run's authority.

    # THE SECOND KIND OF NO, ADDED WITH THE SECOND TOOL (ENT-274)

    `ToolRefused` is about what the skill may do at all: an unlisted tool, or
    one nobody wired. It ends the run, because a model that can probe the
    allow-list has been handed a way to negotiate with its own guardrail.

    This one is different in the only way that matters. The model asked for
    something that genuinely exists and that a policy declines: a customer has
    not granted that tool on that connection. Ending the run there would mean
    one wrong guess costs the customer their whole sweep, for a reason they
    would have to read a record to discover. And there is nothing to probe:
    the grants are in the context the model was already shown, so "not that
    one" is information rather than a hint towards a way in.

    So it is recorded as a refusal, because that is what it was, and the reason
    goes back to the model as the tool's result so the loop can act on it. The
    dispatcher is what performs both halves; a tool chooses which no it is
    raising by choosing which exception to raise.
    """

    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason


class ToolCall(BaseModel):
    """One invocation, as the record stores it."""

    model_config = ConfigDict(frozen=True, extra="forbid")

    tool: str
    arguments: dict[str, object] = Field(default_factory=dict)
    # A summary rather than the whole result. The record is for a person, and
    # pasting a full corpus row into every run makes the useful part
    # unfindable. It also keeps a tool response from becoming a place to smuggle
    # a large payload into a record nobody reads closely.
    result_summary: str = ""
    # Set when the call was refused. Present in the record precisely because a
    # refused call is a fact about the run.
    refused: bool = False


class Tool(Protocol):
    def __call__(self, **kwargs: object) -> str: ...


class ToolDispatcher:
    """Runs a skill's tools, under its allow-list and the run's budget."""

    def __init__(
        self,
        allowed: tuple[str, ...],
        tools: dict[str, Callable[..., str]],
        budget: Budget,
    ) -> None:
        # A tool registered but not allowed is a configuration mistake worth
        # failing on at construction rather than at the first call: it means
        # somebody wired a capability the skill was never granted, and finding
        # that out mid-run tells you much less about who did it.
        unlisted = set(tools) - set(allowed)
        if unlisted:
            raise ValueError(
                f"tools {sorted(unlisted)} are registered but not in the "
                f"skill's allow-list {list(allowed)}"
            )

        self._allowed = allowed
        self._tools = tools
        self._budget = budget
        self.calls: list[ToolCall] = []

    def dispatch(self, tool: str, /, **arguments: object) -> str:
        # POSITIONAL ONLY, AND THE SLASH IS LOAD-BEARING (ENT-274).
        #
        # A tool's own arguments arrive as keywords, and a tool whose argument
        # is named `tool` is not a strange thing to want: `read_evidence` takes
        # a connection and the tool on it to read. Without the slash that
        # collides with this method's own parameter and Python raises
        # "multiple values for argument 'tool'", which reads as a harness bug
        # rather than as a name clash and is invisible until somebody adds
        # exactly that argument.

        # Allow-list BEFORE the budget, so a forbidden tool does not consume a
        # call the skill was entitled to. Refusing and charging for it would
        # let a model exhaust a well-behaved run's budget by asking for things
        # it cannot have.
        if tool not in self._allowed:
            self.calls.append(
                ToolCall(tool=tool, arguments=arguments, refused=True,
                         result_summary="refused: not in the skill's allow-list")
            )
            raise ToolRefused(tool, self._allowed)

        self._budget.check_clock()
        self._budget.spend_tool_call()

        implementation = self._tools.get(tool)
        if implementation is None:
            # Allowed by the skill and not wired by the caller. A different
            # fault from the one above, and worth a different message: the
            # skill did nothing wrong.
            self.calls.append(
                ToolCall(tool=tool, arguments=arguments, refused=True,
                         result_summary="refused: allowed but not implemented")
            )
            raise ToolRefused(tool, self._allowed)

        # A TOOL THAT RAISES IS STILL A TOOL CALL THAT HAPPENED (ENT-277).
        #
        # This used to be a bare call followed by the append below, so anything
        # the implementation raised skipped the record entirely. The two
        # refusals above were written down and the one that actually reached
        # the outside world was not, which is precisely backwards: a call that
        # was refused is the one a customer most needs to see, and a citation
        # this run tried to fabricate or a signal core-api rejected left no
        # trace of having been attempted.
        #
        # Re-raised unchanged, because deciding what a refusal MEANS is the
        # runner's job (see `watch`) and this class only records that it
        # happened. Swallowing it here would turn a refusal into a success with
        # a sad note attached.
        try:
            result = implementation(**arguments)
        except ToolDeclined as exc:
            # RECORDED AS A REFUSAL AND NOT RE-RAISED (ENT-274). See
            # `ToolDeclined` for why this one no does not end the run. The
            # reason becomes the tool's result, so the loop feeds it back and
            # the model is told what its own customer's grant says.
            self.calls.append(
                ToolCall(
                    tool=tool,
                    arguments=arguments,
                    refused=True,
                    result_summary=_summarise(f"refused: {exc.reason}"),
                )
            )
            return f"refused: {exc.reason}"
        except Exception as exc:
            self.calls.append(
                ToolCall(
                    tool=tool,
                    arguments=arguments,
                    refused=True,
                    result_summary=_summarise(f"{type(exc).__name__}: {exc}"),
                )
            )
            raise

        self.calls.append(
            ToolCall(tool=tool, arguments=arguments, result_summary=_summarise(result))
        )
        return result


def _summarise(result: str, limit: int = 500) -> str:
    if len(result) <= limit:
        return result
    return result[:limit] + f"... ({len(result)} chars)"
