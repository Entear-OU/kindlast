# Regulation packs

Kindlast ships GDPR and the EU AI Act. SOC 2 and others come later, and the
point of this document is that the third one should be a data change plus a
bounded migration rather than a redesign.

Two regulations can be special-cased. Three cannot, and the cost of finding
that out late is a schema and an ingest path shaped around two specific laws.
So the boundary is drawn here, before the third act arrives, along with an
honest inventory of the places it is not drawn yet.

Written for ENT-233. The parent epic is ENT-227.

## What a pack is

A pack is one body of regulation and everything the product derives from it:

| Part | Where it lives | Adding one is |
|---|---|---|
| The corpus | `data/corpus/*.json`, listed in `packs.json` | data |
| Obligations | `data/corpus/obligations.json` | data |
| The applicability vocabulary | `apps/core-api/internal/domain/corpus/applieswhen.go` | code |
| Skills and evals | `apps/intelligence/` | code |
| Record types | `db/migrations/` plus `proto/` plus the console | migration |
| Executor actions | `db/migrations/` plus `proto/` plus the console | migration |

The first two rows are the boundary. Everything above the line is a file a
curator edits, reviewable by somebody who knows the law and not the codebase.
Everything below it is engineering work, and the amount of it is the thing this
document is trying to keep small.

### A pack is not a file

`packs.json` names packs, and the id is the curator's rather than a file name,
because the two are not the same thing:

- `obligations.json` spans both regulations today. An obligation's pack
  membership is `citation.celex`, the law it cites, and not the file it happens
  to sit in.
- A regulation could arrive as several files. The AI Act's Annex III was
  briefly its own file (`apps/web/scripts/wire-annex-and-dates.ts` is the
  one-shot script that merged it).

This is why `corpuspack.Pack.ID` stopped being a file name in ENT-233.

## The boundary, stated as a rule

**A pack may bring new data and new vocabulary. It may not bring a new shape.**

A new regulation is data: documents, articles, recitals, annexes, obligations,
guidelines, enforcement decisions. Those tables are regulation-agnostic already
and take a third act without a migration.

A new *kind of thing a customer holds* is a shape, and shapes are migrations.
SOC 2 wants controls and evidence artefacts, and the three record types
(the Article 30 register, the AI system register, the DSAR log) do not express
either. That is the bounded migration a third pack is allowed to cost, and
sizing it is the next section.

### The corpus has no `org_id`, and must not get one

It is the same law for every customer. Ten public-read policies carry it with
`using (true)`, `db/tests/corpus-write-path.test.ts` asserts their absence of
tenancy on purpose, and ENT-207 says so in terms.

A migration adding `org_id` to the corpus to make it look like the rest of the
schema would break cross-tenant reads while reading in review as a consistency
fix. It is called out here because a pack boundary is exactly the context in
which somebody would think of the corpus as per-customer. It is not: the pack
is shared, and what is per-customer is which obligations apply.

## Adding a pack: the procedure

### 1. The corpus, which is data

Add the regulation's JSON to `data/corpus/` and list it in `packs.json`:

```json
{ "id": "soc2", "kind": "document", "file": "soc2.json", "title": "SOC 2" }
```

`kind` is one of `document`, `obligations`, `guidelines`, `enforcement`.
**Order matters and the manifest is where it is decided**: obligations cite
articles, the citation check reads the database, so every regulation goes
before the obligations that cite it. `corpuspack_test.go` asserts this.

Then ingest it:

```bash
corpus-load -api http://localhost:8080 -token "$TOKEN" -dry-run
```

The dry run reports every unresolved citation at once. No Go change is needed
for any of this, which is the property ENT-233 added and the thing to preserve.

### 2. The obligations, which are data with a vocabulary

Each obligation carries `appliesWhen`, saying who it binds. **The vocabulary is
closed**, declared in `apps/core-api/internal/domain/corpus/applieswhen.go`, and
an obligation using a token outside it is refused at ingest.

That refusal is deliberate and the reasoning is in the next section. If your
pack needs a new question asked about an organisation, that is code, and it is
the one place a pack reliably needs some.

### 3. Skills and evals, which are code

