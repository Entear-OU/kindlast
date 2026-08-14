-- ENT-198: organisations.slug, the URL segment every console route hangs off.
--
-- The design settled this in §20.1 after the doc was caught routing on a slug
-- (§22.4) its own DDL never defined. Implemented as specified rather than
-- reinvented here:
--
--   * derived from the name, never chosen; there is no slug picker in the UI
--   * lowercase, runs of non-alphanumerics collapsed to one hyphen, 63 max
--   * collisions take a numeric suffix: acme, acme-2, acme-3
--   * IMMUTABLE in v1: renaming an organisation does not change its slug
--
-- Immutability is the constraint that shapes everything else in this file.
-- Slugs live in bookmarks and in emailed capability-token links, which are
-- exactly the links a compliance product has to keep working, so a slug minted
-- from the wrong name is not a cosmetic problem: it is permanent. Nothing here
-- recomputes a slug, and no trigger keeps it in step with the name. If editable
-- slugs are ever wanted, the mechanism is a redirect table reserving the old
-- one and issuing a 308, which is future work and deliberately absent.
--
-- +goose Up

------------------------------------------------------------------------------
-- The derivation, in one place
------------------------------------------------------------------------------
-- In SQL rather than in Go, because two callers need the identical rule: the
-- backfill below, and provisioning at runtime when a new organisation is
-- created. A second implementation in the service would be a rule that can
-- drift, and a slug minted under a drifted rule cannot be corrected later.
--
-- Pure: it reads no table, so it needs no SECURITY DEFINER and no RLS
-- consideration, unlike the three definer functions this schema already
-- carries.

-- +goose StatementBegin
create or replace function public.org_slug(p_name text, p_ordinal integer default 1)
returns text
language plpgsql
immutable
as $$
declare
  -- The first of a colliding set is unsuffixed, so `acme` then `acme-2`.
  v_suffix text := case when coalesce(p_ordinal, 1) > 1 then '-' || p_ordinal else '' end;
  v_room   integer := 63 - length(v_suffix);
  v_base   text;
begin
  v_base := lower(coalesce(p_name, ''));

  -- Letters that expand to two in ASCII, before anything strips accents.
  --
  -- Order matters and the German pairs are why. Folding an umlaut to its base
  -- letter gives `muller`, which is not how German writes it: the language's
  -- own ASCII convention is `mueller`, and a compliance product sold into the
  -- EU cannot mint a permanent URL that misspells a customer's name. The
  -- Nordic and Icelandic pairs follow the same principle in their own
  -- languages.
  v_base := replace(v_base, 'ä', 'ae');
  v_base := replace(v_base, 'ö', 'oe');
  v_base := replace(v_base, 'ü', 'ue');
  v_base := replace(v_base, 'ß', 'ss');
  v_base := replace(v_base, 'æ', 'ae');
  v_base := replace(v_base, 'œ', 'oe');
  v_base := replace(v_base, 'ø', 'oe');
  v_base := replace(v_base, 'å', 'aa');
  v_base := replace(v_base, 'þ', 'th');
  v_base := replace(v_base, 'ð', 'dh');

  -- Every other accented Latin letter to its base. An explicit table rather
  -- than the `unaccent` extension, and that is the load-bearing choice here:
  -- a slug is immutable, so a derivation that behaves differently depending on
  -- whether an optional extension happens to be installed would mint different
  -- permanent URLs on two deployments of the same product. Determinism matters
  -- more than coverage. The two operands are generated together and are the
  -- same length by construction; changing one without the other silently
  -- truncates the mapping.
  v_base := translate(
    v_base,
    'àáâãçèéêëìíîïñòóôõùúûýÿāăąćĉċčďēĕėęěĝğġģĥĩīĭįĵķĺļľńņňōŏőŕŗřśŝşšţťũūŭůűųŵŷźżžłđħŧı',
    'aaaaceeeeiiiinoooouuuyyaaaccccdeeeeegggghiiiijklllnnnooorrrssssttuuuuuuwyzzzldhti'
  );

  v_base := btrim(regexp_replace(v_base, '[^a-z0-9]+', '-', 'g'), '-');

  -- Truncate before the suffix is added, so the total still fits 63, and trim
  -- again in case the cut landed mid-hyphen and left a trailing one.
  if length(v_base) > v_room then
    v_base := btrim(left(v_base, v_room), '-');
  end if;

  -- A name with nothing alphanumeric in it still has to yield something
  -- routable: '///', but also a name written entirely in a script this rule
  -- does not transliterate, such as Greek or Cyrillic, which the step above
  -- leaves untouched and the collapse above then strips to nothing. The check
  -- constraint requires a leading alphanumeric, so without this such a name
  -- could not be stored at all. An organisation nobody can navigate to is
  -- worse than one with an ugly URL, and the collision loop below turns the
  -- second such name into org-2.
  if v_base = '' then
    v_base := left('org', v_room);
  end if;

  return v_base || v_suffix;
