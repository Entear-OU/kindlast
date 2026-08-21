-- +goose Up
-- 00034_snooze_expiry_runs_as_the_agent.sql (ENT-256, part two)
--
-- `expire_snoozed_findings()` becomes the first thing Temporal runs on a
-- schedule, and for that it has to be callable by the producer role across
-- every organisation at once. It was not.
--
-- WHAT THE FUNCTION DOES, AND WHAT IT DOES NOT DECIDE
--
-- A person defers a finding for N days. When those days are up the finding
-- comes back to "needs a decision": status from `snoozed` to `pending`,
-- `snoozed_until` cleared. That is the whole function (00002), and it is the
-- job `pg_cron` ran daily at 06:10 until 00001 dropped the jobs with the
-- Supabase schema. Nothing has run it since. Every finding anybody has
-- deferred on this schema is still deferred.
--
-- ENT-225's test for where a rule lives is "if it must hold no matter who
-- writes, it is a constraint; if it decides, it is Go". This one is a timer
-- elapsing. The decision, how long to defer and whether to, was a human's and
-- was made at snooze time and written to the audit log then; what happens
-- when the date arrives has exactly one correct answer, and moving that answer
-- into Go would not make it a better decision, only a second implementation
-- of a three-line update. So the function stays, unchanged in body, and this
-- migration changes only who may call it and how.
--
-- WHY SECURITY DEFINER, AND WHY IT IS THE EIGHTH AND NOT A SHORTCUT
--
-- The rule in db/README.md is that a definer function exists only where RLS
-- structurally cannot express the check, and that adding one means writing
-- down why none of the seven already covers you. So:
--
-- Every policy on `findings` is scoped by `app.current_org_id`. That is
-- correct for every caller that exists today: a person acts inside one
-- organisation, and the producer sweeps one organisation per request because
-- `RunSweep` names it in a header and sets the GUC. Snooze expiry is neither.
-- It is a maintenance pass over every organisation's deferred findings at once,
-- started by a schedule with no organisation and no person, and there is no
-- GUC value that means "all of them": setting one would be inventing a tenant,
-- and iterating them would need a read of `organisations`, which the agent
-- role deliberately does not hold (00008: "no organisations, no memberships").
--
-- That is the same shape as `redeem_capability_token` and
-- `resolve_act_delegation`: the caller is not inside any tenancy context, and
-- a policy has nothing to read. And as with `accept_invitation`, the
-- alternative, a policy permissive enough for the agent to see every
-- organisation's findings, would grant far more than the one update this
-- needs.
--
-- What the function may touch is still bounded, by its own body rather than by
-- a policy: one UPDATE, on `findings`, filtered to `status = 'snoozed'` with a
-- `snoozed_until` in the past. It reads nothing else and writes nothing else,
-- and a definer function that does one fixed thing is a narrower grant than a
-- policy that lets a role do anything to matching rows.
--
-- WHO MAY CALL IT: THE AGENT, AND ONLY THE AGENT
--
-- A definer function that PUBLIC may execute is a definer function anyone may
-- execute, and that is what 00002 left: no grant, which for a function means
-- every role including `kindlast_app`. That was harmless while nothing called
-- it and nothing could, since as an invoker function every caller was still
-- behind its own policies. It is not harmless once the function bypasses them,
-- so PUBLIC loses it and the producer role alone gets it. The application role
-- cannot expire a snooze, the same way it cannot create a finding: the thing
-- that serves requests does not get to bring a deferred decision back early.
--
-- SEARCH_PATH IS ALREADY PINNED. 00002 set `search_path = public, pg_temp`
-- on the function, which is the setting that makes a definer function safe to
-- have at all, and `alter function ... security definer` keeps it.

alter function public.expire_snoozed_findings() security definer;

revoke all on function public.expire_snoozed_findings() from public;
grant execute on function public.expire_snoozed_findings() to kindlast_agent;

-- +goose Down

revoke all on function public.expire_snoozed_findings() from kindlast_agent;
alter function public.expire_snoozed_findings() security invoker;
-- 00002 left it at the default, which is PUBLIC execute.
grant execute on function public.expire_snoozed_findings() to public;
