"""The link critic: a message this product sends carries no address the model
wrote (ENT-260, §26.3).

# WHY A MESSAGE NEEDS A CONTROL THE OTHER SKILLS DO NOT

Everything the Analyst and the Hands write is read on a page, behind the
reader's own sign-in, beside the finding it is about. What the Messenger writes
leaves the building. It arrives in a mailbox or a chat the recipient did not
open at that moment, under our From: header, from a product they trust enough
to have connected to their compliance record.

A URL in that message is therefore not a formatting question. A model that can
put one there has been handed a way to send a phishing email from us, and the
recipient has every reason to click it. The same is true of an address to reply
to and a number to ring: both are ways of moving somebody off the channel they
verified and onto one nobody checked.

# AND WHY IT IS WRITTEN NOW, WHILE THE CONTEXT IS SMALL AND CLEAN

A Messenger run today sees an organisation's name, a severity and two counts.
Nothing a stranger typed reaches it, so the realistic failure this week is a
model that hallucinates `https://kindlast.example/findings` because that is
what notification emails look like in its training data, which is merely
embarrassing.

That is exactly the moment to write the control. The context will widen: §17.1
is under argument (see `skills/messenger.py`), a DSAR's subject name comes from
a form and a finding's narrative came from a model, and the first of those to
arrive brings the payload with it. A guardrail added after the input it guards
against is a guardrail somebody has to argue for twice.

# EVERY LINK A DOORBELL NEEDS IS MINTED, NOT WRITTEN

The finding link, the one-tap approve link and the unsubscribe link are built
per recipient inside the delivery transaction, from that person's own
organisation and their own capability token, and appended by
`notify.FindingNotification`. So a correct draft has no link in it, and there
is nothing this critic refuses that a well-behaved run wanted to say.

# IT REFUSES, IT DOES NOT STRIP

`prose.py` gives the argument in full and it lands harder here. Removing a URL
from a message and sending the rest would mean a customer receiving copy no
author wrote, with a sentence that now points at nothing, and a run record
claiming a draft that was never delivered. It would also hide the signal:
"which model tier writes links into outbound mail" is a thing a deployment
choosing a model wants to know, and quietly repairing the output makes a weak
model score as a safe one.
"""

from __future__ import annotations

import re

from .critics import Breach, CriticResult, excerpt

NAME = "no_links"

# THE THREE SHAPES, EACH NAMED, BECAUSE THE RECORD REPORTS WHICH ONE FIRED.
#
# A boolean would tell a maintainer that something was refused and not what to
# go and look at, which is the mistake `critics.Breach` exists to avoid.
_PATTERNS: tuple[tuple[str, re.Pattern[str]], ...] = (
    # Anything with a scheme. Deliberately not a list of schemes: `http`,
    # `https`, `data`, `javascript` and whatever a model invents are all
    # equally not ours, and a whitelist here would be a list somebody has to
    # keep up with an attacker's imagination.
    #
    # The match runs to the next space on purpose, so what the record quotes is
    # the whole address rather than the four characters that announced it. A
    # reader of `agent_runs` wants to see where the message was pointing.
    (
        "a link with a scheme",
        re.compile(r"\b[a-z][a-z0-9+.\-]{1,31}://\S*", re.IGNORECASE),
    ),
    # The schemes that carry no `//` and are the two ways to move somebody off
    # the channel they verified.
    ("a mail or telephone link", re.compile(r"\b(?:mailto|tel|sms):\S*", re.IGNORECASE)),
    ("an email address", re.compile(r"[^\s@<>()\[\]]+@[a-z0-9.\-]+\.[a-z]{2,}", re.IGNORECASE)),
    # A bare host, either announced by `www.` or ending in one of the endings
    # below.
    #
    # THE ENDINGS ARE A LIST, AND THE LIST IS THE DECISION. The general rule,
    # "a word, a dot, and two or more letters", refuses "the finding.The
    # deadline" every time a model forgets a space after a full stop, which is
    # often, and a guardrail that fires on ordinary prose is one somebody
    # eventually switches off. So this covers what an invented link actually
    # looks like and accepts that a host under an ending nobody listed reads as
    # prose. It is not the only control: a host without a scheme is not a link
    # in a plain-text mail or in a Telegram message, so what it buys is
    # catching the copy that READS as a link rather than the one that is one.
    (
        "a web address",
        re.compile(
            r"\bwww\.[a-z0-9\-]|"
            r"\b[a-z0-9\-]{2,}\."
            r"(?:com|net|org|io|co|dev|app|ai|eu|ee|uk|de|info|biz|xyz|link|site|online"
            # RFC 2606's reserved names, which are not registrable and are
            # therefore the ones a model reaches for: every notification email
            # in every document it was trained on points at example.com.
            r"|example|test|invalid)\b",
            re.IGNORECASE,
        ),
    ),
)

_PREAMBLE = (
    "the message contains something that reads as a link or an address, and "
    "every link a notification needs is minted per recipient and added after "
    "the draft, so anything written here is one nobody minted"
)


class LinkCritic:
    """Refuses a message carrying an address the model wrote."""

    name = NAME

    def review(self, text: str) -> CriticResult:
        """Find every match, in the order they appear in the text.

        Every one rather than the first, on `prose.py`'s argument: a single
        rewrite should fix the whole draft rather than costing a model call per
        occurrence. Sorted by position rather than by pattern so the detail
        reads in the order somebody would find them.
        """
        found = [
            (match.start(), match.end(), name, match.group(0))
            for name, pattern in _PATTERNS
            for match in pattern.finditer(text)
        ]
        # ONE BREACH PER PIECE OF TEXT, NOT ONE PER RULE THAT NOTICED IT.
        #
        # The patterns overlap by design: `https://acme.example` is a scheme
        # and the host inside it is a web address, and `us@acme.example` is an
        # address and the half after the `@` is a host. Reporting each twice
        # would make a record listing four problems for two, and would make the
        # count of how often each rule fires meaningless.
        #
        # Earliest first, longest first at a tie, and then anything overlapping
        # something already kept is dropped. That leaves the widest rule that
        # starts soonest, which is the one that names what a reader is looking
        # at: "a link with a scheme" rather than "a web address" for a URL.
        found.sort(key=lambda f: (f[0], -(f[1] - f[0])))
        kept: list[tuple[int, int, str, str]] = []
        for start, end, name, matched in found:
            if kept and start < kept[-1][1]:
                continue
            kept.append((start, end, name, matched))

        return CriticResult(
            critic=NAME,
            preamble=_PREAMBLE,
            breaches=[
                Breach(
                    pattern=name,
                    matched=matched,
                    index=start,
                    excerpt=excerpt(text, start, end - start),
                )
                for start, end, name, matched in kept
            ],
        )


def review_links(text: str) -> CriticResult:
    """Convenience for tests and for anything that wants one call."""
    return LinkCritic().review(text)
