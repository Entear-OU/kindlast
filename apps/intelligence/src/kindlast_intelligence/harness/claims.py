"""The claim critic: the model explains applicability, it does not state law.

ENT-248, ruled after the first two live narrations on the 2B tier. Both cited
`gdpr-art-30-ropa` correctly, both were schema-valid, within budget and free of
em dashes, and both stated the law wrongly in the body:

  "the obligation to keep such records applies to every controller and
  processor, regardless of how small the company is"

  "it cannot claim the exemption that applies to organisations with fewer than
  250 employees"

The first asserts the opposite of Article 30(5), which exempts organisations
under 250 employees subject to its conditions. The second treats the absence of
a record as deciding a headcount question. The corpus summary the model was
handed says the exemption is narrow and that most SMEs cannot rely on it, so in
both cases the model contradicted the text it was given.

# WHY THE CITATION VALIDATOR STRUCTURALLY CANNOT SEE THIS

`citations.py` checks that a slug resolves to an obligation the run was offered.
Both runs cited the one obligation they were offered, so both passed, correctly.
The validator is working as designed. Nothing validated the CLAIM.

`AGENTS.md` opens by saying a fabricated citation is worse than nothing, because
the product's value is that a human can check the claim against the law. A
correct citation attached to a false claim is worse still: the customer who
checks the citation finds it valid and believes the sentence beside it.

# THE STRUCTURAL HALF COMES FIRST, AND THIS IS THE ENFORCEMENT HALF

The primary fix is not this file. It is that the skill's output splits
(`skills/analyst.py`): the model writes `why_it_applies_to_you`, about this
organisation, and the statement of law is taken verbatim from the corpus row by
the renderer. A model never asked to state the law cannot misstate it, and that
is a property of the design rather than a probability.

But `AGENTS.md` is explicit that the model may ask and only code refuses, and
the narrowing so far lives in a prompt. This is the code. It is the same
relationship `prose.py` has to the "do not use em dashes" line that models
ignored for two releases.

# WHY PATTERNS, AND NOT A SECOND MODEL

An LLM-as-judge was ruled out as a control (ENT-248, and the reason DeepEval and
Ragas were kept out of the gate at ENT-229): a control whose enforcement is
itself a probabilistic call by the same class of thing being controlled is not a
control, it is a second opinion. A pattern cannot be argued with, runs in
microseconds, and fails the same way twice. A judge model is welcome as an eval
SIGNAL, where being usually right is worth something.

The cost of a pattern is a false positive, and the direction of that cost was
chosen deliberately: a false positive is a refusal, a refusal is recorded, the
finding keeps the deterministic sentence the Watcher wrote, and nobody is
misinformed. A false negative is a customer told the opposite of the law with a
citation that checks out. Those are not symmetrical, so this errs towards
refusing.

# WHAT THIS CAN CATCH, AND WHAT IT CANNOT

It catches the SHAPE of a legal assertion: an article or recital number, a
universal quantifier over legal subjects, "regardless of", the vocabulary of
exemptions and thresholds, an instrument as the subject of "requires", and a
legal subject as the subject of "must". Both observed narratives trip it several
times over, and they are fixtures in the golden set.

It cannot catch a false statement of law that avoids every one of those shapes,
because it is a lexical check and not a reasoning one. "You are too small for
this to bite" is wrong in the same way and matches nothing here. That gap is
real, it is named in the pull request rather than papered over, and the reason
it is tolerable is that the structural split above is what makes such a sentence
rare, while this is what makes the common shapes impossible.

It also cannot tell a true statement of law from a false one, and does not try.
It refuses BOTH, because the product does not need the model to state the law
correctly: the corpus row already does, in text a human wrote.

# SECOND PERSON IS ALLOWED, THIRD PERSON IS NOT

"You need a written record of your candidate CVs" passes. "Controllers must
maintain a record" does not. That is the whole distinction the split encodes: a
sentence about this organisation is what the model was asked for, and a sentence
about a class of legal persons is the corpus's job. The patterns are written to
that line rather than to a list of forbidden words, which is why "must" alone is
not refused and "controllers must" is.
"""

from __future__ import annotations

import re

from .critics import Breach, CriticResult, excerpt

NAME = "legal_claim"

# The subjects a claim about the law is made ABOUT. Second person is absent on
# purpose: "you" is this organisation, which is exactly what the model was asked
# to write about.
_LEGAL_SUBJECT = (
    r"controllers?|processors?|organisations?|organizations?|companies|company"
    r"|businesses|business|firms?|deployers?|providers?|entities|entity"
)

# The instruments. An assertion whose subject is a regulation is a statement of
# law however it is phrased.
_INSTRUMENT = r"the law|the regulation|the gdpr|gdpr|the ai act|the act|article \d+"

