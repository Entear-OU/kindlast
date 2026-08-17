-- +goose Up
-- 00011_records_keyset_indexes.sql (ENT-200, the records read surface)
--
-- Give the three registers the indexes the orderings RecordsService paginates by
-- actually need. Two are missing, on two different tables, for two different
-- reasons.
--
-- WHAT IS ACTUALLY WRONG TODAY
--
-- 00002 added an org index to all three record tables, but not the same one:
--
--   processing_activities_org_idx   (org_id, created_at DESC)
--   dsars_org_idx                   (org_id, created_at DESC)
--   ai_systems_org_idx              (org_id)
--
-- Nothing has noticed, because nothing lists AI systems yet. RecordsService
-- does, and it paginates by keyset over `(created_at, id)` for the reason every
-- other list in this API does: an offset page 2 silently skips a row whenever
-- one is inserted between requests, and in this product a skipped row is a
-- system that is on the register and never appeared on screen.
--
-- With only `(org_id)` to work from, that query is still correct and quietly
-- expensive: Postgres reads every row for the tenant and sorts it, on every
-- page, so the cost of page one grows with the size of the register rather than
-- with the size of the page. That is the sort of thing that is invisible at
-- fixture scale and arrives all at once for the customer with the most systems,
-- which is also the customer who can least afford it to.
--
-- WHY THE PLAIN ORG INDEX IS DROPPED
--
-- It becomes a strict prefix of the composite, so every lookup it served is
-- served by the new one. Keeping both costs a second write on every insert and
-- update for no read it can answer alone.
--
-- The drop is safe in the sense that matters here: it is not a uniqueness
-- constraint and no policy or foreign key depends on it. `pg_class` is asserted
-- over by the isolation suite, which is what will notice if that stops being
-- true.
--
-- NOT A TENANCY CHANGE
--
-- No policy, grant or column is touched. `ai_systems` keeps FORCE ROW LEVEL
-- SECURITY and its four two-GUC policies from 00001 and 00002 exactly as they
-- are; an index is not a security boundary and this migration must not read as
-- though it moved one.

create index if not exists ai_systems_org_created_idx
  on public.ai_systems (org_id, created_at desc);

drop index if exists public.ai_systems_org_idx;

-- THE SECOND GAP, WHICH IS NOT THE ONE THIS MIGRATION WAS OPENED FOR
--
-- `dsars` is listed by `response_due_at` ascending rather than by creation,
-- because the only question anyone asks of a DSAR log is which one runs out
-- first. It has no index for that ordering either.
--
-- What it does have is `dsars_due_idx`, which looks like the right index and is
-- not:
--
--   dsars_due_idx  (response_due_at) WHERE status IN ('open', 'in_progress')
--
-- It does not lead with `org_id`, so it cannot serve a per-tenant ordered scan,
-- and it is partial on the two unfinished statuses, so it excludes exactly the
-- answered requests an auditor asks to see. It is the right index for the
-- cross-tenant deadline sweep the Watcher runs, which is why it is kept rather
-- than replaced: the two queries are different shapes and want different
-- indexes.
--
-- Found by the test in db/tests/records-keyset-index.test.ts, which was written
-- against the ai_systems gap and reported this one too.

create index if not exists dsars_org_due_idx
  on public.dsars (org_id, response_due_at);

-- +goose Down

drop index if exists public.dsars_org_due_idx;

create index if not exists ai_systems_org_idx
  on public.ai_systems (org_id);

drop index if exists public.ai_systems_org_created_idx;
