# Roadmap

Where Kindlast is going, and roughly in what order. This is a direction of
travel, not a set of commitments with dates. Priorities move when reality
argues with them.

If something here matters to you, say so on the relevant issue. That is a real
input into ordering, particularly for the integrations.

## Now

Proving the core loop end to end: a company onboards, an agent finds something
real, a notification reaches them outside the app, they approve in one tap, and
a compliance record is created without anyone opening a spreadsheet.

| | Status |
|---|---|
| Conversational onboarding and compliance profile | Built |
| Initial posture assessment | Built |
| Analyst findings with cited obligations | Built |
| Feed with approve, reject and snooze | Built |
| ROPA, DSAR log, AI systems register | Built |
| Compliance dashboard: posture, open items, deadlines | Built |
| Email notifications with one-tap actions | Built |
| Weekly briefing and deadline alerts | Built |
| Regulatory corpus: GDPR, AI Act, EDPB, enforcement decisions | Built |
| Free and Pro tiers | Built |
| **Watcher on a daily schedule** | In progress |
| **Executor creating ROPA entries and DSAR tasks on approval** | In progress |

The two in progress are what close the loop. Until the Watcher runs on its own
schedule, findings depend on something else triggering analysis, and until the
Executor writes records, an approval does not yet become a compliance artefact.

## Next

**WhatsApp Business notifications.** The highest open rate of any channel we
could use, and no app for the recipient to install. Deliberately not first:
Meta Business API verification takes weeks, and blocking the core loop on it
would have been a poor trade.

**Slack integration.** For engineering and product teams already living there.

**Multi-user and team routing.** Today an account is one person. Routing
engineering findings to the technical lead and HR findings to the operations
lead needs a team model first.

**Full DPIA workflow.** Today Kindlast can tell you a DPIA is required and
create the task. Actually walking someone through one is a larger piece of work.

## Later

- **GitHub and Jira monitoring**, so the Watcher can see a compliance-relevant
  change at the moment it lands rather than when someone remembers to mention
  it. Large integration surface, hence the ordering.
- **Portfolio view for DPOs and consultants** managing several client
  organisations.
- **UK GDPR and other jurisdictions.** EU first, deliberately. Doing one
  regulatory environment properly beats doing three shallowly.
- **Voice briefings.**
- **Sector-specific guidance.** Horizontal product first.

## Not planned

Some things get asked for often enough to be worth answering up front.

**Kindlast will not give legal advice.** It summarises regulation, cites
sources, and proposes actions. Where a summary and the regulation disagree, the
regulation governs, and the product says so. Crossing that line would change
what this product is and what it can responsibly claim.

**Agents will not act without approval.** The Executor applies approved changes
only. An agent that silently edits your compliance records is not a feature, it
is a liability, and the architecture separates the agents specifically to
prevent it.

## Contributing to this

The integrations are the most parallelisable work here and the most amenable to
outside contribution. If you want to pick something up, open an issue first so
we can talk about fit before you spend real time on it. See
[CONTRIBUTING.md](../CONTRIBUTING.md).
