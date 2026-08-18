"""One seam for every critic that refuses generated prose (ENT-248, §26.3).

# WHY THERE IS A SEAM AT ALL, RATHER THAN TWO CRITICS SIDE BY SIDE

ENT-163 added the house-style critic (`prose.py`) and ENT-248 adds the claim
critic (`claims.py`). Written independently they would have been two hand-rolled
scanners, two excerpt windows, two truncation rules, two detail formats and two
call sites in `run.py`, and the second one written would have quietly disagreed
with the first about how a refusal reads. ENT-248 makes the shared seam an
acceptance criterion for exactly that reason.

So this module owns everything that is the same between critics: what a breach
is, how much text is quoted around it, how many are spelled out before the rest
are counted, how the stored detail is assembled, and the order the ring runs
them in. A critic owns only the thing that makes it that critic: what it looks
for, and the one sentence explaining why that is refused.

# A BREACH RECORDS THE PATTERN THAT FIRED, NOT ONLY THAT ONE DID

`agent_runs` is read by a customer asking why they have no narrative, and
"refused on house style" answers nothing they can act on. Every breach carries
the named rule, the text that matched it and the words around it, so the record
says what was rejected and what rejected it. ENT-248 asks for both halves by
name.

# CRITICS REFUSE, THEY NEVER REWRITE

Inherited from `prose.py` and worth restating where the shared code lives,
because the seam is where somebody would add a `fix()` method. A rewrite is an
edit to a claim about the law made by no author, it erases the signal the golden
set measures (a weak model would score as a compliant one), and it would be the
only guardrail here that repairs rather than stops. The citation validator, the
truncation check and the budgets all refuse.
"""

from __future__ import annotations

from typing import Mapping, Protocol, Sequence

from pydantic import BaseModel, ConfigDict, Field

# How much of the sentence to quote either side of the offending text. Not the
# whole narrative: the detail is stored on the run and read in a list, and a
# paragraph pasted into that column makes the useful part unfindable.
WINDOW = 32

# How many breaches to spell out before summarising. A model that produced
# twenty would otherwise produce a detail nobody reads to the end of, and the
# remedy after the third is identical to the remedy after the first.
MAX_REPORTED = 3


class Breach(BaseModel):
    """One thing a critic refused, and enough context to find it.

    Frozen, because a finding some later step can edit is not a finding.
    """

    model_config = ConfigDict(frozen=True, extra="forbid")

    # The named rule that fired, in the words the record will show. ENT-248
    # asks for the pattern by name rather than for a boolean, because "the
    # claim critic refused this" and "the claim critic refused this for
    # asserting a universal rule" send a reader to different places.
    pattern: str
    # The exact text that matched. Kept beside the excerpt rather than derived
    # from it: the excerpt is padded and elided for reading, and a reader
    # comparing two refusals wants the thing itself.
    matched: str
    # Zero-based, as an index into the text. Rendered as a one-based character
    # position in `detail`, because that is how a person counts.
    index: int
    excerpt: str


class CriticResult(BaseModel):
    """What one critic made of one piece of text."""

    model_config = ConfigDict(extra="forbid")

    # The critic's own name, so a run record and a golden case can assert which
    # control fired rather than only that something did.
    critic: str
    # The sentence that goes in front of the list, supplied by the critic
    # because it is the only part that is about what this critic is for.
    preamble: str
    breaches: list[Breach] = Field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.breaches

    @property
    def detail(self) -> str:
        """What the run record says, written for the customer reading it.

        Names the rule, gives the position, and quotes the words around it. A
        refusal somebody cannot act on reads to them as the harness having
        broken, which is the opposite of what a working guardrail should look
        like.
        """
        if self.ok:
            return ""

        shown = [
            f'{b.pattern} at character {b.index + 1}, in "{b.excerpt}"'
            for b in self.breaches[:MAX_REPORTED]
        ]
        remaining = len(self.breaches) - len(shown)
        if remaining > 0:
            shown.append(f"and {remaining} more")

        return f"{self.preamble}: " + "; ".join(shown)


class Critic(Protocol):
    """A thing that reads generated text and refuses some of it.

    Deliberately not "a thing that reads a narrative": the claim critic reads
    one field of the output and the house-style critic reads the same field
    today, but the contract is about text rather than about the skill's schema,
    so a second free-text field costs a call rather than a redesign.
    """

    name: str

    def review(self, text: str) -> CriticResult: ...


def excerpt(
    text: str,
    index: int,
    length: int = 1,
    redact: Mapping[str, str] | None = None,
) -> str:
    """The words around a match, with anything unquotable replaced.

    # THE QUOTE MAY NOT CARRY THE THING IT IS REPORTING

    `redact` exists for the house-style critic, whose whole subject is a
    character that must not appear in stored copy. The detail is written into
    `agent_runs` and read back on a page, so quoting the raw character would put
    the thing being refused into the very text a customer reads, and would make
    the record itself a hit for anybody searching stored copy for em dashes.

    The claim critic passes nothing, because its matches are ordinary words and
    a redacted quote would hide the sentence a reader needs to see.

    Newlines are flattened for the same reason the excerpt is short: this is one
    field on one row, and a detail that wraps to five lines is a detail nobody
    finishes.
    """
    start = max(0, index - WINDOW)
    end = min(len(text), index + length + WINDOW)

    window = text[start:end]
    for character, replacement in (redact or {}).items():
        window = window.replace(character, replacement)

    window = " ".join(window.split())

    prefix = "..." if start > 0 else ""
    suffix = "..." if end < len(text) else ""
    return f"{prefix}{window}{suffix}"


def first_breach(text: str, critics: Sequence[Critic]) -> CriticResult | None:
    """Run the critics in order and return the first that refuses.

    # IN ORDER, AND THE ORDER IS HOW BADLY A CUSTOMER IS SERVED

    The ring's existing rule, extended rather than reinvented: a fabricated
    citation outranks everything, then a false statement of law, then
    typography. A narrative that both misstates Article 30 and uses an em dash
    is refused for the statement of law, because a record reporting the
    typography would send somebody to fix the wrong thing.

    # AND THE FIRST ONE ONLY

    Not every critic's findings merged into one detail. A customer reading the
    record is deciding what to do about one refusal, and a detail listing
    breaches from three different controls buries the one that matters. The
    remedy for the first is a re-run, which is where the second would be found
    anyway.
    """
    for critic in critics:
        result = critic.review(text)
        if not result.ok:
            return result
    return None
