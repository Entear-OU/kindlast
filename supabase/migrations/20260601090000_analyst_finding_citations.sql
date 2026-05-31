-- The Analyst: precise obligation citation on every finding (ENT-59)
--
-- ENT-58 produced findings with a baseline `regulatory_obligation` (the
-- obligation title) and a nullable `obligation_id` — a signal whose anchor slug
-- wasn't a catalogue row could still convert. ENT-59 makes the citation
-- trustworthy and auditable: every finding cites the exact regulatory article it
-- maps to, with a non-null, delete-protected link to the obligation and a
-- resolvable source URL. "Users can trust the proposal and audit it later."
--
-- Citation is derived purely from the obligation's own citation fields — no
-- corpus join. The corpus is referenced by NATURAL KEY (CELEX + article/recital/
-- annex; see the obligations catalogue ENT-52), the EUR-Lex ELI URL is built
-- deterministically (the same scheme `lib/corpus/resolve.ts` fetches at runtime),
-- and `regulatory_documents` isn't required to be seeded. This keeps generation
-- deterministic and SQL-first, consistent with ENT-58.
--
-- Idempotent: `if not exists` / `or replace` / `drop … if exists` throughout.
-- ─────────────────────────────────────────────────────────────────────────────

-- 1. Schema: tighten the obligation link + add the source URL ──────────────────
--
-- obligation_id becomes NOT NULL and delete-protected. The conversion now
-- requires a resolvable obligation (an unresolvable signal is skipped, see §3),
-- so the column can carry the invariant. RESTRICT (not CASCADE) so deleting a
-- catalogue obligation that a live finding cites is rejected rather than
-- silently destroying the finding's audit anchor.

alter table public.findings
  add column if not exists citation_url text;

alter table public.findings
  drop constraint if exists findings_obligation_id_fkey;

alter table public.findings
  add constraint findings_obligation_id_fkey
    foreign key (obligation_id) references public.obligations(id) on delete restrict;

alter table public.findings
  alter column obligation_id set not null;