Per-pack, in `apps/intelligence/`, pinned by version like everything else the
model touches. Out of scope for ENT-233 beyond naming them as part of a pack.

### 4. Record types and executor actions, which are a migration

Only if the pack produces a kind of record the product does not have. Adding
one means, at minimum:

- a table with `org_id`, `FORCE ROW LEVEL SECURITY`, and policies in the
  two-GUC form (see AGENTS.md; `bun run test:db` checks this over `pg_class`)
- a value added to `findings_action_type_check` and
  `obligations_action_type_check`
- an executor trigger, or its Go successor once ENT-225 lands
- proto, generated code, console surface

This is the bounded migration. It is bounded because the corpus half above
costs nothing.

## The applicability vocabulary, and why it is closed

This is the finding ENT-233 turned up, and it is the reason the issue was worth
doing before a third act rather than after.

`applies_when` was opaque from end to end. The curator wrote JSON, the loader
kept it as `json.RawMessage` on purpose, the proto carries it as a string, the
column is `jsonb`, and the only thing with an opinion about its contents was a
pair of plpgsql functions in `00001_baseline.sql`. Nothing in between could tell
a token the Watcher evaluates from one it had never heard of.

**At two regulations the vocabulary had already drifted, in both directions, and
nothing anywhere went red.** ENT-233 wrote the vocabulary down and refused
unknown tokens at ingest, which made the drift visible. ENT-246 closed it.

| Token | Was | Now |
|---|---|---|
| `thresholds.high_risk` | written by 4 obligations, read by nobody | split into `high_risk_processing` and `high_risk_ai_system`, each read from its own profile fact |
| `thresholds.large_scale_monitoring` | written by 1 obligation, read by nobody | read from the `large_scale_monitoring` fact |
| `lawful_basis_includes` | written by 1 obligation, read by nobody | read from the `lawful_bases` fact, and the basis named must be one of Article 6(1)'s six |
| `thresholds.employees_min` | read by `watcher_obligation_applies`, written by nobody | removed from the vocabulary and the evaluator |

For as long as that drift stood, Article 35's DPIA obligation reached every
controller rather than only high-risk processing, and Article 37's DPO duty
reached every controller rather than those doing large-scale monitoring. A DPIA
is expensive and a DPO is a hire.

### The two directions do not cost the same

Neither is acceptable, and knowing which way a mistake fails is what decides how
loudly to refuse it.

**An unevaluated threshold over-reports.** `watcher_obligation_applies` ignores
a condition it does not recognise, so an unread threshold narrows nothing and
the obligation binds more organisations than the curator wrote. It is visible to
the customer and they can dismiss it, which makes it survivable and not
harmless: a fabricated obligation is worse than no obligation, because the
product's value is that a human can check the claim.

**An unevaluated gap token is silent, and that is the one that matters.**
`watcher_gap_satisfied` returns `true` for a token it does not recognise, and
logs. Satisfied means no gap, and no gap means no finding. A pack whose
obligations require `access_review` would ingest cleanly, report as applying,
and produce nothing, for ever. The customer reads an empty feed as compliance.

In a compliance product that is the worst available failure. A regulation that
silently never fires is worse than one that is missing, because a missing
regulation is visible.

### So the rule is

- **Nothing is declared without being evaluated**, on either side. ENT-233 left
  thresholds a second tier, declared but unread, pinned by a test so the list
  could only grow deliberately. ENT-246 emptied it and removed the tier:
  `UnevaluatedKeys()` returns nothing, and a test asserts that.
- **The declaration is checked against the running evaluator.**
  `corpus_vocabulary_test.go` asks each gap token about a profile where the gap
  is genuinely open, and each fact-backed threshold about an organisation that
  has and has not answered it. A declaration that merely asserts what another
  file does is the arrangement that drifted in the first place.
- **An unknown token is refused at ingest**, naming the token and the vocabulary
  it is missing from, because whoever hits it is authoring a pack and needs to
  know whether to add data or add code.

Refusing at ingest rather than at watcher time is the same treatment a citation
that does not resolve already gets from `IngestService`. Both are claims the
system cannot honour, and the honest moment to say so is when the claim
arrives.

