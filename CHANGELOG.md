# Changelog

Notable changes to Kindlast, in the format of
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versioning follows
[Semantic Versioning](https://semver.org), and
[`docs/versioning.md`](./docs/versioning.md) says what a breaking change means
for this product.

Entries are written rather than generated. The audience is somebody upgrading a
system that holds their compliance record, so an entry says what changed and
what they have to do about it, which no commit subject knows.

## [Unreleased]

### Added

- **An organisation can choose its own model provider, and the choice is a
  compliance event rather than a preference** (ENT-236).

  The bundled stack runs its own model and needs no API key, which is what lets
  a deployment holding a compliance record run with no outbound internet at
  all. Pointing one organisation at a hosted provider is the act of giving that
  up: from then on its compliance profile, its findings and its DSAR content
  are processed by somebody else, which lengthens that customer's own
  sub-processor list and is a processing decision they have to be able to
  account for.

  So it is shaped as a decision, not a switch:

  - **Owner only**, and only when a person has been shown in plain language
    what changes and confirmed it. The confirmation is enforced by core-api and
    not by the console, so an API caller cannot skip the warning either.
  - **It writes an `audit_log` row** through the same function every other
    decision goes through, with the provider, the endpoint and who decided. It
    lists, filters and exports with the rest of the record, so a customer asked
    "since when has your findings text been going to a US provider" can answer
    from their own audit log.
  - **The choice cannot be edited**, only replaced. Switching provider revokes
    one row and inserts another, so the sequence of rows is the history of
    where that customer's data has been processed.
  - **Every agent run records which provider served it**, so the period a
    provider was in use is an answerable question rather than an inference.
  - **Turning it back off destroys the stored key** in the same statement that
    revokes the choice. It cannot reach content the provider already processed,
    and the product says so rather than implying an off switch is a recall.

  **For a self-hoster the important line is that this is off unless you switch
  it on.** `KINDLAST_BYOK_PROVIDERS` is empty by default and an empty list
  permits nobody, so "nobody at this company may point our compliance data at
  an external API" stays enforceable in your configuration rather than being a
  policy every organisation owner has to be trusted to follow. Entries are
  written `name=host` (`openai=api.openai.com`), because the host is what an
  endpoint is checked against; a leading dot makes an entry a suffix for a
  customer's own subdomain. Endpoints must be HTTPS, must be on the host you
  permitted, and must resolve to a public address, and all of that is checked
  again on every use rather than once when it is saved.
  [`docs/self-hosting.md`](./docs/self-hosting.md) has the whole of it.

- **A public readiness assessment at `/readiness`** (ENT-189). A visitor
  answers the questions a data protection officer would ask, with no account
  and no sign-up, and gets back the obligations in the Kindlast corpus that
  match their answers, each quoting the corpus entry and citing the regulation
  behind it.

  Three things a self-hoster should know about it, because they are properties
  of the deployment rather than of the page:

  - **It has no server side.** The corpus is compiled into the bundle at build
    time and the applicability rules are a pure function in the browser. There
    is no route handler, no server action and no call to `core-api`, so the
    page adds no unauthenticated surface to your deployment, needs no rate
    limit, and costs nothing per visitor beyond serving static output.
  - **It stores nothing, anywhere.** Answers live in one React state hook for
    the life of the tab. No database row, no cookie, no `localStorage`, no
    query string, and no request. A refresh loses the assessment, deliberately.
  - **Every statement of law on it is a corpus row, verbatim.** If you ship a
    modified `data/corpus/`, this page renders your text. If you ship no
    changes, it renders ours. Nothing on the page paraphrases a regulation, and
    a test fails the build if a string ever tries to.

  There is no email capture, which the issue asked for and which is held for a
  second iteration: sending the summary means putting somebody's address and
  their answers through a mail provider, and that needs a lawful basis, a
  notice, a retention position and an answer to a subject access request about
  the assessment itself, written down before the code rather than after it.

### Changed

- The local stack can run once per checkout instead of once per machine.
  `deploy/compose.yaml` still defaults to the project name `kindlast` and to
  the ports every instruction here names, so a self-hoster with one clone sees
  no change at all. What is new is that `COMPOSE_PROJECT_NAME`, the
  `KINDLAST_*_PORT` variables and `KINDLAST_MODEL_DIR` now reach every part of
  the stack, including container names, so a second copy of the repository can
  bring up a second stack that shares nothing with the first.
  `scripts/stack-env.sh` derives a consistent set of those values, and
  `docs/maintainers.md` explains it. Relevant to anyone running two
  environments from one machine, and to anyone whose tooling assumed a
  container was called `kindlast-postgres-app`: it still is, unless a project
  name is set.

## [0.1.0]

The version the repository has carried in its manifests since the beginning,
recorded here so the history starts somewhere rather than appearing to begin
mid-sentence. No tag was cut for it, and this file does not attempt to
reconstruct the changes that led to it: the git history is the record for
everything before the first release.

[Unreleased]: https://github.com/Entear-OU/kindlast/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Entear-OU/kindlast/releases/tag/v0.1.0
