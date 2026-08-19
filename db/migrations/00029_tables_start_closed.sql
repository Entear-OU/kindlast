-- +goose Up
-- 00029_tables_start_closed.sql (ENT-243)
--
-- Narrows `kindlast_app` to the commands its policies admit, and stops new
-- tables arriving with DML already attached.
--
-- WHAT WAS ACTUALLY IN FORCE
--
-- 00002 did two things at the end of its grant section:
--
--   grant select, insert, update, delete on all tables in schema public
--     to kindlast_app;
--   alter default privileges in schema public
--     grant select, insert, update, delete on tables to kindlast_app;
--
-- The first covered every table that existed. The second covered every table
-- the migrator has created since. So a migration that writes
-- `grant select, insert on public.new_table to kindlast_app` is not narrowing
-- anything: the four commands are already there and the grant is additive.
-- Only an explicit revoke moves the boundary.
--
-- Nothing was broken by this. Every table has `FORCE ROW LEVEL SECURITY`, and
-- a command with no policy touches zero rows. The boundary held. It held one
-- layer thinner than the documentation said it did, and the layer that was
-- actually holding is the quiet one: a missing grant is a 42501 at parse time,
-- where a missing policy is silence, and silence reads the same as "there is
-- nothing here yet". 00015 wrote that reasoning down when it revoked on
-- `capability_tokens` instead of leaning on the absent policy, and 00017 did
-- the same for `billing_webhook_events`. This migration applies it to the rest.
--
-- WHY IT MATTERS WHEN NOTHING IS BROKEN
--
-- `db/README.md` says `audit_log` is append only because `kindlast_app` holds
-- no delete grant on it. That sentence is the load-bearing one behind the
-- promise that nobody, including us, can quietly make a decision disappear,
-- and it was false as written: the role held delete, and the policy was doing
-- the work the grant was being credited for. Somebody auditing that claim
-- would have found the grant present and concluded either that the document
-- cannot be trusted or that the property is broken, and neither was the truth.
--
-- The second reason is what happens next. If a delete policy is ever added to
-- `audit_log` for a plausible-looking reason, a retention feature or an
-- erasure path or a test fixture, the grant offers no backstop today because
-- it is already there. With it revoked, that change fails loudly in review.
--
-- THE RULING: TABLES START CLOSED
--
-- Decided by the architecture session on 2026-08-17. The default privilege is
-- why this recurs by construction: every new table inherits DML and each
-- migration has to remember to refuse it. 00015 remembered, 00014 did not.
-- The default is the wrong way round, so it goes. From here a table has to ask
-- for what it needs, and no application role has a default privilege at all.
--
-- Note that narrowing the default touches nothing that already exists, which
-- is the quiet half of this migration. The revokes below are a per-table
-- judgement about what each existing table should hold, and they cannot be
-- derived from the default change.
--
-- HOW EACH REVOKE WAS CHECKED
--
-- Every write path into these tables was traced to the role that runs it
-- before the command was taken away. In summary:
--
--   audit_log              Written by `record_audit_log`, which inserts and is
--                          security invoker, so `kindlast_app` keeps insert.
--                          Nothing updates or deletes: there is no retention
--                          job, and `audit_log_no_update` already refuses an
--                          update at the row level.
--   transactional_outbox   `Tenant.EnqueueMessage` inserts as the app. Every
--                          update is `AgentStore.deliverOne`, on the agent
--                          pool. Nothing deletes (ENT-242).
--   notification_outbox    Enqueued by the `enqueue_finding_notification`
--                          trigger, which fires after insert on `findings`
--                          and therefore only ever as the agent, since the app
--                          has no insert policy on `findings`. Marked sent,
--                          skipped or failed by `AgentStore`, on the agent
--                          pool.
--   findings               Inserted by `analyst_convert_signal`, reached only
--                          through `run_analyst()` on the agent pool. The app
--                          updates them (approve, reject, snooze, narrate) and
--                          keeps update. Nothing deletes.
--   watcher_findings       Inserted and upserted by `emit_watcher_finding`,
--                          reached only through `run_watcher()` on the agent
--                          pool. The app reads them and nothing more.
--   product_review_flags   Inserted by `Tenant.flagForProductReview` as the
--                          app, which keeps insert. Nothing updates, and
--                          `product_review_flags_no_update` already refuses.
--   subscriptions          Written only by `BillingStore`, on the billing
--                          pool, from the webhook. The app reads a plan and an
--                          entitlement and nothing else.
--   user_identities        Upserted by `org.go` on sign-in as the app, which
--                          keeps insert and update. Nothing deletes; identity
--                          rows go with the user by cascade.
--   notification_preferences  Upserted by `notifications.go` as the app.
--                          Nothing deletes.
--   deadline_alert_log     No writer exists. Neither Go nor plpgsql touches
--   weekly_briefing_log    either table. Both are 00001 leftovers with a
--                          select policy and no producer, so the app holds
--                          read and nothing else until one is built.
--   goose_db_version       goose's own bookkeeping, written by
--                          `kindlast_migrator`, which owns it. It is not
--                          domain data and the application has no business
--                          reading it. It was swept in by 00002's loop rather
--                          than by anybody's decision.
--
-- Three roles were audited and left alone, because every command each holds
-- already has a policy admitting it: `kindlast_billing`, `kindlast_ingest` and
-- `kindlast_vector_ro` (which holds nothing at all, by design, until the
-- chunk tables land). `kindlast_agent`'s blanket update on `findings` is left
-- in place deliberately, for the reason 00022 gave: `run_analyst()` and the
-- act-path functions also write that table, and whether they do so as caller
-- or as definer has to be established before the grant is narrowed. ENT-225
-- owns that audit.
--
-- `kindlast_migrator` is untouched throughout. It owns the schema and is
-- expected to hold everything.

