"""The house-style critic: no em dashes, no en dashes (ENT-163, §26.3).

# WHY THIS IS CODE AND NOT A SENTENCE IN THE PROMPT

The prompt already asks. `analyst.SYSTEM_PROMPT` has ended with "do not use em
dashes or en dashes" since ENT-160, and the copy kept coming back with them,
which is the whole reason ENT-163 exists. `AGENTS.md` states the rule this
violated: the model may ask, only code refuses, and a prompt is never the thing
that prevents an action.

A request is a probability. This is a comparison. The two are not the same kind
of object and swapping one for the other is the mistake OWASP LLM01 describes:
a control whose enforcement lives inside the thing being controlled.

# WHY IT REFUSES AND DOES NOT REWRITE

Replacing the character would be one line and it is the wrong line, for three
reasons that all point the same way.

A rewrite is an edit to a claim about the law, made by no author, after the
model's output has been validated and before a person sees it. Even a
character swap changes text a customer may quote to a regulator, and the run
record would then hold a narrative nobody generated.

A rewrite also erases the signal. The golden set measures which model tier
produces usable copy (ENT-229); silently repairing the output makes a weak
model score as a compliant one, and the next person choosing a tier reads a
number that is no longer about the model.

And a rewrite would be the only guardrail here that fixes rather than stops.
The citation validator refuses rather than curating, the truncation check
refuses rather than trimming, and the budgets refuse rather than borrowing.
Refusal is cheap since ENT-245 gave the narrative its own column, so a refused
run costs nothing but the tokens already spent.

# EXACTLY TWO CHARACTERS

U+2014 and U+2013, because those are the two `AGENTS.md` names. The
hyphen-minus is deliberately not here: the rule allows hyphens in compound
words (`plain-language`) and in numeric ranges (`2-4 hours`), and a check that
refused those would refuse most correct narratives, which is the shape of
guardrail somebody eventually switches off.

The neighbouring code points (U+2012 figure dash, U+2015 horizontal bar,
U+2212 minus sign) are not covered. They are not what the rule names, none has
been observed in this service's output, and adding one is a decision with its
own golden case rather than a quiet widening of a set.
"""

from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field

# Written as escapes rather than as the characters themselves, so this file
# obeys the rule it enforces and so a reader can tell the two apart. On screen
# they differ by a few pixels, which is exactly why a human reviewer is not a
# control either.
EM_DASH = "\u2014"
EN_DASH = "\u2013"


FORBIDDEN: dict[str, str] = {
    EM_DASH: "em dash (U+2014)",
    EN_DASH: "en dash (U+2013)",
}

# How much of the sentence to quote either side of the offending character. Not
# the whole narrative: the detail is stored on the run and read in a list, and
# a paragraph pasted into that column makes the useful part unfindable.
_WINDOW = 32

# How many to spell out before summarising. A model that produced twenty would
# otherwise produce a detail nobody reads to the end of, and the remedy after
# the third is identical to the remedy after the first.
_MAX_REPORTED = 3


class DashFound(BaseModel):
    """One forbidden character, and enough context to find it.

    Frozen, because a finding some later step can edit is not a finding.
    """

    model_config = ConfigDict(frozen=True, extra="forbid")

    character: str
    name: str
    # Zero-based, as an index into the text. Rendered as a one-based character
    # position in `detail`, because that is how a person counts.
    index: int
    excerpt: str


class ProseResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    found: list[DashFound] = Field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.found

    @property
    def detail(self) -> str:
        """What the run record says, written for the customer reading it.

        Names the character, gives its position, and quotes the words around
        it, because "refused on house style" tells somebody nothing they can
        act on and reads as the harness having broken.
        """
        if self.ok:
            return ""

        shown = [
            f'{f.name} at character {f.index + 1}, in "{f.excerpt}"'
            for f in self.found[:_MAX_REPORTED]
        ]
        remaining = len(self.found) - len(shown)
        if remaining > 0:
            shown.append(f"and {remaining} more")

        return (
            f"the narrative uses {len(self.found)} dash character(s) the house "
            "style does not allow, and the prompt asking the model not to is a "
            "request rather than a control: " + "; ".join(shown)
        )


def review_prose(text: str) -> ProseResult:
    """Find every forbidden dash, in order.

    Every one rather than the first, so a single rewrite fixes the narrative.
    Stopping at the first would have the caller re-run, be refused on the
    second, and pay a model call per character.
    """
    return ProseResult(
        found=[
            DashFound(
                character=character,
                name=FORBIDDEN[character],
                index=index,
                excerpt=_excerpt(text, index),
            )
            for index, character in enumerate(text)
            if character in FORBIDDEN
        ]
    )


def _excerpt(text: str, index: int) -> str:
    """The words around the character, with the character itself named.

    # THE QUOTE DOES NOT CARRY THE CHARACTER IT IS REPORTING

    Every forbidden dash inside the window is replaced by its name in brackets,
    including the one being reported. The detail is stored in `agent_runs` and
    read back on a page, so quoting the raw character would put the thing being
    refused into the very text a customer reads, and would make the record
    itself a hit for anybody searching stored copy for em dashes.

    Newlines are flattened for the same reason the excerpt is short: this is
    one field on one row, and a detail that wraps to five lines is a detail
    nobody finishes.
    """
    start = max(0, index - _WINDOW)
    end = min(len(text), index + _WINDOW + 1)

    window = text[start:end]
    for character, name in FORBIDDEN.items():
        # The bracketed short name rather than the full one with its code
        # point: the code point is already stated once beside the position, and
        # repeating it inside the quote crowds out the words that locate it.
        window = window.replace(character, f"[{name.split(' (')[0]}]")

    window = " ".join(window.split())

    prefix = "..." if start > 0 else ""
    suffix = "..." if end < len(text) else ""
    return f"{prefix}{window}{suffix}"