### Where a threshold's answer comes from

Three of them ask about the *processing* rather than about the *organisation*,
and the legacy `compliance_profiles` has a column for none of them. They are
answered from ENT-228's `org_profile_facts`:

| Threshold | Fact | Question |
|---|---|---|
| `high_risk_processing` | `high_risk_processing` | Is the processing likely to result in a high risk to people (Article 35(1))? |
| `high_risk_ai_system` | `high_risk_ai_system` | Is an AI system provided or deployed that falls in Annex III? |
| `large_scale_monitoring` | `large_scale_monitoring` | Are data subjects monitored regularly and systematically, on a large scale (Article 37(1)(b))? |

Adding those questions was not a migration, which is the property this document
exists to protect: the fact vocabulary lives in the proto enum and in
`domain/memory`, and `00023` adds no column and no constraint.

**High risk was two questions wearing one token.** GDPR Article 35 asks whether
the processing is likely to result in a high risk to people's rights; the AI Act
asks whether a system falls within Annex III. Different tests under different
regulations, and a shared answer would have meant a controller's answer about
profiling deciding whether Annex III bound it. A pack author writing a new
threshold should ask the same question of it: is this one condition, or two that
happen to share a word?

### An absent fact means the obligation does not apply

The direction is a product decision and it is written down here, in
`applieswhen.go` and in the migration, because it is the sort of thing that gets
quietly reversed.

Asserting a DPIA from silence is a fabricated obligation: nobody asked, so there
are no grounds, and "you owe a DPIA" is the most expensive sentence this product
can say wrongly. The mirror risk is real, an organisation that does do high-risk
processing no longer being told about Article 35, and it is one answer away from
being fixed: the fact is editable on the memory page today, and onboarding
writes it at ENT-212.

`unsure` counts as applying, and that is not a rounding of "no". ENT-228 kept
`unsure` as its own answer because "we asked and they did not know" is a
different claim from "they said no", and an organisation that does not know
whether its processing is high-risk has not done the Article 35(1) screening the
obligation exists for.

### These functions are decisions, not invariants

By the test in [`db/README.md`](../db/README.md): if a second process connected
tomorrow and did not know these rules, the data would not be wrong, it would
have made a different product decision about who an obligation binds. That is a
decision, and decisions belong in Go.

**ENT-225 owns moving them**, and neither ENT-233 nor ENT-246 did, because the
only caller is the plpgsql sweep and moving that wholesale inside a pack change
or a fix would bundle two changes. ENT-246 taught the existing evaluator to read
the three conditions rather than adding a second evaluator in Go that nothing
calls, on the grounds that two implementations of one rule, one of them
decorative, is what produced this bug in the first place.

What ENT-225 inherits:

- the vocabulary declaration, which is the list of what the evaluator has to
  implement, already written down, including which fact each threshold reads
- `corpus_vocabulary_test.go` and `watcher_applicability_test.go`, which assert
  the same properties and should keep asserting them against the Go evaluator,
  at which point the first stops needing a database
- no unevaluated tier to reproduce

## What a third regulation breaks today

Ordered by what it costs, not by where it sits. Everything here was found while
drawing the boundary; the ones ENT-233 fixed are marked.

### Fixed in ENT-233

1. **`corpuspack.All` hardcoded `[]string{GDPRFile, AIActFile}`.** Adding a
   regulation was a Go change and a release. Now a line in `packs.json`.
2. **The drift test hardcoded the same two files and a pack count of five.** Now
   derived from the manifest, so a third act is covered the moment it is listed.
3. **`appliesWhen` was unvalidated.** Now a closed vocabulary, refused at
   ingest, with the plpgsql evaluator checked against the declaration.

### Fixed in ENT-246

- **Three declared tokens were read by nobody, and one reader had no writer.**
  The applicability vocabulary now has no unevaluated tier, the three
  conditions are answered from `org_profile_facts`, and `employees_min` is
  gone. Article 35's DPIA no longer reaches every controller. Unnumbered
  because the list below continues ENT-233's numbering.

### Silent when it breaks, so worth fixing before a third act