------------------------------------------------------------------------------
-- 1. Tables start closed
------------------------------------------------------------------------------
-- Migrations run as `kindlast_migrator`, so this is the same default-privilege
-- entry 00002 created and this revoke empties it. `pg_default_acl` should hold
-- no row mentioning an application role afterwards.

alter default privileges in schema public
  revoke select, insert, update, delete on tables from kindlast_app;

------------------------------------------------------------------------------
-- 2. The accountability record, which is the one the documentation claims
------------------------------------------------------------------------------

revoke update, delete on public.audit_log from kindlast_app;

------------------------------------------------------------------------------
-- 3. The two queues, whose drains run as the agent
------------------------------------------------------------------------------

revoke update, delete on public.transactional_outbox from kindlast_app;
revoke insert, update, delete on public.notification_outbox from kindlast_app;

------------------------------------------------------------------------------
-- 4. What the agent produces and the customer only acts on
------------------------------------------------------------------------------
-- The app keeps update on `findings`, which is approve, reject, snooze and the
-- narrative columns. It never authors one.

revoke insert, delete on public.findings from kindlast_app;
revoke insert, update, delete on public.watcher_findings from kindlast_app;

------------------------------------------------------------------------------
-- 5. Append-only records the app writes but must not revise
------------------------------------------------------------------------------

revoke update, delete on public.product_review_flags from kindlast_app;

------------------------------------------------------------------------------
-- 6. Rows owned by another role or by nobody
------------------------------------------------------------------------------

revoke insert, update, delete on public.subscriptions from kindlast_app;
revoke delete on public.user_identities from kindlast_app;
revoke delete on public.notification_preferences from kindlast_app;
revoke insert, update, delete on public.deadline_alert_log from kindlast_app;
revoke insert, update, delete on public.weekly_briefing_log from kindlast_app;

------------------------------------------------------------------------------
-- 7. goose's bookkeeping, which is not part of the domain schema
------------------------------------------------------------------------------
-- `revoke all` rather than the four commands, so nothing survives on a table
-- the application should not be able to name. This is also what retires the
-- `ALLOWED` exception in `db/tests/addressable-but-empty.test.ts`: the table
-- needed an exception only because it was reachable and policy-free.

revoke all on public.goose_db_version from kindlast_app;

-- +goose Down

------------------------------------------------------------------------------
-- Restores exactly what 00002 left in place.
------------------------------------------------------------------------------
-- Every command here was held before this migration, so the down is faithful
-- rather than approximate. It is a real down and not a stub because the whole
-- claim of this migration is that a grant can be checked, and a rollback that
-- did not restore the grants would leave that unprovable.

grant select, insert, update, delete on public.audit_log to kindlast_app;
grant select, insert, update, delete on public.transactional_outbox to kindlast_app;
grant select, insert, update, delete on public.notification_outbox to kindlast_app;
grant select, insert, update, delete on public.findings to kindlast_app;
grant select, insert, update, delete on public.watcher_findings to kindlast_app;
grant select, insert, update, delete on public.product_review_flags to kindlast_app;
grant select, insert, update, delete on public.subscriptions to kindlast_app;
grant select, insert, update, delete on public.user_identities to kindlast_app;
grant select, insert, update, delete on public.notification_preferences to kindlast_app;
grant select, insert, update, delete on public.deadline_alert_log to kindlast_app;
grant select, insert, update, delete on public.weekly_briefing_log to kindlast_app;
grant select, insert, update, delete on public.goose_db_version to kindlast_app;

alter default privileges in schema public
  grant select, insert, update, delete on tables to kindlast_app;
