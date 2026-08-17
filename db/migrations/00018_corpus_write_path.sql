-- +goose Up
-- 00018_corpus_write_path.sql (ENT-207)
--
-- A role that can write the law, and reach no customer's data at all.
--
-- WHAT WAS ACTUALLY TRUE BEFORE THIS, WHICH IS NOT WHAT THE SCHEMA SAID
--
-- 00002 introduced the corpus policies under this comment:
--
--   "The regulatory corpus: shared reference data, public reads by design.
--    No write policies: ingestion runs as the migrator."
--
-- The first half is right. The second describes an intention rather than the
-- schema, and two things were off.
--
-- `kindlast_app` held `insert, update, delete` on every corpus table, inherited
-- from 00002's blanket grant. It could not actually use them, because FORCE ROW
-- LEVEL SECURITY with no write policy refuses an insert and matches zero rows
-- on an update. So the grants were dead weight that read as permission, which
-- is the worst state for a grant to be in: `information_schema` says the
-- request-handling role may write the law, and only a careful reading of the
-- policy set says otherwise. ENT-207 asks for this to be assertable from role
-- grants rather than from convention, and it was not.
--
-- And "ingestion runs as the migrator" is the thing §16.5 exists to prevent. A
-- migrator connection is `BYPASSRLS` and owns the schema; making it the
-- ingestion path means the process that writes the corpus can also rewrite any
-- tenant's findings, drop a policy, or truncate `audit_log`. It also puts the
-- write outside core-api, which is the single-writer rule (§16.5) that keeps
-- workers owning no database access.
--
-- WHY A SIXTH ROLE, AND WHY NOT ANY OF THE FIVE
--
-- Same reasoning as 00017's fifth, applied to a different asymmetry.
--
--   kindlast_app       serves browser requests. A role that answers a console
--                      request must not be able to edit the law that the
--                      product's findings cite. That is the whole trust story:
--                      a customer can check a claim against the regulation
--                      precisely because the request path cannot touch it.
--   kindlast_agent     can invent a finding. Give it corpus writes and it
--                      becomes a role that can invent a finding AND author the
--                      obligation the finding cites, which is a machine that
--                      can manufacture a citation end to end. That is the exact
--                      failure AGENTS.md opens by calling worse than nothing.
--   kindlast_billing   holds two tables and should keep holding two tables.
--   kindlast_migrator  bypasses RLS entirely; see above.
--
-- So `kindlast_ingest`: NOSUPERUSER, NOBYPASSRLS, owns nothing, and holds
-- grants on the corpus tables and NOTHING ELSE. It cannot read a finding, a
-- membership, an organisation or an audit row. If its credential leaks, the
-- blast radius is the public text of European law.
--
-- NO DELETE, ON PURPOSE
--
-- `insert` and `update` only. A row removed from a later corpus snapshot is
-- left in place, which 00001's seed header already committed to for
-- obligations, and the reason generalises: a finding cites an obligation, and
-- an obligation cites an article. Deleting either out from under a stored
-- finding turns a citation a customer could check into a dangling reference,
-- and it does so retroactively, to a record they may have already shown a
-- regulator.
--
-- Retiring an obligation is therefore a different operation from ingesting a
-- newer snapshot, and it needs its own decision about what happens to the
-- findings that cite it. It is not this migration.
--
-- THE CORPUS HAS NO org_id AND THAT IS NOT AN OVERSIGHT
--
-- It is the same law for every customer. ENT-192 carried ten public-read
-- policies for exactly this, and ENT-207 says in terms: do not add an `org_id`
-- to these tables to make them look like the rest of the schema. What is tenant
-- data is the finding that cites them. The write policies below are
-- unconditional for the same reason: there is no organisation to scope to, so
-- scoping would be theatre.
--
-- The isolation suite asserts both halves rather than trusting this comment:
-- that these tables carry no `org_id`, and that a caller in one organisation
-- reads the same corpus rows as a caller in another.

-- +goose StatementBegin
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'kindlast_ingest') then
    raise exception using
      message = 'role kindlast_ingest does not exist',
      detail  = 'The corpus write path needs a role that is neither the '
                'request handler nor the agent nor the migrator.',
      hint    = 'Recreate the stack so deploy/postgres/init/01-roles.sh runs, '
                'or create the role by hand with NOSUPERUSER NOBYPASSRLS '
                'before applying this migration.';
  end if;
end
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- 1. Take the corpus away from the request-handling role
------------------------------------------------------------------------------
-- Reads stay: `kindlast_app` serves the console, and the console shows the law.
-- Writes go, and with them the last reading of `information_schema` that says
-- a browser request could reach the regulation.

revoke insert, update, delete on public.regulatory_documents             from kindlast_app;
revoke insert, update, delete on public.regulatory_articles              from kindlast_app;
revoke insert, update, delete on public.regulatory_article_paragraphs    from kindlast_app;
revoke insert, update, delete on public.regulatory_article_recitals      from kindlast_app;
revoke insert, update, delete on public.regulatory_recitals              from kindlast_app;
revoke insert, update, delete on public.regulatory_annexes               from kindlast_app;
revoke insert, update, delete on public.regulatory_annex_items           from kindlast_app;
revoke insert, update, delete on public.regulatory_guidelines            from kindlast_app;
revoke insert, update, delete on public.regulatory_enforcement_decisions from kindlast_app;
revoke insert, update, delete on public.obligations                      from kindlast_app;

