"""What a skill is, so the harness can run more than one (§26.2, ENT-258).

# WHY THIS ARRIVES WITH THE SECOND SKILL AND NOT THE FIRST

`run.py` imports `..skills.analyst` by name. That was right while there was one
skill: a protocol with a single implementation is a description of that
implementation wearing a hat, and it would have been written from imagination
rather than from two cases.

There are two now, and they differ in the way that matters: the Analyst is
given everything and answers in one call, and the Watcher decides across
several with a tool between them. What they share is what is written down
here, and it is deliberately small.

# WHAT IS IN THE PROTOCOL, AND WHAT IS NOT

In: identity (`NAME`, `VERSION`), the allow-list, the grammar, and how to build
the opening messages. Those are the four things every runner needs from a skill
without knowing which skill it has.

Not in: the output type, the critics, whether there is a loop at all. Those
differ per skill and pretending otherwise would produce a protocol that every
implementation satisfies by widening a type to `Any`, which is a protocol that
proves nothing. `run.draft_narrative` and `watch.watch` each know their own
skill's output; what they share is the plumbing around it.

# THE VERSION IS PART OF THE CONTRACT, NOT DECORATION

`agent_runs` records which version answered, and a run is only reproducible if
that means something. Bump it when the prompt, the schema OR the tool list
changes, because all three change what the model was asked and therefore what
its answer means.
"""

from __future__ import annotations

from typing import Any, Protocol, runtime_checkable


@runtime_checkable
class Skill(Protocol):
    """A skill module, as the harness sees it.

    Satisfied by a MODULE rather than by a class, which is why the attributes
    are upper case: a skill is a bundle of declarations that ships read-only in
    the image (§26), and giving it a constructor would invite per-run state
    into the one thing that must be identical between runs to be reproducible.
    """

    NAME: str
    VERSION: str
    # What the model may call during the loop. Empty is a statement, not a
    # placeholder: it says this skill is given everything it needs and then
    # answers. See `tools.py` for why an unlisted tool is refused rather than
    # retried.
    ALLOWED_TOOLS: tuple[str, ...]

    def output_schema(self) -> dict[str, Any]:
        """The grammar, generated from the skill's own output model.

        Generated rather than written beside it, so the thing that constrains
        the model and the thing that parses it cannot drift apart.
        """
        ...