# Each pattern is named, and the name is what the run record shows. Ordered
# most specific first only for readability; every one is evaluated, because a
# narrative that trips three is more informative than a narrative that trips
# the first.
PATTERNS: tuple[tuple[str, re.Pattern[str]], ...] = (
    (
        # A provision reference is the clearest case: the model has left
        # talking about the organisation and started talking about the
        # instrument. The citation belongs in `citations`, where it is
        # validated, and is rendered from the stored obligation.
        "a provision reference",
        re.compile(r"\b(?:articles?|arts?\.|recitals?|annexe?s?)\s*\d+", re.IGNORECASE),
    ),
    (
        "a provision reference",
        re.compile(r"\bannexe?s?\s+[ivxlc]+\b", re.IGNORECASE),
    ),
    (
        # The exact shape of the first observed narrative.
        "a claim about who the law applies to",
        re.compile(
            r"\bapplies\s+to\s+(?:every|all|any|each|both)\b"
            r"|\bapply\s+to\s+(?:every|all|any|each|both)\b"
            r"|\b(?:every|all|any|each)\s+(?:" + _LEGAL_SUBJECT + r")\b",
            re.IGNORECASE,
        ),
    ),
    (
        # "regardless of how small the company is" was the clause that turned a
        # merely general sentence into a false one.
        "a claim that the law admits no exception",
        re.compile(
            r"\bregardless\s+of\b|\birrespective\s+of\b|\bno\s+matter\s+how\b"
            r"|\bwithout\s+exception\b|\bin\s+all\s+cases\b",
            re.IGNORECASE,
        ),
    ),
    (
        # The shape of the second observed narrative. Exemptions and thresholds
        # are where the law is most conditional and a weak model is most
        # confident, and they are precisely what the authored summary carries.
        "a claim about an exemption or a threshold",
        re.compile(
            r"\bexempt\w*\b|\bexception\w*\b|\bderogation\w*\b|\bcarve[- ]?out\b"
            r"|\bthresholds?\b|\bfewer\s+than\s+\d+\b|\bmore\s+than\s+\d+\s+employees\b"
            r"|\bunder\s+\d+\s+employees\b|\bat\s+least\s+\d+\s+employees\b",
            re.IGNORECASE,
        ),
    ),
    (
        # An instrument as the subject of a requirement verb.
        "a statement of what the law requires",
        re.compile(
            r"\b(?:" + _INSTRUMENT + r")\s+"
            r"(?:requires?|mandates?|obliges?|demands?|states?|says?|provides?"
            r"|stipulates?|prohibits?|forbids?|permits?|allows?)\b",
            re.IGNORECASE,
        ),
    ),
    (
        # A class of legal persons as the subject of an obligation verb. "You
        # must" is deliberately not here.
        "an obligation stated over a class of organisations",
        re.compile(
            r"\b(?:" + _LEGAL_SUBJECT + r")\s+"
            r"(?:must|shall|are\s+required\s+to|is\s+required\s+to|have\s+to"
            r"|has\s+to|are\s+obliged\s+to|need\s+to)\b",
            re.IGNORECASE,
        ),
    ),
)

_PREAMBLE = (
    "the text states the law rather than explaining applicability to this "
    "organisation, and the statement of law is the corpus's to make: the "
    "obligation summary beside it is written by a person and this text is not"
)


class ClaimCritic:
    """Refuses a free-text field that asserts law.

    A class rather than a function so it satisfies the `Critic` protocol
    alongside `ProseCritic`, and so a future skill with a differently named
    free-text field configures one rather than forking one.
    """

    name = NAME

    def review(self, text: str) -> CriticResult:
        """Every match from every pattern, in the order they appear in the text.

        Every one rather than the first, for the reason `prose.py` gives: one
        rewrite should fix the whole narrative, and stopping at the first would
        have the caller re-run and be refused on the second, paying a model call
        per sentence.

        Overlapping matches from different patterns are kept as separate
        breaches. "applies to every controller" is both a universal claim and a
        legal subject, and a reader of the record is better served by seeing
        that two independent rules objected than by an arbitrary choice between
        them.
        """
        breaches = [
            Breach(
                pattern=name,
                matched=match.group(0),
                index=match.start(),
                excerpt=excerpt(text, match.start(), len(match.group(0))),
            )
            for name, pattern in PATTERNS
            for match in pattern.finditer(text)
        ]
        breaches.sort(key=lambda b: (b.index, b.pattern))

        return CriticResult(critic=NAME, preamble=_PREAMBLE, breaches=breaches)


def review_claims(text: str) -> CriticResult:
    """Convenience for tests and for anything that wants one call."""
    return ClaimCritic().review(text)
