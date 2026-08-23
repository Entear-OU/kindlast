-- +goose Up
-- 00037_producer_policies_name_their_tenant.sql (ENT-272)
--
-- Eight of the producer role's policies are `using (true)`. This makes them
-- org equality, in the form the rest of the schema already uses, and changes
-- two Go call sites that were relying on the open form.
--
-- WHY THEY WERE OPEN, WHICH WAS A DECISION AND NOT DRIFT
--
-- 00025 says it plainly, and cites 00019 and 00020 as the precedent it was
-- following:
--
--   Unconditional policies, matching `agent_runs` in 00019 and `org_evidence`
--   in 00020: the agent runs for organisations nobody is signed in to, so it
--   has no tenancy GUCs to be checked against. What keeps that honest is that
--   the role reaches almost nothing else.
--
-- Both halves of that were true when it was written. The first half has since
-- stopped being true, and the second half has stopped being enough.
--
-- IT HAS NO TENANCY GUCs: NOT TRUE ANY MORE, AND NOT TRUE PER ROLE EITHER
--
-- The producer sets `app.current_org_id` on most of what it does now:
-- `RunSweep`, `WatcherContextFor`, `RaiseSignal`, `IngestEvidence`,
-- `RunAnalyst` and `FindingsAwaitingNarrative` all open a transaction and
-- `set_config` the organisation they were handed before they touch a row.
-- `IngestEvidence` is the sharpest case, because it does that on
-- `org_evidence` and `integration_fetches`, two of the tables listed below:
-- the insert policy on each already reads the GUC, in the same statement whose
-- select policy says there is no GUC to read.
--
-- So "the agent has no tenancy GUCs" was never a property of the ROLE. It is a
-- property of some of its paths, and the ones it is true of are the relays:
-- `sweep_targets`, `PendingExecutorJobs`, `PendingMessageIDs`,
-- `expire_snoozed_findings`. Those pick the next row from every tenant at once
-- and there is no GUC value meaning "all of them", which is exactly why their
-- policies stay open here and why 00034 gave the snooze sweep a definer
-- function instead. What changes is only the paths that already know whose
-- data they are asking for.
--
-- THE ROLE REACHES ALMOST NOTHING ELSE: STILL TRUE, STILL NOT ENOUGH
--
-- It is a good argument about blast radius and a bad one about tenancy. The
-- property the two-GUC form exists for is not "an attacker cannot reach this
-- table", it is "a bug that points the producer at the wrong organisation, or
-- at none, touches no rows instead of every tenant's". A narrow grant does not
-- give you that, and the first code to read these tables on the producer pool
-- proved it: `WatcherContextFor` shipped two selects with no org predicate,
-- and the Watcher's context carried another organisation's connections and
-- another organisation's profile facts. A test caught it (ENT-258, PR #242).
-- Nothing in the schema would have.
--
-- WHY THE ONE-ARGUMENT `current_setting`, WHICH ERRORS RATHER THAN EMPTIES
--
-- `current_setting('app.current_org_id')` raises `42704` when the GUC is unset.
-- The two-argument form returns null, which makes the comparison null, which
-- reads zero rows quietly. Both are safe. This uses the one-argument form for
-- two reasons: it is what every other agent select policy already uses
-- (`compliance_profiles_agent`, `findings_agent`, `watcher_findings_agent`,
-- `dsars_agent_read`), so the producer's reads now have one spelling rather
-- than two; and a producer path that forgot to say whose data it wants is a
-- bug, and 00029's argument applies unchanged, that the loud failure on the
-- first call beats the quiet one that looks like an empty result until
-- somebody notices a feed that never fills.
--
-- The insert policies below are left on the two-argument form they were
-- written with. They already hold the boundary, a missing GUC already refuses
-- the write, and rewriting a policy that is doing its job to match a spelling
-- is churn with a migration attached.
--
-- WHAT IS DELIBERATELY NOT TOUCHED
--
-- Enumerating all 26 producer policies, the open ones divide three ways and
-- only one of the three is a problem:
--
--   The corpus. `obligations`, `regulatory_documents`, `regulatory_articles`,
--   `regulatory_article_paragraphs`, `regulatory_recitals`. These are the law.
--   They carry no `org_id` and there is nothing to scope them to.
--
--   The relays. `transactional_outbox`, `sweep_triggers`, `executor_jobs`,
--   `capability_tokens`, and `notification_outbox`'s dispatch pair. Each one
--   lists across every tenant to find the next row to work on, and each one
--   YIELDS the org id that the work then runs under. Scoping these to one
--   organisation would stop them functioning, and there is no organisation to
--   scope them to at the moment they run.
--
--   The eight below, every one of which has an `org_id` column and a caller
--   that already knows which organisation it means.

alter policy org_profile_facts_agent on public.org_profile_facts
  using (org_id = (select current_setting('app.current_org_id')::uuid));

alter policy integrations_agent on public.integrations
  using (org_id = (select current_setting('app.current_org_id')::uuid));

alter policy integration_tools_agent on public.integration_tools
  using (org_id = (select current_setting('app.current_org_id')::uuid));

alter policy integration_fetches_agent on public.integration_fetches
  using (org_id = (select current_setting('app.current_org_id')::uuid));

-- Nothing in Go reads this on the producer pool today. The grant was issued in
-- 00025 for a reader that has not been written, which makes this the cheapest
-- of the eight to get right and the easiest to get wrong later: a first reader
-- arriving against an open policy is how the `WatcherContextFor` leak happened.
alter policy audit_evidence_agent on public.audit_evidence
  using (org_id = (select current_setting('app.current_org_id')::uuid));

alter policy org_evidence_agent on public.org_evidence
  using (org_id = (select current_setting('app.current_org_id')::uuid));

alter policy org_model_config_agent on public.org_model_config
  using (org_id = (select current_setting('app.current_org_id')::uuid));

-- `agent_runs` is the widest of the eight: `for all`, not `for select`, so the
-- open form let the producer read, update and delete any organisation's run
-- record. The producer only ever inserts (`RecordAgentRun`); the console reads
-- these on the app pool, under `agent_runs_select_org`, which has been org and
-- membership equality since 00019.
--
-- `with check` moves with `using` here rather than being left open, because an
-- insert policy that admits any org_id is the same hole facing the other way:
-- a run recorded against a tenant that did not ask for it is a false line in
-- the record a customer reads to understand what ran on their data.
alter policy agent_runs_agent on public.agent_runs
  using      (org_id = (select current_setting('app.current_org_id')::uuid))
  with check (org_id = (select current_setting('app.current_org_id')::uuid));

-- +goose Down
alter policy org_profile_facts_agent   on public.org_profile_facts   using (true);
alter policy integrations_agent        on public.integrations        using (true);
alter policy integration_tools_agent   on public.integration_tools   using (true);
alter policy integration_fetches_agent on public.integration_fetches using (true);
alter policy audit_evidence_agent      on public.audit_evidence      using (true);
alter policy org_evidence_agent        on public.org_evidence        using (true);
alter policy org_model_config_agent    on public.org_model_config    using (true);
alter policy agent_runs_agent          on public.agent_runs          using (true) with check (true);
