-- +goose Up
-- 00032_watcher_reads_dsars.sql
--
-- The Watcher could not complete a single sweep, because it cannot read
-- `dsars` and two of its three detectors do.
--
-- WHAT BREAKS, AND WHY IT WAS INVISIBLE
--
-- `run_watcher_for_profile` calls three detectors in order:
--
--   watcher_detect_deadlines        (00002)  reads public.dsars
--   watcher_detect_gaps             (00009)
--   watcher_detect_dsar_escalation  (00002)  reads public.dsars
--
-- All three are SECURITY INVOKER, so they run with the privileges of whoever
-- called `run_watcher()`. That caller is `kindlast_agent`, on the producer
-- pool `AgentStore` opens, and 00008 granted that role the set the Watcher was
-- believed to need: watcher_findings, findings, compliance_profiles, the
-- obligations and the corpus. `dsars` is not in the list. 00008's own comment
-- says the agent gets "no records", and a DSAR is a record, so the omission
-- reads as deliberate right up until you notice that two detectors written in
-- 00002, six migrations earlier, already select from it.
--
-- So every sweep fails on the first detector with
--
--   ERROR: permission denied for table dsars (SQLSTATE 42501)
--
-- and returns that to the caller as an internal error. Not a subset of
-- findings, not a degraded sweep: no findings at all, ever, for any
-- organisation.
--
-- It stayed invisible because nothing runs a sweep on a schedule. 00001
-- dropped the three pg_cron jobs when the schema left Supabase, and Temporal
-- does not arrive until build-order step 8, so `SweepService.RunSweep` is the
-- only thing that has ever called this and it has to be triggered by hand.
-- A console that has never swept shows the same empty feed as one whose sweeps
-- all failed, which is why "the Watcher has not run for this organisation" was
-- read as a state rather than as a symptom.
--
-- WHY SELECT, AND NOTHING ELSE
--
-- Both detectors only read: they count open requests, compare
-- `response_due_at` against now, and hand what they find to
-- `emit_watcher_finding`, which writes to `watcher_findings` where the agent
-- already holds insert. Nothing in the sweep writes a DSAR, and nothing should
-- want to. A role that can notice a missed deadline must not also be able to
-- alter the record of when the request arrived, because that record is the
-- evidence the deadline is measured against.
--
-- This narrows 00008's "no records" rather than abandoning it. The agent still
-- gets nothing on processing_activities, ai_systems, dsar_trail_entries,
-- organisations, memberships, audit_log or billing.
--
-- WHY A POLICY IS NEEDED TOO, AND NOT JUST A GRANT
--
-- `dsars` is FORCE ROW LEVEL SECURITY, and its only select policy is
-- `dsars_select_org`, which requires an org match AND a membership row for
-- `app.current_user_id`. `AgentStore.RunSweep` deliberately sets only
-- `app.current_org_id`: a sweep is started by the system, so there is no
-- member to name, and setting one would invent an actor.
--
-- Under that policy the agent would read zero rows even holding the grant, and
-- the failure would be worse than the current one rather than better: a sweep
-- that completes, reports success, and silently finds no DSAR deadline for any
-- customer. The grant alone is the version of this fix that looks right and is
-- wrong.
--
-- So the policy takes the shape 00008 gave every other agent-visible tenant
-- table (`watcher_findings_agent`, `findings_agent`,
-- `compliance_profiles_agent`): scoped to `kindlast_agent`, org equality
-- against the one GUC a sweep sets, no membership term. Select only, so it has
-- no `with check`.

grant select on public.dsars to kindlast_agent;

create policy dsars_agent_read on public.dsars
  for select to kindlast_agent
  using (org_id = (select current_setting('app.current_org_id')::uuid));

-- +goose Down

drop policy if exists dsars_agent_read on public.dsars;
revoke select on public.dsars from kindlast_agent;