end;
$$;
-- +goose StatementEnd

comment on function public.org_slug(text, integer) is
  'Derives the URL slug for an organisation name. Shared by the ENT-198 backfill and by runtime provisioning so the rule cannot drift.';

------------------------------------------------------------------------------
-- The guard that has to run before a single slug is minted
------------------------------------------------------------------------------
-- Before PR #114, just-in-time provisioning named a personal organisation
-- after the owner's raw IdP subject, because a Zitadel access token carries
-- neither `name` nor `email` and the fallback chain reached its last resort
-- every time. Those names are snowflake integers such as 386250729179840515.
--
-- A slug derived from one is a permanent URL built out of an IdP identifier,
-- and immutability means there is no fixing it afterwards. So this refuses
-- rather than proceeding.
--
-- It cannot repair them itself, and that is the whole reason the repair lives
-- in the service instead: the correct name comes from the OIDC userinfo
-- endpoint, userinfo needs the caller's own access token, and a migration has
-- no caller. Worse, the affected rows have user_identities.email null by
-- construction, because the code that wrote them recorded an address the token
-- never carried. There is nothing in this database to derive a better name
-- from.
--
-- The remedy is the lazy rename PR #114 shipped: the next time an affected
-- person signs in, GetCurrentUser notices the organisation is named after
-- their subject claim, fetches userinfo once, and renames it. This migration
-- becomes applicable as soon as that has happened for everyone affected.
-- +goose StatementBegin
do $$
declare
  v_count integer;
begin
  select count(*) into v_count
  from public.organisations o
  join public.user_identities ui on ui.user_id = o.personal_owner_id
  where o.name = ui.subject;

  if v_count > 0 then
    raise exception
      '% personal organisation(s) are still named after their owner''s subject claim', v_count
      using
        detail = 'A slug is immutable once minted, so deriving one from an IdP subject would make that identifier a permanent URL.',
        hint   = 'Each affected owner must sign in once so the ENT-197 lazy rename can replace the name from userinfo, then re-run this migration. To find them: select o.id, o.name from organisations o join user_identities ui on ui.user_id = o.personal_owner_id where o.name = ui.subject;';
  end if;
end
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- The column and the backfill
------------------------------------------------------------------------------

alter table public.organisations add column slug text;

comment on column public.organisations.slug is
  'The URL segment for this organisation. Derived from the name at creation and never recomputed: bookmarks and emailed approval links depend on it.';

-- Row by row rather than one statement with a window function, and the reason
-- is a collision a window function gets wrong. Partitioning by the unsuffixed
-- base makes each partition's Nth row take `base-N`, which is fine until a
-- DIFFERENT name derives that exact string: an organisation called `Acme 2`
-- derives `acme-2`, which is also what the second `Acme` is about to be given.
-- They land in different partitions, neither knows about the other, and the
-- unique index rejects the whole statement.
--
-- So each row asks the same question the runtime path asks, against the slugs
-- that actually exist: what is the lowest ordinal still free. Rows not yet
-- reached hold null and cannot collide. That also makes the two mechanisms the
-- same mechanism, which is worth more than the saved statement.
-- +goose StatementBegin
do $$
declare
  r         record;
  v_ordinal integer;
  v_slug    text;
begin
  for r in select id, name from public.organisations order by created_at, id loop
    v_ordinal := 1;
    loop
      v_slug := public.org_slug(r.name, v_ordinal);
      exit when not exists (select 1 from public.organisations where slug = v_slug);
      v_ordinal := v_ordinal + 1;
    end loop;

    update public.organisations set slug = v_slug where id = r.id;
  end loop;
end
$$;
-- +goose StatementEnd

------------------------------------------------------------------------------
-- The shape, enforced by the database
------------------------------------------------------------------------------
-- The check is not redundant with the derivation. It is what stops a future
-- writer that skips org_slug from minting something unroutable, and since a
-- slug is immutable, the moment to refuse it is the only moment there is.
--
-- The pattern is a DNS-label shape: starts and ends alphanumeric, hyphens only
-- in between, 63 characters. Global uniqueness leaks nothing, because a slug
-- only resolves for a member; a stranger gets 404 rather than 403 (§20.1), so
-- the URL never confirms that an organisation exists.

alter table public.organisations alter column slug set not null;

alter table public.organisations
  add constraint organisations_slug_key unique (slug);

alter table public.organisations
  add constraint organisations_slug_shape
  check (slug ~ '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$');

-- The application resolves a slug from the URL on every authenticated request.
grant execute on function public.org_slug(text, integer) to kindlast_app;

-- +goose Down

alter table public.organisations drop constraint if exists organisations_slug_shape;
alter table public.organisations drop constraint if exists organisations_slug_key;
alter table public.organisations drop column if exists slug;
drop function if exists public.org_slug(text, integer);
