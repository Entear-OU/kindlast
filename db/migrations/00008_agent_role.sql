-- +goose Up
-- 00008_agent_role.sql (ENT-203)
--
-- The producer gets a role of its own, so the sweeps can actually run.
--
-- WHY THIS IS NEEDED, MEASURED
--
-- core-api connects as kindlast_app. That role has select and update on
-- `findings` and select only on `watcher_findings`; neither table has an insert
-- policy, deliberately. So the Watcher and the Analyst cannot run as the
-- application, and a sweep endpoint served on the app pool fails at runtime:
--
--   ERROR:  new row violates row-level security policy for table "watcher_findings"
--
-- That is not a gap to be patched by granting the application insert. It is
-- 00002's design working: the thing that serves requests should not be able to
-- fabricate findings, because a finding is a claim about a customer's legal
-- exposure and the request path is the part exposed to the world.
--
-- 00002's header named the answer already: these functions run on "a
-- maintenance connection (kindlast_migrator or a future system role), never as
-- kindlast_app". This is that role.
--
-- WHY ITS POLICIES OMIT THE MEMBERSHIP CHECK, AND WHY THAT IS STILL TENANCY
--
-- Every policy in 00002 is an org equality plus a membership `exists`. The
-- second half cannot apply here: a sweep is started by the system, not by a
-- person, so there is no `app.current_user_id` to find in `memberships`.
-- Requiring one would mean inventing a user to attribute the Watcher to, and an
-- audit trail naming a human who did not act is worse than one naming nobody.
--
-- So the agent's policies keep the org equality and drop the membership check.
-- Tenancy still binds: the sweep can only touch the organisation its GUC names,
-- and pointing it at one organisation cannot read or write another. What it
-- loses is the defence against a caller naming an organisation they do not
-- belong to, which is meaningless for a role that belongs to none.
--
-- The policies are `to kindlast_agent`, so this reasoning applies to that role
-- and no other. kindlast_app's policies are untouched, and it still cannot
-- insert a finding.
--
-- WHY THE ROLE IS NOT CREATED HERE
--
-- kindlast_migrator is NOCREATEROLE by design, so a migration cannot create a
-- login role and this one must not be the exception that makes it CREATEROLE.
-- The role is created by deploy/postgres/init/01-roles.sh, as the superuser,
-- before anything else touches the database.
--
-- That means an existing deployment needs the role created by an operator
-- before this migration runs. It fails loudly below rather than skipping,
-- because a migration that quietly does nothing would leave the sweeps broken
-- in a way that only shows up as an empty feed.
--
--   create role kindlast_agent login password '...'
--     nosuperuser nocreatedb nocreaterole noinherit nobypassrls;
--   grant connect on database kindlast to kindlast_agent;
--   grant usage on schema public to kindlast_agent;
--
-- A local stack gets it from a `docker compose down -v` and back up.

-- +goose StatementBegin
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'kindlast_agent') then
    raise exception
      'kindlast_agent does not exist. Create it first (see the header of %), '
      'or recreate the stack with `docker compose -f deploy/compose.yaml down -v`.',
      '00008_agent_role.sql';
  end if;
end
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- Grants: narrow, and narrower than kindlast_app's
------------------------------------------------------------------------------
--
-- The producer writes signals and findings, and stamps watcher_last_run_at on
-- the profile it swept. It reads the corpus and the obligations to do so. It
-- gets nothing else: no organisations, no memberships, no audit_log, no
-- records, no billing. A role that can invent a finding should not also be able
-- to approve one.

grant select, insert, update on public.watcher_findings to kindlast_agent;
grant select, insert, update on public.findings           to kindlast_agent;
grant select, update          on public.compliance_profiles to kindlast_agent;
grant select on public.obligations               to kindlast_agent;
grant select on public.regulatory_documents      to kindlast_agent;
grant select on public.regulatory_articles       to kindlast_agent;
grant select on public.regulatory_recitals       to kindlast_agent;
grant select on public.regulatory_article_paragraphs to kindlast_agent;

-- The notification enqueue trigger fires on every finding insert, so the role
-- that inserts findings must be able to write the outbox row or the insert
-- fails. Insert only: the agent enqueues, and something else delivers.
grant select, insert on public.notification_outbox to kindlast_agent;

------------------------------------------------------------------------------
-- Policies: org equality, no membership check, agent only
------------------------------------------------------------------------------

create policy watcher_findings_agent on public.watcher_findings
  to kindlast_agent
  using      (org_id = (select current_setting('app.current_org_id')::uuid))
  with check (org_id = (select current_setting('app.current_org_id')::uuid));

create policy findings_agent on public.findings
  to kindlast_agent
  using      (org_id = (select current_setting('app.current_org_id')::uuid))
  with check (org_id = (select current_setting('app.current_org_id')::uuid));

create policy compliance_profiles_agent on public.compliance_profiles
  to kindlast_agent
  using      (org_id = (select current_setting('app.current_org_id')::uuid))
  with check (org_id = (select current_setting('app.current_org_id')::uuid));

create policy notification_outbox_agent on public.notification_outbox
  to kindlast_agent
  using      (org_id = (select current_setting('app.current_org_id')::uuid))
  with check (org_id = (select current_setting('app.current_org_id')::uuid));

-- The corpus and the obligation set are not tenant data: they are the law, the
-- same rows for every customer. They carry no org_id, so their policies are
-- unconditional reads rather than org-scoped ones. Being explicit here beats
-- leaving the agent unable to resolve a citation for reasons nobody can find.
create policy obligations_agent_read on public.obligations
  for select to kindlast_agent using (true);
create policy regulatory_documents_agent_read on public.regulatory_documents
  for select to kindlast_agent using (true);
create policy regulatory_articles_agent_read on public.regulatory_articles
  for select to kindlast_agent using (true);
create policy regulatory_recitals_agent_read on public.regulatory_recitals
  for select to kindlast_agent using (true);
create policy regulatory_article_paragraphs_agent_read on public.regulatory_article_paragraphs
  for select to kindlast_agent using (true);

-- +goose Down

drop policy if exists regulatory_article_paragraphs_agent_read on public.regulatory_article_paragraphs;
drop policy if exists regulatory_recitals_agent_read on public.regulatory_recitals;
drop policy if exists regulatory_articles_agent_read on public.regulatory_articles;
drop policy if exists regulatory_documents_agent_read on public.regulatory_documents;
drop policy if exists obligations_agent_read on public.obligations;
drop policy if exists notification_outbox_agent on public.notification_outbox;
drop policy if exists compliance_profiles_agent on public.compliance_profiles;
drop policy if exists findings_agent on public.findings;
drop policy if exists watcher_findings_agent on public.watcher_findings;

-- +goose StatementBegin
do $$
begin
  if exists (select 1 from pg_roles where rolname = 'kindlast_agent') then
    revoke all on public.notification_outbox from kindlast_agent;
    revoke all on public.regulatory_article_paragraphs from kindlast_agent;
    revoke all on public.regulatory_recitals from kindlast_agent;
    revoke all on public.regulatory_articles from kindlast_agent;
    revoke all on public.regulatory_documents from kindlast_agent;
    revoke all on public.obligations from kindlast_agent;
    revoke all on public.compliance_profiles from kindlast_agent;
    revoke all on public.findings from kindlast_agent;
    revoke all on public.watcher_findings from kindlast_agent;
  end if;
end
$$;
-- +goose StatementEnd
