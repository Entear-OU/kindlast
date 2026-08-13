-- Dev/test fixtures for postgres-app (ENT-192). Runs as kindlast_migrator
-- via the seed job; idempotent by primary key.
--
-- Two organisations and three humans:
--   * Alpha Compliance GmbH: owner Ada, member Miko
--   * Beta Retail OU: owner Bob
-- Ada and Bob exercise the two-org isolation paths; Miko exercises the
-- member (non-owner) role. The uuids are fixed so the web client and the
-- future auth slice can reference them in dev.

insert into organisations (id, name) values
  ('a0000000-0000-4000-8000-000000000001', 'Alpha Compliance GmbH'),
  ('b0000000-0000-4000-8000-000000000001', 'Beta Retail OU')
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
