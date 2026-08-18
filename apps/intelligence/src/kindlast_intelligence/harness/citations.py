"""The citation validator (§26.3, ENT-218).

The single most important thing in this package, and the reason a 4B model is
allowed near a compliance product at all.

# WHY THIS EXISTS, IN ONE MEASUREMENT

`AGENTS.md` opens by saying a fabricated citation is worse than nothing,
because the product's value is that a human can check the claim against the
law. Here is what that looks like in practice, measured against the local model
during ENT-235 and not hypothesised:

Asked three times which GDPR article requires a record of processing
activities, with a JSON schema constraining the output shape, the 2B tier
answered article 50, then 34, then 54. The answer is 30. Every response was
schema-valid, well-formed, and confidently wrong, and two of them disagreed
with each other about the same question.

The grammar guarantees the shape. **Nothing guarantees the content.** So a
citation is not believed because the model produced it; it is believed because
it resolved against the corpus, and it is refused otherwise.

# REFUSING IS NOT DROPPING

A rejected citation is kept and reported, never silently removed. A validator
that quietly discarded bad citations would leave an `agent_runs` record
indistinguishable from a run where the model never tried to cite anything, and
the customer's question is precisely "what did it get wrong".
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Protocol


@dataclass(frozen=True)
class Citation:
    """A claim that some obligation supports what was written.

    Identified by slug rather than by id, matching CorpusService: a slug is
    stable across rewordings and is a key somebody can put in a document, where
    the row's uuid differs between two installations of the same law.
    """

    slug: str
    # What the model said this citation supports. Kept on a rejection so the
    # record shows what it was trying to claim, not merely that it failed.
    claim: str = ""


@dataclass
class ValidationResult:
    resolved: list[Citation] = field(default_factory=list)
    rejected: list[tuple[Citation, str]] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        """True when nothing was rejected.

        Deliberately not "true when at least one resolved". A narrative citing
        one real obligation and one invented one is not partially trustworthy;
        it is a document a customer would check, find wrong, and stop believing
        the rest of.
        """
        return not self.rejected


class ObligationLookup(Protocol):
    """What the validator needs: does this slug name a stored obligation.

    A Protocol rather than the core-api client, so the validator can be tested
    without a stack and so it cannot accidentally acquire the ability to do
    anything else with that client. The real implementation is one RPC.
    """

    def exists(self, slug: str) -> bool: ...


class CitationValidator:
    """Refuses any citation that does not resolve to a stored obligation."""

    def __init__(self, lookup: ObligationLookup) -> None:
        self._lookup = lookup

    def validate(self, citations: list[Citation]) -> ValidationResult:
        result = ValidationResult()
        seen: set[str] = set()

        for citation in citations:
            slug = citation.slug.strip()

            if not slug:
                result.rejected.append((citation, "empty slug"))
                continue

            # A repeated citation is not an error and not a second citation.
            # Deduplicated here so the record shows what was cited rather than
            # how many times the model mentioned it.
            if slug in seen:
                continue
            seen.add(slug)

            # NO NORMALISATION, NO FUZZY MATCH, NO NEAREST NEIGHBOUR.
            #
            # The temptation is to help: lowercase it, strip a suffix, find the
            # closest slug. Every one of those turns "the model invented this"
            # into "the model nearly got it right", and a near miss is exactly
            # the failure mode here. `gdpr-art-31-ropa` is not a typo for
            # `gdpr-art-30-ropa`; it is a different article, and silently
            # correcting it would produce a citation the customer checks and
            # finds says something else.
            if not self._lookup.exists(slug):
                result.rejected.append(
                    (citation, "does not resolve to a stored obligation")
                )
                continue

            result.resolved.append(Citation(slug=slug, claim=citation.claim))

        return result
