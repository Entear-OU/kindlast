-- ENT-159: declare the table privileges the app's roles have always assumed.
--
-- Every table in `public` has RLS enabled and policies defined, but no migration
-- ever issued a GRANT. RLS filters rows *within* privileges a role already
-- holds — Postgres raises "permission denied for table" before a policy is ever
-- consulted. The result on a fresh stack: a user could sign up and log in, then
-- hit an error boundary on /onboarding and 5 other authed routes, and the
-- Watcher / Analyst / Executor pipeline had no access either.
--
-- Why this stayed hidden: hosted Supabase projects are bootstrapped with
--   alter default privileges in schema public grant all on tables
--     to anon, authenticated, service_role;
-- so every table created inherited grants invisibly. Current Supabase ships a
-- hardened default for `public` that grants only Dxtm (TRUNCATE / REFERENCES /
-- TRIGGER / MAINTAIN). The schema must declare what it needs instead of
-- inheriting it from whichever platform version created the project.
--
-- Privilege model: grants are permissive and **RLS is the sole gate**. That is
-- the contract the codebase is written and tested against — a denied read or
-- write comes back as a silent empty result (zero rows affected), not a 42501
-- error. See subscriptions.test.ts ("denies a user updating their own
-- subscription") and compliance-profile.test.ts ("denies anonymous reads"),
-- both of which assert `error` is null. Narrowing these grants to match each
-- table's policy set would turn those silent no-ops into raw Postgres errors
-- surfacing in the UI, so the grants intentionally mirror the hosted default.
--
-- Security rests on RLS, which is enabled on all 26 tables:
--   * corpus tables expose a public read policy by design (anon reads the
--     regulatory corpus);
--   * user-owned tables scope every command to `auth.uid() = user_id`;
--   * billing_webhook_events has RLS enabled and zero policies, so it stays
--     unreachable for anon and authenticated despite the blanket grant.

grant select, insert, update, delete
  on all tables in schema public
  to anon, authenticated, service_role;

-- Future tables must inherit the same contract, or this bug silently returns
-- the next time someone adds a table. tests/integration/authenticated-grants.ts
-- fails if a policied table ever ships without the grants its policies need.
alter default privileges in schema public
  grant select, insert, update, delete on tables
  to anon, authenticated, service_role;