-- 2. Citation builders (pure functions over the obligation's citation fields) ──
--
-- Abbreviation map for the two regulations the product tracks. Falls back to the
-- CELEX itself for anything else so an un-mapped regulation still yields a label.
create or replace function public.analyst_regulation_abbrev(p_celex text)
returns text
language sql
immutable
set search_path = public, pg_temp
as $$
  select case p_celex
    when '32016R0679' then 'GDPR'
    when '32024R1689' then 'EU AI Act'
    else p_celex
  end;
$$;

-- Human-readable citation label, e.g. "GDPR Art. 30", "GDPR Art. 30(1)(b)",
-- "EU AI Act Annex III", "GDPR Recital 47". The paragraph label (e.g. "1(b)") is
-- rendered with each level parenthesised: `'(' || replace(p, '(', ')(')` turns
-- "1(b)" into "(1)(b)". The seeded catalogue is article-grain today, so the
-- paragraph branch is the edge case the format test pins down.
create or replace function public.analyst_citation_label(
  p_celex     text,
  p_kind      text,
  p_article   int,
  p_recital   int,
  p_annex     text,
  p_paragraph text
)
returns text
language plpgsql
immutable
set search_path = public, pg_temp
as $$
declare
  v_abbrev text := public.analyst_regulation_abbrev(p_celex);
begin
  case p_kind
    when 'article' then
      return v_abbrev || ' Art. ' || p_article
        || case when p_paragraph is not null
                then '(' || replace(p_paragraph, '(', ')(')
                else '' end;
    when 'recital' then
      return v_abbrev || ' Recital ' || p_recital;
    when 'annex' then
      return v_abbrev || ' Annex ' || p_annex
        || case when p_paragraph is not null then ' (' || p_paragraph || ')' else '' end;
    else
      return v_abbrev;
  end case;
end;
$$;

-- Resolvable EUR-Lex ELI anchor for the cited element. Mirrors the URL scheme in
-- `lib/corpus/resolve.ts` (citationKeyToUrl): regulation CELEX → ELI base, then a
-- deep-link anchor per element kind. The number is cast to int so leading zeros
-- are stripped ("0679" → 679), matching resolve.ts. Returns NULL for a CELEX
-- that isn't a recognised regulation identifier.
create or replace function public.analyst_citation_url(
  p_celex   text,
  p_kind    text,
  p_article int,
  p_recital int,
  p_annex   text
)
returns text
language plpgsql
immutable
set search_path = public, pg_temp
as $$
declare
  v_m    text[] := regexp_match(p_celex, '^3(\d{4})R(\d{4})$');
  v_base text;
begin
  if v_m is null then
    return null;
  end if;

  v_base := 'https://eur-lex.europa.eu/eli/reg/' || v_m[1] || '/' || (v_m[2])::int || '/oj';

  return v_base || case p_kind
    when 'article' then '#art_' || p_article
    when 'recital' then '#rct_' || p_recital
    when 'annex'   then '#anx_' || p_annex
    else ''
  end;
end;
$$;

-- 3. Conversion: cite precisely, require a resolvable obligation ───────────────
--
-- Re-declares the ENT-58 conversion. Changes: resolve-or-skip (the NOT NULL
-- obligation_id can't be met otherwise), `regulatory_obligation` now the precise
-- citation label, and the new `citation_url`. Everything else (detected /
-- severity / proposed_action baselines, status never reset on replay) is
-- unchanged — ENT-60 and ENT-61 own those fields.

create or replace function public.analyst_convert_signal(p_signal_id uuid)
returns uuid
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_sig    public.watcher_findings;
  v_obl    public.obligations;
  v_action text;
  v_id     uuid;
begin
  select * into v_sig from public.watcher_findings where id = p_signal_id;
  if not found then
    raise exception 'analyst_convert_signal: unknown signal %', p_signal_id;
  end if;

  if v_sig.obligation_slug is not null then
    select * into v_obl from public.obligations where slug = v_sig.obligation_slug;
  end if;

  -- ENT-59: a finding must cite a real obligation. A signal whose slug doesn't
  -- resolve cannot satisfy the NOT NULL link, so it is skipped (and logged)
  -- rather than producing an uncitable finding.
  if v_obl.id is null then
    raise log 'analyst_convert_signal: signal % has no resolvable obligation (slug %), skipping',
      p_signal_id, v_sig.obligation_slug;
    return null;
  end if;

  v_action := case v_sig.kind
    when 'deadline'          then 'Review this obligation and prepare to meet its upcoming deadline.'
    when 'profile_gap'       then 'Put the missing control in place to satisfy this obligation.'
    when 'dsar'              then 'Prepare and log a response to this data-subject request before its deadline.'
    when 'regulatory_update' then 'Review this regulatory update and assess its impact on your obligations.'
    else                          'Review this finding and take the appropriate action.'
  end;

  insert into public.findings (
    profile_id, user_id, watcher_finding_id, obligation_id, obligation_slug,
    detected, severity, proposed_action, regulatory_obligation, citation_url,
    supporting_context, effort_estimate, metadata
  )
  values (
    v_sig.profile_id,
    v_sig.user_id,
    v_sig.id,
    v_obl.id,
    v_sig.obligation_slug,
    v_sig.title,
    v_sig.severity,
    v_action,
    public.analyst_citation_label(
      v_obl.citation_celex, v_obl.citation_kind, v_obl.citation_article,
      v_obl.citation_recital, v_obl.citation_annex, v_obl.citation_paragraph
    ),
    public.analyst_citation_url(
      v_obl.citation_celex, v_obl.citation_kind, v_obl.citation_article,
      v_obl.citation_recital, v_obl.citation_annex
    ),
    v_obl.summary,
    'hours',
    jsonb_build_object(
      'signal_kind',      v_sig.kind,
      'signal_dedup_key', v_sig.dedup_key,
      'signal_metadata',  v_sig.metadata
    )
  )
  on conflict (watcher_finding_id) do update set
    obligation_id         = excluded.obligation_id,
    obligation_slug       = excluded.obligation_slug,
    detected              = excluded.detected,
    severity              = excluded.severity,
    proposed_action       = excluded.proposed_action,
    regulatory_obligation = excluded.regulatory_obligation,
    citation_url          = excluded.citation_url,
    supporting_context    = excluded.supporting_context,
    metadata              = excluded.metadata,
    updated_at            = now()
  returning id into v_id;

  return v_id;
end;
$$;

-- 4. run_analyst_for_profile(): count only signals that produced a finding ─────
--
-- A skipped (unresolvable) signal returns null from the conversion; it must not
-- inflate the converted count.

create or replace function public.run_analyst_for_profile(p_profile_id uuid)
returns integer
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  s record;
  n integer := 0;
begin
  for s in
    select id from public.watcher_findings
    where profile_id = p_profile_id
      and status = 'open'
    order by created_at, id
  loop
    if public.analyst_convert_signal(s.id) is not null then
      n := n + 1;
    end if;
  end loop;

  return n;
end;
$$;
