-- +goose Up
-- 00016_decisions_move_to_go.sql (ENT-225 phase 1)
--
-- Nine functions that decided things, dropped now that Go decides them.
--
-- The rule they were failing is in db/README.md and AGENTS.md: if it must hold
-- no matter who writes, it is a constraint; if it decides, it is Go. These came
-- across in 00001 and 00002 as a bridge from the Supabase era, where the
-- database was the only place to put logic, and most migrations since have
-- rewritten one of their bodies.
--
-- WHAT IS BEING DROPPED, AND WHAT IT DECIDED
--
--   approve_finding             which transition counts as a repeat
--   reject_finding              when three rejections make a product-review flag
--   snooze_finding              how far a deferral may be pushed
--   create_processing_activity  whether the plan's manual cap is reached
--   update_processing_activity  whether a change is worth an audit row
--   create_ai_system_manual     whether High-Risk needs a reviewed approval
--   update_ai_system            whether a reclassification needs one
--   log_dsar                    the Article 12(3) deadline, and refusing a
--                               future receipt date
--   mark_dsar_responded         whether a reviewed approval is required, and
--                               what counts as already answered
--
-- Each is now in `apps/core-api/internal/store/postgres`, in the transaction
-- the request already had, with the same GUCs set and therefore the same
-- policies applying. Nothing about who can see or write what has changed.
--
-- WHAT IS DELIBERATELY NOT DROPPED
--
-- `record_audit_log` stays, and the ENT-225 issue's own table is wrong to offer
-- dropping it as an option. It snapshots the actor's role at the time of the
-- action and appends a row that `audit_log_no_update` then freezes: an
-- invariant, not a decision. More concretely, the three Executor trigger
-- functions call it from inside the very UPDATE that Go now issues, so there is
-- no version of this change where it goes away. Go calls the same function, and
-- one regulatory record keeps one writer.
--
-- The three `executor_*_on_approval` triggers stay too. They are phase 2, gated
-- on Temporal at build-order step 8. They still fire on the Go UPDATE, exactly
-- as they fired on the function's.
--
-- TWO CONSEQUENCES WORTH WRITING DOWN RATHER THAN DISCOVERING
--
-- 1. The manual ROPA cap no longer binds anything except core-api.
--
-- `ropa_manual_activity_limit()` was called from inside
-- `create_processing_activity`, so the cap applied to any writer that used the
-- function. It is now a Go check in the only application that writes, and the
-- single-writer rule says there will not be another. That is a real narrowing
-- and it is deliberate rather than overlooked: somebody reading a dropped
-- trigger in a year should not have to guess whether an invariant was lost.
--
-- If a second writer ever appears, the cap is not an invariant that survives it
-- and would need re-expressing as one. It is a plan gate, so the honest place
-- for it would still be the application, and the second writer would need the
-- same check rather than the database growing it back.
--
-- 2. The two audit rows an approval writes are unordered, and always were.
--
-- Approving a finding whose action creates a record writes the Executor's
-- creation row (from the trigger, during the UPDATE) and the decision row.
-- `audit_log.occurred_at` defaults to `now()`, which is the transaction
-- timestamp, and both are written in one transaction, so they carry the
-- identical value. `order by occurred_at desc limit 1` is not a tiebreak
-- between them and never was.
--
-- Nothing depended on it: every lookup disambiguates by excluding rows whose
-- target is the finding itself. Recorded here because the design doc described
-- the order as an observed fact, and it is not observable.

drop function if exists public.approve_finding(uuid, boolean);
drop function if exists public.reject_finding(uuid, text);
drop function if exists public.snooze_finding(uuid, integer);

drop function if exists public.create_processing_activity(text, text, text, text[], text[], text);
drop function if exists public.update_processing_activity(uuid, text, text, text, text[], text[], text);
drop function if exists public.create_ai_system_manual(text, text, text, text, text, boolean);
drop function if exists public.update_ai_system(uuid, text, text, text, text, text, boolean);
drop function if exists public.log_dsar(text, text, text, timestamptz);
drop function if exists public.mark_dsar_responded(uuid, boolean);

-- The last reader of `app.billing_enabled`, so the GUC goes with it (00013).
--
-- That GUC was a correct fix in the wrong layer, and 00013's header said so at
-- the time: a database function needed a deployment fact it could not read, so
-- the fact was spelled into every transaction's session. With the cap decided
-- in Go the fact travels as a field on the transaction and nothing sets a third
-- GUC any more. `app.current_org_id` and `app.current_user_id` are unchanged and
-- remain the whole of tenancy.
drop function if exists public.ropa_manual_activity_limit();

-- +goose Down
--
-- Deliberately not a restoration.
--
-- Recreating nine function bodies here would mean maintaining a second copy of
-- rules that now live in Go, and the copy would be wrong the first time either
-- side changed. A rollback of this migration is a rollback of the application
-- that goes with it, and the pair belong to the same deployment.
--
-- Restore from the previous release's image and this migration's parent, or
-- from a backup. The tables and their data are untouched by this migration, so
-- there is nothing here to lose either way.

-- +goose StatementBegin
do $$
begin
  raise exception
    '00016 has no automatic down migration: the dropped functions live in Go '
    'as of ENT-225, and recreating them here would fork the rules. Roll back '
    'the application and the schema together.';
end
$$;
-- +goose StatementEnd
