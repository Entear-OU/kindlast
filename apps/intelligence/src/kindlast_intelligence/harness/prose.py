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

# THE SCANNING, THE WINDOW AND THE DETAIL FORMAT MOVED OUT (ENT-248)

They live in `critics.py` now, shared with the claim critic. ENT-248 made one
refusing-critic seam an acceptance criterion rather than a preference: written
separately, the second critic would have had its own excerpt window, its own
truncation rule and its own detail format, and a customer reading two refusals
would have found them written by two different products.

What stayed here is the only part that is about house style: which characters,
and the sentence explaining why they are refused.
"""

from __future__ import annotations

from .critics import Breach, CriticResult, excerpt

NAME = "house_style"

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

# What the excerpt puts in place of each forbidden character. The bracketed
# short name rather than the full one with its code point: the code point is
# already stated once beside the position, and repeating it inside the quote
# crowds out the words that locate it.
_REDACTIONS = {
    character: f"[{name.split(' (')[0]}]" for character, name in FORBIDDEN.items()
}

_PREAMBLE = (
    "the narrative uses dash characters the house style does not allow, and "
    "the prompt asking the model not to is a request rather than a control"
)


class ProseCritic:
    """Refuses a text containing a character `AGENTS.md` forbids."""

    name = NAME

    def review(self, text: str) -> CriticResult:
        """Find every forbidden dash, in order.

        Every one rather than the first, so a single rewrite fixes the
        narrative. Stopping at the first would have the caller re-run, be
        refused on the second, and pay a model call per character.
        """
        return CriticResult(
            critic=NAME,
            preamble=_PREAMBLE,
            breaches=[
                Breach(
                    pattern=FORBIDDEN[character],
                    matched=character,
                    index=index,
                    excerpt=excerpt(text, index, 1, redact=_REDACTIONS),
                )
                for index, character in enumerate(text)
                if character in FORBIDDEN
            ],
        )


def review_prose(text: str) -> CriticResult:
    """Convenience for tests and for anything that wants one call."""
    return ProseCritic().review(text)
