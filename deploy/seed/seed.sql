-- Dev/test fixtures for postgres-app (ENT-192). Runs as kindlast_migrator
-- via the seed job; idempotent by primary key.
--
-- Two organisations and three humans:
--   * Alpha Compliance GmbH: owner Ada, member Miko
--   * Beta Retail OU: owner Bob
-- Ada and Bob exercise the two-org isolation paths; Miko exercises the
-- member (non-owner) role. The uuids are fixed so the web client and the
-- future auth slice can reference them in dev.

-- The slug is derived here rather than written literally, through the same
-- org_slug() the ENT-198 backfill and runtime provisioning use, so the fixture
-- cannot drift from the rule it is meant to demonstrate. It is deterministic
-- from a fixed name, so these stay 'alpha-compliance-gmbh' and
-- 'beta-retail-ou' and are safe to reference from a test or a bookmark.
insert into organisations (id, name, slug) values
  ('a0000000-0000-4000-8000-000000000001', 'Alpha Compliance GmbH', org_slug('Alpha Compliance GmbH')),
  ('b0000000-0000-4000-8000-000000000001', 'Beta Retail OU',        org_slug('Beta Retail OU'))
on conflict (id) do nothing;

insert into memberships (org_id, user_id, role) values
  ('a0000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-0000000000aa', 'owner'),
  ('a0000000-0000-4000-8000-000000000001', 'a0000000-0000-4000-8000-0000000000ab', 'member'),
  ('b0000000-0000-4000-8000-000000000001', 'b0000000-0000-4000-8000-0000000000ba', 'owner')
on conflict (org_id, user_id) do nothing;

insert into subscriptions (org_id, plan, status) values
  ('a0000000-0000-4000-8000-000000000001', 'pro', 'active'),
  ('b0000000-0000-4000-8000-000000000001', 'free', 'active')
on conflict (org_id) do nothing;