4. **`analyst_regulation_abbrev`** (`00001_baseline.sql:133`) is the one true
   two-branch switch on regulation identity in the system:

   ```sql
   select case p_celex
     when '32016R0679' then 'GDPR'
     when '32024R1689' then 'EU AI Act'
     else p_celex
   end;
   ```

   Everything a customer reads as "GDPR Art. 30" is built from it. A third act
   not added here produces citation labels reading `32025R1234 Art. 12`. Not
   wrong, but it reads as a bug to the customer and it is the label on the
   evidence the whole product asks them to trust.

5. **`analyst_citation_url`** (`00001_baseline.sql:98`) matches only
   `^3(\d{4})R(\d{4})$`, which is an EU *regulation*. A directive, a decision,
   or anything non-EU yields NULL, so the "check this against the law" link
   silently disappears. SOC 2 has no CELEX number at all, which makes this the
   first place a genuinely non-EU pack stops working.

6. **`Citation.Target()`** (`internal/domain/corpus/corpus.go:59`) is a second,
   map-less label builder that emits raw CELEX. It is only used for error
   messages today, and the comments in `corpus_read.go` and `stored.go` are
   explicit that rendering labels in Go is the thing not to do. Worth keeping
   that way.

### Loud when it breaks, so cheaper to leave

7. **The four-value `action_type` CHECK** (`00001_baseline.sql:427`,
   `00007:68`) and the three executor triggers. A pack needing a fourth action
   fails at the constraint, immediately and visibly. This is the bounded
   migration, and it is correctly a migration.

8. **`compliance_profiles`** columns are a GDPR questionnaire in the schema:
   `has_ropa`, `has_dpo`, `transfers_outside_eu`, `ai_systems`. Every per-pack
   profile question would be a column. ENT-228's `org_profile_facts` is the
   replacement and the reason this is not being fixed here.

9. **`gdpr_articles`** (`00001_baseline.sql:606`, `ingest.proto:402`) is a
   regulation-named column for the generic question "which articles did this
   decision turn on". An enforcement decision under a third act has nowhere to
   go. Renaming it touches the proto, so it is noted rather than done. See the
   proposal at the end.

10. **`'gdpr-arts-12-22-data-subject-rights'`** is hardcoded in the two DSAR
    watcher detectors (`00002_organisations.sql:1222,1275`), as is the 30-day
    Article 12(3) clock in three places. These are GDPR-specific by nature
    rather than by accident, and they are fine where they are as long as the
    DSAR record type is understood as GDPR's.

11. **`edpb-guidelines.json`** is named for GDPR's supervisory body. The AI Act
    has the AI Office and the AI Board. The `guidelines` table is
    publisher-agnostic, so this is a file name and a manifest id, not a schema
    problem.

12. **Marketing copy** says "Two regulations" in about a dozen places
    (`app/layout.tsx`, `components/landing/*`). Deliberate and correct today.
    It is listed so that whoever adds the third act knows the copy is a
    findable set rather than a hunt.

## What ENT-233 deliberately did not change

- **The plpgsql evaluators.** ENT-225's, and bundling them here would make two
  changes reviewable only as one.
- **`compliance_profiles` and the record-type constraints.** The deep coupling,
  and ENT-228 is actively replacing the profile half.
- **`obligations.json` is still one file across both regulations.** Splitting it
  per pack is cheap to do and was not done, because pack membership is already
  `citation.celex` and a split would be churn without a third act to justify the
  shape. The manifest records this as a known seam.
- **`analyst_regulation_abbrev`.** Making it table-driven means the abbreviation
  becomes a column on `regulatory_documents`, which is a migration on the
  corpus and belongs with whatever work adds the third act's data.
- **`gdpr_articles`.** Renaming it to `cited_articles` is the right change and
  it touches `proto/kindlast/platform/v1/ingest.proto`, which was owned by
  another change in flight. The proposal:

  ```proto
  // was: repeated int32 gdpr_articles = 9;
  repeated int32 cited_articles = 9;
  ```

  plus the column rename in a migration, the six Go sites, and
  `enforcement-decisions.json`. Field number unchanged, so it is wire
  compatible; the JSON name changes, which matters only to `corpus-load` and
  the Postman collection, both in-repo.