------------------------------------------------------------------------------
-- 2. Give it to a role that holds nothing else
------------------------------------------------------------------------------
-- `select` as well as the writes, because an idempotent upsert has to read what
-- is there: resolving a CELEX to a document id, and checking that an
-- obligation's citation names an article that exists are both reads.

grant select, insert, update on public.regulatory_documents             to kindlast_ingest;
grant select, insert, update on public.regulatory_articles              to kindlast_ingest;
grant select, insert, update on public.regulatory_article_paragraphs    to kindlast_ingest;
grant select, insert         on public.regulatory_article_recitals      to kindlast_ingest;
grant select, insert, update on public.regulatory_recitals              to kindlast_ingest;
grant select, insert, update on public.regulatory_annexes               to kindlast_ingest;
grant select, insert, update on public.regulatory_annex_items           to kindlast_ingest;
grant select, insert, update on public.regulatory_guidelines            to kindlast_ingest;
grant select, insert, update on public.regulatory_enforcement_decisions to kindlast_ingest;
grant select, insert, update on public.obligations                      to kindlast_ingest;

-- The junction gets no `update`: it has no columns to update. Its primary key
-- IS the pair, so a re-ingest is an insert that conflicts and does nothing.

------------------------------------------------------------------------------
-- 3. Policies, unconditional, because there is no tenant to scope to
------------------------------------------------------------------------------
-- A grant alone writes nothing here. Every table in `public` is FORCE ROW LEVEL
-- SECURITY, so a table with a grant and no matching policy refuses an insert
-- and matches zero rows on an update: exactly the state `kindlast_app` was left
-- in, and exactly the trap that caught `capability_tokens` and
-- `billing_webhook_events` in this same stack.

-- +goose StatementBegin
do $$
declare
  t text;
begin
  foreach t in array array[
    'regulatory_documents',
    'regulatory_articles',
    'regulatory_article_paragraphs',
    'regulatory_article_recitals',
    'regulatory_recitals',
    'regulatory_annexes',
    'regulatory_annex_items',
    'regulatory_guidelines',
    'regulatory_enforcement_decisions',
    'obligations'
  ]
  loop
    execute format(
      'create policy %I on public.%I for insert to kindlast_ingest with check (true)',
      t || '_ingest_insert', t);
  end loop;

  -- The junction has nothing to update.
  foreach t in array array[
    'regulatory_documents',
    'regulatory_articles',
    'regulatory_article_paragraphs',
    'regulatory_recitals',
    'regulatory_annexes',
    'regulatory_annex_items',
    'regulatory_guidelines',
    'regulatory_enforcement_decisions',
    'obligations'
  ]
  loop
    execute format(
      'create policy %I on public.%I for update to kindlast_ingest using (true) with check (true)',
      t || '_ingest_update', t);
  end loop;

  -- And the reads. The existing `_select_public` policies are `to public`, so
  -- kindlast_ingest already matches them; nothing to add.
end
$$;
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
do $$
declare
  t text;
begin
  foreach t in array array[
    'regulatory_documents',
    'regulatory_articles',
    'regulatory_article_paragraphs',
    'regulatory_article_recitals',
    'regulatory_recitals',
    'regulatory_annexes',
    'regulatory_annex_items',
    'regulatory_guidelines',
    'regulatory_enforcement_decisions',
    'obligations'
  ]
  loop
    execute format('drop policy if exists %I on public.%I', t || '_ingest_insert', t);
    execute format('drop policy if exists %I on public.%I', t || '_ingest_update', t);
  end loop;
end
$$;
-- +goose StatementEnd

-- +goose StatementBegin
do $$
begin
  if exists (select 1 from pg_roles where rolname = 'kindlast_ingest') then
    execute 'revoke all on public.regulatory_documents             from kindlast_ingest';
    execute 'revoke all on public.regulatory_articles              from kindlast_ingest';
    execute 'revoke all on public.regulatory_article_paragraphs    from kindlast_ingest';
    execute 'revoke all on public.regulatory_article_recitals      from kindlast_ingest';
    execute 'revoke all on public.regulatory_recitals              from kindlast_ingest';
    execute 'revoke all on public.regulatory_annexes               from kindlast_ingest';
    execute 'revoke all on public.regulatory_annex_items           from kindlast_ingest';
    execute 'revoke all on public.regulatory_guidelines            from kindlast_ingest';
    execute 'revoke all on public.regulatory_enforcement_decisions from kindlast_ingest';
    execute 'revoke all on public.obligations                      from kindlast_ingest';
  end if;
end
$$;
-- +goose StatementEnd

-- Restoring 00002's blanket grants to kindlast_app, so Down leaves the schema
-- as this migration found it rather than as somebody would prefer it.
grant insert, update, delete on public.regulatory_documents             to kindlast_app;
grant insert, update, delete on public.regulatory_articles              to kindlast_app;
grant insert, update, delete on public.regulatory_article_paragraphs    to kindlast_app;
grant insert, update, delete on public.regulatory_article_recitals      to kindlast_app;
grant insert, update, delete on public.regulatory_recitals              to kindlast_app;
grant insert, update, delete on public.regulatory_annexes               to kindlast_app;
grant insert, update, delete on public.regulatory_annex_items           to kindlast_app;
grant insert, update, delete on public.regulatory_guidelines            to kindlast_app;
grant insert, update, delete on public.regulatory_enforcement_decisions to kindlast_app;
grant insert, update, delete on public.obligations                      to kindlast_app;
