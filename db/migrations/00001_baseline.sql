-- +goose Up
-- 00001_baseline.sql (ENT-192)
--
-- The 38 Supabase migrations squashed into one auth-free baseline. This file
-- is the legacy schema exactly as it stood, with Supabase removed:
--
--   * every `references auth.users(id)` is gone; user ids are IdP subjects
--     (plain uuids) and the identity provider owns the users themselves
--   * no policy from the old stack survives into this file: all row level
--     security policies are (re)written in 00002_organisations.sql in the
--     two-GUC organisation form, and reviewed there policy by policy
--   * no business function or business trigger lands here either, for the
--     same reason: everything touching tenancy is ported in 00002
--   * pg_cron and its three jobs (watcher-daily, analyst-daily,
--     snooze-expiry-daily) are dropped: schedules move to Temporal at
--     build-order step 8, and nothing in between runs them
--
-- The split between 00001 and 00002 is the production-import seam: migrate
-- to 00001, restore a data-only dump from the old stack, then let 00002
-- stamp every row with its organisation. A fresh deploy just runs both.
-- Because a restore happens against THIS schema, 00001 deliberately carries
-- no AFTER INSERT triggers: importing findings must not re-run the Executor
-- or enqueue notifications.

-- The vector extension is not trusted, so a non-superuser cannot create it;
-- deploy/postgres/init/01-roles.sh installs it (and pgcrypto) into template1
-- before this database exists. Assert rather than assume.
-- +goose StatementBegin
do $$
begin
  if not exists (select 1 from pg_extension where extname = 'vector') then
    raise exception 'pgvector is not installed in this database. The '
      'deploy/postgres/init/01-roles.sh init script installs it into '
      'template1; a database created before that ran must be recreated.';
  end if;
  if not exists (select 1 from pg_extension where extname = 'pgcrypto') then
    create extension pgcrypto;
  end if;
end
$$;
-- +goose StatementEnd


COMMENT ON SCHEMA public IS 'standard public schema';

CREATE TYPE public.effort_level AS ENUM (
    'minutes',
    'hours',
    'days'
);

CREATE TYPE public.email_frequency AS ENUM (
    'immediate',
    'daily',
    'weekly',
    'off'
);

CREATE TYPE public.severity_level AS ENUM (
    'low',
    'medium',
    'high',
    'critical'
);

-- +goose StatementBegin
CREATE FUNCTION public.analyst_citation_label(p_celex text, p_kind text, p_article integer, p_recital integer, p_annex text, p_paragraph text) RETURNS text
    LANGUAGE plpgsql IMMUTABLE
    SET search_path TO 'public', 'pg_temp'
    AS $$
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
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.analyst_citation_url(p_celex text, p_kind text, p_article integer, p_recital integer, p_annex text) RETURNS text
    LANGUAGE plpgsql IMMUTABLE
    SET search_path TO 'public', 'pg_temp'
    AS $_$
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
$_$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.analyst_effort(p_kind text) RETURNS public.effort_level
    LANGUAGE sql IMMUTABLE
    SET search_path TO 'public', 'pg_temp'
    AS $$
  select (case p_kind
    when 'dsar'              then 'hours'
    when 'deadline'          then 'days'
    when 'profile_gap'       then 'days'
    when 'regulatory_update' then 'hours'
    else                          'hours'
  end)::public.effort_level;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.analyst_regulation_abbrev(p_celex text) RETURNS text
    LANGUAGE sql IMMUTABLE
    SET search_path TO 'public', 'pg_temp'
    AS $$
  select case p_celex
    when '32016R0679' then 'GDPR'
    when '32024R1689' then 'EU AI Act'
    else p_celex
  end;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.analyst_severity(p_baseline text, p_signal_severity text, p_kind text, p_days_remaining integer, p_data_categories text[]) RETURNS public.severity_level
    LANGUAGE plpgsql IMMUTABLE
    SET search_path TO 'public', 'pg_temp'
    AS $$
declare
  v_level     int;
  v_sig_level int;
  v_sensitive text[] := array[
    'health', 'medical', 'biometric', 'genetic', 'financial', 'bank', 'payment',
    'children', 'child', 'racial', 'ethnic', 'religious', 'sexual', 'criminal',
    'political'
  ];
  c text;
begin
  v_level := case lower(coalesce(p_baseline, ''))
    when 'low' then 1 when 'medium' then 2 when 'high' then 3 when 'critical' then 4
    else 2 end;

  -- proximity to deadline (deadline / DSAR signals carry days_remaining)
  if p_days_remaining is not null then
    if p_days_remaining < 3 then
      v_level := v_level + 2;
    elsif p_days_remaining < 7 then
      v_level := v_level + 1;
    end if;
  end if;

  -- data sensitivity: any captured category matching a special-category marker
  if p_data_categories is not null then
    foreach c in array p_data_categories loop
      if exists (select 1 from unnest(v_sensitive) s where lower(c) like '%' || s || '%') then
        v_level := v_level + 1;
        exit;
      end if;
    end loop;
  end if;

  -- recency: a freshly-effective regulatory change is more urgent
  if p_kind = 'regulatory_update' then
    v_level := v_level + 1;
  end if;

  v_level := greatest(1, least(4, v_level));

  -- never downgrade below the signal's own (possibly escalated) severity
  v_sig_level := case lower(coalesce(p_signal_severity, ''))
    when 'low' then 1 when 'medium' then 2 when 'high' then 3 when 'critical' then 4
    else 0 end;
  v_level := greatest(v_level, v_sig_level);

  return (array['low', 'medium', 'high', 'critical'])[v_level]::public.severity_level;
end;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.audit_log_forbid_update() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
  raise exception 'audit_log is append-only: UPDATE on row % is not permitted', old.id
    using errcode = 'check_violation';
end;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.jsonb_text_array(p jsonb) RETURNS text[]
    LANGUAGE sql IMMUTABLE
    AS $$
  select case
    when jsonb_typeof(p) = 'array' then array(select jsonb_array_elements_text(p))
    else '{}'::text[]
  end;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.product_review_flags_forbid_update() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
  raise exception 'product_review_flags is append-only: UPDATE on row % is not permitted', old.id
    using errcode = 'check_violation';
end;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
begin
  new.updated_at = now();
  return new;
end;
$$;
-- +goose StatementEnd

CREATE TABLE public.compliance_profiles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    session_id uuid NOT NULL,
    user_id uuid NOT NULL,
    industry text NOT NULL,
    eu_jurisdictions text[] DEFAULT '{}'::text[] NOT NULL,
    data_categories text[] DEFAULT '{}'::text[] NOT NULL,
    data_subjects text[] DEFAULT '{}'::text[] NOT NULL,
    ai_systems text[] DEFAULT '{}'::text[] NOT NULL,
    has_dpo text NOT NULL,
    has_ropa text NOT NULL,
    transfers_outside_eu text NOT NULL,
    transfer_destinations text[] DEFAULT '{}'::text[] NOT NULL,
    vendor_list text DEFAULT ''::text NOT NULL,
    staff_count integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    watcher_last_run_at timestamp with time zone,
    CONSTRAINT compliance_profiles_has_dpo_check CHECK ((has_dpo = ANY (ARRAY['yes'::text, 'no'::text, 'unsure'::text]))),
    CONSTRAINT compliance_profiles_has_ropa_check CHECK ((has_ropa = ANY (ARRAY['yes'::text, 'no'::text, 'unsure'::text]))),
    CONSTRAINT compliance_profiles_transfers_outside_eu_check CHECK ((transfers_outside_eu = ANY (ARRAY['yes'::text, 'no'::text, 'unsure'::text])))
);

-- +goose StatementBegin
CREATE FUNCTION public.watcher_gap_satisfied(p_token text, p_profile public.compliance_profiles) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE
    SET search_path TO 'public', 'pg_temp'
    AS $$
begin
  case p_token
    when 'ropa' then
      return p_profile.has_ropa = 'yes';
    when 'dpo' then
      return p_profile.has_dpo = 'yes';
    when 'ai_register' then
      -- Satisfied only when the org operates no AI systems. Using AI with no
      -- register entry is the gap; there is no dedicated register field yet, so
      -- "operates any AI system" stands in until one lands (ENT-56 owns the
      -- re-map). NULL/empty array ⇒ no AI ⇒ satisfied.
      return coalesce(array_length(p_profile.ai_systems, 1), 0) = 0;
    when 'transfer_safeguards' then
      -- Satisfied when at least one transfer destination is documented. Pairs
      -- with the cross_border_transfers applicability gate so this only matters
      -- for orgs that actually transfer outside the EU.
      return coalesce(array_length(p_profile.transfer_destinations, 1), 0) > 0;
    else
      -- Unknown token: ignore (treat as satisfied) so an unrecognised rule
      -- never raises a gap. Logged for visibility during corpus authoring.
      raise log 'watcher_gap_satisfied: unknown requires token %', p_token;
      return true;
  end case;
end;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.watcher_obligation_applies(p_applies_when jsonb, p_profile public.compliance_profiles) RETURNS boolean
    LANGUAGE plpgsql IMMUTABLE
    SET search_path TO 'public', 'pg_temp'
    AS $$
declare
  v_role  text  := p_applies_when ->> 'role';
  v_min   int   := (p_applies_when #>> '{thresholds,employees_min}')::int;
begin
  -- role
  if v_role in ('deployer', 'provider') then
    if coalesce(array_length(p_profile.ai_systems, 1), 0) = 0 then
      return false;
    end if;
  end if;
  -- 'controller' (and absent role) impose no role restriction.

  -- cross-border transfers
  if coalesce((p_applies_when #>> '{thresholds,cross_border_transfers}')::boolean, false) then
    if p_profile.transfers_outside_eu is distinct from 'yes' then
      return false;
    end if;
  end if;

  -- employee threshold (NULL staff_count is treated as "unknown ⇒ applicable")
  if v_min is not null and p_profile.staff_count is not null
     and p_profile.staff_count < v_min then
    return false;
  end if;

  -- engages a processor
  if coalesce((p_applies_when ->> 'engages_processor')::boolean, false) then
    if coalesce(btrim(p_profile.vendor_list), '') = '' then
      return false;
    end if;
  end if;

  return true;
end;
$$;
-- +goose StatementEnd

CREATE TABLE public.ai_systems (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    profile_id uuid NOT NULL,
    user_id uuid NOT NULL,
    finding_id uuid,
    name text NOT NULL,
    vendor text,
    purpose text,
    risk_classification text DEFAULT 'unclassified'::text NOT NULL,
    documentation_status text DEFAULT 'missing'::text NOT NULL,
    last_reviewed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ai_systems_documentation_status_check CHECK ((documentation_status = ANY (ARRAY['missing'::text, 'in_progress'::text, 'complete'::text]))),
    CONSTRAINT ai_systems_risk_classification_check CHECK ((risk_classification = ANY (ARRAY['unacceptable'::text, 'high'::text, 'limited'::text, 'minimal'::text, 'unclassified'::text])))
);

CREATE TABLE public.audit_log (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    finding_id uuid,
    action_type text NOT NULL,
    target_table text NOT NULL,
    target_id uuid,
    before jsonb,
    after jsonb,
    approving_user_id uuid NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT audit_log_action_type_check CHECK ((length(btrim(action_type)) > 0)),
    CONSTRAINT audit_log_target_table_check CHECK ((length(btrim(target_table)) > 0))
);

CREATE TABLE public.billing_webhook_events (
    event_id text NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.deadline_alert_log (
    finding_id uuid NOT NULL,
    threshold integer NOT NULL,
    user_id uuid NOT NULL,
    sent_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT deadline_alert_log_threshold_check CHECK ((threshold = ANY (ARRAY[1, 7, 14, 30])))
);

CREATE TABLE public.dsars (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    subject_name text,
    request_type text,
    status text DEFAULT 'open'::text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    response_due_at timestamp with time zone NOT NULL,
    responded_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    handler text,
    finding_id uuid,
    CONSTRAINT dsars_status_check CHECK ((status = ANY (ARRAY['open'::text, 'in_progress'::text, 'responded'::text, 'closed'::text])))
);

CREATE TABLE public.findings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    profile_id uuid NOT NULL,
    user_id uuid NOT NULL,
    watcher_finding_id uuid NOT NULL,
    obligation_id uuid NOT NULL,
    obligation_slug text,
    detected text NOT NULL,
    severity public.severity_level DEFAULT 'medium'::public.severity_level NOT NULL,
    proposed_action text NOT NULL,
    regulatory_obligation text,
    supporting_context text,
    effort_estimate public.effort_level DEFAULT 'hours'::public.effort_level NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    citation_url text,
    narrative_generated_at timestamp with time zone,
    action_type text DEFAULT 'review'::text NOT NULL,
    approved_by uuid,
    approval_reviewed boolean DEFAULT false NOT NULL,
    rejection_reason text,
    snoozed_until timestamp with time zone,
    CONSTRAINT findings_action_type_check CHECK ((action_type = ANY (ARRAY['review'::text, 'create_ropa'::text, 'create_dsar'::text, 'create_ai_system'::text]))),
    CONSTRAINT findings_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'snoozed'::text])))
);

CREATE TABLE public.notification_outbox (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    finding_id uuid NOT NULL,
    user_id uuid NOT NULL,
    channel text DEFAULT 'email'::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    sent_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT notification_outbox_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'sent'::text, 'skipped'::text, 'failed'::text])))
);

CREATE TABLE public.notification_preferences (
    user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    timezone text DEFAULT 'Europe/Tallinn'::text NOT NULL,
    weekly_briefing_enabled boolean DEFAULT true NOT NULL,
    email text,
    min_severity_for_email public.severity_level DEFAULT 'medium'::public.severity_level NOT NULL,
    deadline_alerts_enabled boolean DEFAULT true NOT NULL,
    quiet_hours_start time without time zone,
    quiet_hours_end time without time zone
);

CREATE TABLE public.obligations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    slug text NOT NULL,
    title text NOT NULL,
    summary text NOT NULL,
    citation_celex text NOT NULL,
    citation_kind text NOT NULL,
    citation_article integer,
    citation_recital integer,
    citation_annex text,
    citation_paragraph text,
    applies_when jsonb DEFAULT '{}'::jsonb NOT NULL,
    severity text DEFAULT 'medium'::text NOT NULL,
    due_within_days integer,
    recurrence text,
    effective_date date,
    topic_tags text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT obligations_citation_kind_check CHECK ((citation_kind = ANY (ARRAY['article'::text, 'recital'::text, 'annex'::text]))),
    CONSTRAINT obligations_citation_matches_kind CHECK ((((citation_kind = 'article'::text) AND (citation_article IS NOT NULL) AND (citation_recital IS NULL) AND (citation_annex IS NULL)) OR ((citation_kind = 'recital'::text) AND (citation_recital IS NOT NULL) AND (citation_article IS NULL) AND (citation_annex IS NULL)) OR ((citation_kind = 'annex'::text) AND (citation_annex IS NOT NULL) AND (citation_article IS NULL) AND (citation_recital IS NULL)))),
    CONSTRAINT obligations_due_within_days_nonneg CHECK (((due_within_days IS NULL) OR (due_within_days >= 0))),
    CONSTRAINT obligations_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text]))),
    CONSTRAINT obligations_summary_length CHECK (((char_length(summary) >= 100) AND (char_length(summary) <= 2000)))
);

CREATE TABLE public.onboarding_messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    session_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    ordering integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT onboarding_messages_role_check CHECK ((role = ANY (ARRAY['user'::text, 'assistant'::text])))
);

CREATE TABLE public.onboarding_sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    status text DEFAULT 'in_progress'::text NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT onboarding_sessions_status_check CHECK ((status = ANY (ARRAY['in_progress'::text, 'completed'::text, 'abandoned'::text])))
);

CREATE TABLE public.processing_activities (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    profile_id uuid NOT NULL,
    user_id uuid NOT NULL,
    finding_id uuid,
    name text NOT NULL,
    purpose text,
    legal_basis text,
    data_categories text[] DEFAULT '{}'::text[] NOT NULL,
    recipients text[] DEFAULT '{}'::text[] NOT NULL,
    retention_period text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.product_review_flags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    profile_id uuid NOT NULL,
    obligation_slug text NOT NULL,
    finding_id uuid,
    rejection_count integer NOT NULL,
    reasons text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.regulatory_annex_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    annex_id uuid NOT NULL,
    item_label text NOT NULL,
    heading text,
    summary text NOT NULL,
    effective_date date,
    ordering integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT regulatory_annex_items_summary_length CHECK (((char_length(summary) >= 100) AND (char_length(summary) <= 2000)))
);

CREATE TABLE public.regulatory_annexes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    document_id uuid NOT NULL,
    annex_label text NOT NULL,
    heading text NOT NULL,
    summary text NOT NULL,
    effective_date date,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT regulatory_annexes_summary_length CHECK (((char_length(summary) >= 100) AND (char_length(summary) <= 2000)))
);

CREATE TABLE public.regulatory_article_paragraphs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    article_id uuid NOT NULL,
    paragraph_label text NOT NULL,
    ordering integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    summary text NOT NULL,
    CONSTRAINT regulatory_article_paragraphs_summary_length CHECK (((char_length(summary) >= 1) AND (char_length(summary) <= 2000)))
);

CREATE TABLE public.regulatory_article_recitals (
    article_id uuid NOT NULL,
    recital_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.regulatory_articles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    document_id uuid NOT NULL,
    article_number integer NOT NULL,
    heading text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    effective_date date,
    summary text NOT NULL,
    CONSTRAINT regulatory_articles_summary_length CHECK (((char_length(summary) >= 1) AND (char_length(summary) <= 2000)))
);

CREATE TABLE public.regulatory_documents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    celex_number text NOT NULL,
    title text NOT NULL,
    short_title text NOT NULL,
    version_date date NOT NULL,
    official_url text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.regulatory_enforcement_decisions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    slug text NOT NULL,
    dpa text NOT NULL,
    title text NOT NULL,
    decision_date date NOT NULL,
    fine_eur bigint,
    summary text NOT NULL,
    source_url text NOT NULL,
    gdpr_articles integer[] DEFAULT '{}'::integer[] NOT NULL,
    topic_tags text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT regulatory_enforcement_decisions_summary_length CHECK (((char_length(summary) >= 100) AND (char_length(summary) <= 2000)))
);

CREATE TABLE public.regulatory_guidelines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    slug text NOT NULL,
    publisher text DEFAULT 'EDPB'::text NOT NULL,
    title text NOT NULL,
    adopted_date date NOT NULL,
    version text,
    source_url text NOT NULL,
    topic_tags text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.regulatory_recitals (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    document_id uuid NOT NULL,
    recital_number integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    summary text NOT NULL,
    CONSTRAINT regulatory_recitals_summary_length CHECK (((char_length(summary) >= 1) AND (char_length(summary) <= 2000)))
);

CREATE TABLE public.subscriptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    plan text DEFAULT 'free'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    provider text,
    provider_customer_id text,
    current_period_end timestamp with time zone,
    CONSTRAINT subscriptions_plan_check CHECK ((plan = ANY (ARRAY['free'::text, 'pro'::text]))),
    CONSTRAINT subscriptions_status_check CHECK ((status = ANY (ARRAY['active'::text, 'past_due'::text, 'canceled'::text])))
);

CREATE TABLE public.watcher_findings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    profile_id uuid NOT NULL,
    user_id uuid NOT NULL,
    kind text NOT NULL,
    obligation_slug text,
    severity text DEFAULT 'medium'::text NOT NULL,
    title text NOT NULL,
    detail text,
    status text DEFAULT 'open'::text NOT NULL,
    dedup_key text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    resolved_at timestamp with time zone,
    CONSTRAINT watcher_findings_kind_check CHECK ((kind = ANY (ARRAY['deadline'::text, 'profile_gap'::text, 'dsar'::text, 'regulatory_update'::text]))),
    CONSTRAINT watcher_findings_severity_check CHECK ((severity = ANY (ARRAY['low'::text, 'medium'::text, 'high'::text, 'critical'::text]))),
    CONSTRAINT watcher_findings_status_check CHECK ((status = ANY (ARRAY['open'::text, 'resolved'::text, 'dismissed'::text])))
);

CREATE TABLE public.weekly_briefing_log (
    user_id uuid NOT NULL,
    period_start date NOT NULL,
    sent_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.ai_systems
    ADD CONSTRAINT ai_systems_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.billing_webhook_events
    ADD CONSTRAINT billing_webhook_events_pkey PRIMARY KEY (event_id);

ALTER TABLE ONLY public.compliance_profiles
    ADD CONSTRAINT compliance_profiles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.compliance_profiles
    ADD CONSTRAINT compliance_profiles_session_id_key UNIQUE (session_id);

ALTER TABLE ONLY public.deadline_alert_log
    ADD CONSTRAINT deadline_alert_log_pkey PRIMARY KEY (finding_id, threshold);

ALTER TABLE ONLY public.dsars
    ADD CONSTRAINT dsars_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.findings
    ADD CONSTRAINT findings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.notification_outbox
    ADD CONSTRAINT notification_outbox_finding_id_key UNIQUE (finding_id);

ALTER TABLE ONLY public.notification_outbox
    ADD CONSTRAINT notification_outbox_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.notification_preferences
    ADD CONSTRAINT notification_preferences_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY public.obligations
    ADD CONSTRAINT obligations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.obligations
    ADD CONSTRAINT obligations_slug_key UNIQUE (slug);

ALTER TABLE ONLY public.onboarding_messages
    ADD CONSTRAINT onboarding_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.onboarding_messages
    ADD CONSTRAINT onboarding_messages_session_id_ordering_key UNIQUE (session_id, ordering);

ALTER TABLE ONLY public.onboarding_sessions
    ADD CONSTRAINT onboarding_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.processing_activities
    ADD CONSTRAINT processing_activities_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.product_review_flags
    ADD CONSTRAINT product_review_flags_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.product_review_flags
    ADD CONSTRAINT product_review_flags_profile_id_obligation_slug_key UNIQUE (profile_id, obligation_slug);

ALTER TABLE ONLY public.regulatory_annex_items
    ADD CONSTRAINT regulatory_annex_items_annex_id_item_label_key UNIQUE (annex_id, item_label);

ALTER TABLE ONLY public.regulatory_annex_items
    ADD CONSTRAINT regulatory_annex_items_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.regulatory_annexes
    ADD CONSTRAINT regulatory_annexes_document_id_annex_label_key UNIQUE (document_id, annex_label);

ALTER TABLE ONLY public.regulatory_annexes
    ADD CONSTRAINT regulatory_annexes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.regulatory_article_paragraphs
    ADD CONSTRAINT regulatory_article_paragraphs_article_id_paragraph_label_key UNIQUE (article_id, paragraph_label);

ALTER TABLE ONLY public.regulatory_article_paragraphs
    ADD CONSTRAINT regulatory_article_paragraphs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.regulatory_article_recitals
    ADD CONSTRAINT regulatory_article_recitals_pkey PRIMARY KEY (article_id, recital_id);

ALTER TABLE ONLY public.regulatory_articles
    ADD CONSTRAINT regulatory_articles_document_id_article_number_key UNIQUE (document_id, article_number);

ALTER TABLE ONLY public.regulatory_articles
    ADD CONSTRAINT regulatory_articles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.regulatory_documents
    ADD CONSTRAINT regulatory_documents_celex_number_key UNIQUE (celex_number);

ALTER TABLE ONLY public.regulatory_documents
    ADD CONSTRAINT regulatory_documents_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.regulatory_enforcement_decisions
    ADD CONSTRAINT regulatory_enforcement_decisions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.regulatory_enforcement_decisions
    ADD CONSTRAINT regulatory_enforcement_decisions_slug_key UNIQUE (slug);

ALTER TABLE ONLY public.regulatory_guidelines
    ADD CONSTRAINT regulatory_guidelines_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.regulatory_guidelines
    ADD CONSTRAINT regulatory_guidelines_slug_key UNIQUE (slug);

ALTER TABLE ONLY public.regulatory_recitals
    ADD CONSTRAINT regulatory_recitals_document_id_recital_number_key UNIQUE (document_id, recital_number);

ALTER TABLE ONLY public.regulatory_recitals
    ADD CONSTRAINT regulatory_recitals_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_user_id_key UNIQUE (user_id);

ALTER TABLE ONLY public.watcher_findings
    ADD CONSTRAINT watcher_findings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.weekly_briefing_log
    ADD CONSTRAINT weekly_briefing_log_pkey PRIMARY KEY (user_id, period_start);

CREATE UNIQUE INDEX ai_systems_finding_idx ON public.ai_systems USING btree (finding_id) WHERE (finding_id IS NOT NULL);

CREATE INDEX ai_systems_profile_idx ON public.ai_systems USING btree (profile_id);

CREATE INDEX audit_log_finding_idx ON public.audit_log USING btree (finding_id) WHERE (finding_id IS NOT NULL);

CREATE INDEX audit_log_user_recent_idx ON public.audit_log USING btree (user_id, occurred_at DESC);

CREATE INDEX compliance_profiles_user_idx ON public.compliance_profiles USING btree (user_id);

CREATE INDEX deadline_alert_log_user_idx ON public.deadline_alert_log USING btree (user_id);

CREATE INDEX dsars_due_idx ON public.dsars USING btree (response_due_at) WHERE (status = ANY (ARRAY['open'::text, 'in_progress'::text]));

CREATE UNIQUE INDEX dsars_finding_idx ON public.dsars USING btree (finding_id) WHERE (finding_id IS NOT NULL);

CREATE INDEX dsars_user_idx ON public.dsars USING btree (user_id);

CREATE INDEX findings_profile_status_idx ON public.findings USING btree (profile_id, status);

CREATE INDEX findings_snoozed_until_idx ON public.findings USING btree (snoozed_until) WHERE (status = 'snoozed'::text);

CREATE INDEX findings_user_idx ON public.findings USING btree (user_id);

CREATE UNIQUE INDEX findings_watcher_finding_idx ON public.findings USING btree (watcher_finding_id);

CREATE INDEX notification_outbox_status_created_idx ON public.notification_outbox USING btree (status, created_at);

CREATE INDEX obligations_applies_when_idx ON public.obligations USING gin (applies_when);

CREATE INDEX obligations_celex_idx ON public.obligations USING btree (citation_celex, citation_kind);

CREATE INDEX obligations_topic_tags_idx ON public.obligations USING gin (topic_tags);

CREATE INDEX onboarding_sessions_user_started_idx ON public.onboarding_sessions USING btree (user_id, started_at DESC);

CREATE UNIQUE INDEX processing_activities_finding_idx ON public.processing_activities USING btree (finding_id) WHERE (finding_id IS NOT NULL);

CREATE INDEX processing_activities_profile_idx ON public.processing_activities USING btree (profile_id);

CREATE INDEX regulatory_annex_items_annex_idx ON public.regulatory_annex_items USING btree (annex_id, ordering);

CREATE INDEX regulatory_annexes_document_idx ON public.regulatory_annexes USING btree (document_id, annex_label);

CREATE INDEX regulatory_article_paragraphs_article_idx ON public.regulatory_article_paragraphs USING btree (article_id, ordering);

CREATE INDEX regulatory_article_recitals_recital_idx ON public.regulatory_article_recitals USING btree (recital_id);

CREATE INDEX regulatory_articles_document_idx ON public.regulatory_articles USING btree (document_id, article_number);

CREATE INDEX regulatory_enforcement_decisions_articles_idx ON public.regulatory_enforcement_decisions USING gin (gdpr_articles);

CREATE INDEX regulatory_enforcement_decisions_dpa_idx ON public.regulatory_enforcement_decisions USING btree (dpa, decision_date DESC);

CREATE INDEX regulatory_enforcement_decisions_topic_tags_idx ON public.regulatory_enforcement_decisions USING gin (topic_tags);

CREATE INDEX regulatory_guidelines_publisher_idx ON public.regulatory_guidelines USING btree (publisher, adopted_date DESC);

CREATE INDEX regulatory_guidelines_topic_tags_idx ON public.regulatory_guidelines USING gin (topic_tags);

CREATE INDEX regulatory_recitals_document_idx ON public.regulatory_recitals USING btree (document_id, recital_number);

CREATE INDEX subscriptions_provider_customer_idx ON public.subscriptions USING btree (provider_customer_id);

CREATE INDEX subscriptions_user_idx ON public.subscriptions USING btree (user_id);

CREATE UNIQUE INDEX watcher_findings_open_dedup_idx ON public.watcher_findings USING btree (profile_id, dedup_key) WHERE (status = 'open'::text);

CREATE INDEX watcher_findings_profile_status_idx ON public.watcher_findings USING btree (profile_id, status);

CREATE INDEX watcher_findings_user_idx ON public.watcher_findings USING btree (user_id);

CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON public.audit_log FOR EACH ROW EXECUTE FUNCTION public.audit_log_forbid_update();

CREATE TRIGGER product_review_flags_no_update BEFORE UPDATE ON public.product_review_flags FOR EACH ROW EXECUTE FUNCTION public.product_review_flags_forbid_update();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.ai_systems FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.compliance_profiles FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.dsars FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.findings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.notification_outbox FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.notification_preferences FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.obligations FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.onboarding_sessions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.processing_activities FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.regulatory_annex_items FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.regulatory_annexes FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.regulatory_article_paragraphs FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.regulatory_articles FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.regulatory_documents FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.regulatory_enforcement_decisions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.regulatory_guidelines FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.regulatory_recitals FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.subscriptions FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

CREATE TRIGGER set_updated_at BEFORE UPDATE ON public.watcher_findings FOR EACH ROW EXECUTE FUNCTION public.set_updated_at();

ALTER TABLE ONLY public.ai_systems
    ADD CONSTRAINT ai_systems_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.compliance_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.compliance_profiles
    ADD CONSTRAINT compliance_profiles_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.onboarding_sessions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.deadline_alert_log
    ADD CONSTRAINT deadline_alert_log_finding_id_fkey FOREIGN KEY (finding_id) REFERENCES public.findings(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.findings
    ADD CONSTRAINT findings_obligation_id_fkey FOREIGN KEY (obligation_id) REFERENCES public.obligations(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.findings
    ADD CONSTRAINT findings_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.compliance_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.findings
    ADD CONSTRAINT findings_watcher_finding_id_fkey FOREIGN KEY (watcher_finding_id) REFERENCES public.watcher_findings(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.notification_outbox
    ADD CONSTRAINT notification_outbox_finding_id_fkey FOREIGN KEY (finding_id) REFERENCES public.findings(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.onboarding_messages
    ADD CONSTRAINT onboarding_messages_session_id_fkey FOREIGN KEY (session_id) REFERENCES public.onboarding_sessions(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.processing_activities
    ADD CONSTRAINT processing_activities_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.compliance_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.product_review_flags
    ADD CONSTRAINT product_review_flags_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.compliance_profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.regulatory_annex_items
    ADD CONSTRAINT regulatory_annex_items_annex_id_fkey FOREIGN KEY (annex_id) REFERENCES public.regulatory_annexes(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.regulatory_annexes
    ADD CONSTRAINT regulatory_annexes_document_id_fkey FOREIGN KEY (document_id) REFERENCES public.regulatory_documents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.regulatory_article_paragraphs
    ADD CONSTRAINT regulatory_article_paragraphs_article_id_fkey FOREIGN KEY (article_id) REFERENCES public.regulatory_articles(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.regulatory_article_recitals
    ADD CONSTRAINT regulatory_article_recitals_article_id_fkey FOREIGN KEY (article_id) REFERENCES public.regulatory_articles(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.regulatory_article_recitals
    ADD CONSTRAINT regulatory_article_recitals_recital_id_fkey FOREIGN KEY (recital_id) REFERENCES public.regulatory_recitals(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.regulatory_articles
    ADD CONSTRAINT regulatory_articles_document_id_fkey FOREIGN KEY (document_id) REFERENCES public.regulatory_documents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.regulatory_recitals
    ADD CONSTRAINT regulatory_recitals_document_id_fkey FOREIGN KEY (document_id) REFERENCES public.regulatory_documents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.watcher_findings
    ADD CONSTRAINT watcher_findings_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.compliance_profiles(id) ON DELETE CASCADE;

ALTER TABLE public.ai_systems ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.audit_log ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.billing_webhook_events ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.compliance_profiles ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.deadline_alert_log ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.dsars ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.findings ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.notification_outbox ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.notification_preferences ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.obligations ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.onboarding_messages ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.onboarding_sessions ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.processing_activities ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.product_review_flags ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.regulatory_annex_items ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.regulatory_annexes ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.regulatory_article_paragraphs ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.regulatory_article_recitals ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.regulatory_articles ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.regulatory_documents ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.regulatory_enforcement_decisions ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.regulatory_guidelines ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.regulatory_recitals ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.subscriptions ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.watcher_findings ENABLE ROW LEVEL SECURITY;

ALTER TABLE public.weekly_briefing_log ENABLE ROW LEVEL SECURITY;

-- Obligations catalogue seed (generated from data/corpus/obligations.json
-- by apps/web/scripts/generate-obligations-seed.ts; carried verbatim).

-- Seed the obligations catalogue (ENT-157)
--
-- Generated from data/corpus/obligations.json by
-- scripts/generate-obligations-seed.ts — DO NOT EDIT BY HAND. Re-run
-- `bun run generate:obligations-seed` after editing the corpus; a drift-guard
-- unit test (__tests__/lib/corpus/obligations-seed-sql.test.ts) fails if this
-- file and the corpus disagree.
--
-- Why a migration and not the `bun run ingest:obligations` script: nothing ever
-- ran that script automatically, so production shipped with an empty
-- `public.obligations` and the Watcher's gap detector iterated zero rows —
-- an empty feed for every real user (ENT-157). Seeding from a migration means
-- every environment (local reset, CI `supabase start`, remote deploy) gets
-- the catalogue with no manual step.
--
-- Idempotent: `on conflict (slug) do update` upserts by the natural key, so
-- re-applying the migration (or re-running the curated ingest) converges to
-- the same row state. Rows removed from a later corpus snapshot are left
-- in place — same non-destructive policy as the runtime ingest.

insert into public.obligations
  (slug, title, summary, citation_celex, citation_kind, citation_article, citation_recital, citation_annex, citation_paragraph, applies_when, severity, due_within_days, recurrence, effective_date, topic_tags)
values
  ('gdpr-art-30-ropa',
   'Records of Processing Activities (ROPA)',
   'Article 30 GDPR: Controllers (and processors) must maintain a written record of processing activities under their responsibility, including purposes, categories of data subjects and data, recipients, third-country transfers, retention schedules, and a general description of technical and organisational security measures. The 250-employee exemption in Article 30(5) is narrow (it does not apply where processing is not occasional, involves special categories, or is likely to result in a risk to data subjects) so most SMEs cannot rely on it.',
   '32016R0679',
   'article',
   30,
   null,
   null,
   null,
   '{"role":"controller","requires":["ropa"]}'::jsonb,
   'high',
   null,
   'continuous',
   '2018-05-25',
   array['ropa', 'documentation', 'accountability']::text[]),
  ('gdpr-art-35-dpia',
   'Data Protection Impact Assessment (DPIA) for high-risk processing',
   'Article 35 GDPR: Before processing that is likely to result in a high risk to the rights and freedoms of natural persons, the controller must carry out a Data Protection Impact Assessment. Triggers include systematic and extensive evaluation of personal aspects (including profiling) producing legal effects, large-scale processing of special categories or criminal-conviction data, and systematic monitoring of publicly accessible areas. National supervisory authorities publish DPIA blacklists under Art 35(4); consult yours.',
   '32016R0679',
   'article',
   35,
   null,
   null,
   null,
   '{"role":"controller","thresholds":{"high_risk":true}}'::jsonb,
   'high',
   null,
   'ad-hoc',
   '2018-05-25',
   array['dpia', 'risk', 'profiling']::text[]),
  ('gdpr-art-33-breach-notification',
   'Personal data breach notification to the supervisory authority (72 hours)',
   'Article 33 GDPR: The controller must notify the competent supervisory authority of a personal data breach without undue delay and, where feasible, no later than 72 hours after becoming aware of it. The 72-hour window starts at the moment of awareness, not at detection by an automated system. If the breach is unlikely to result in a risk to natural persons the notification can be skipped; the controller''s reasoning must still be documented under Art 33(5). Processors notify the controller without undue delay under Art 33(2).',
   '32016R0679',
   'article',
   33,
   null,
   null,
   null,
   '{"role":"controller"}'::jsonb,
   'high',
   0,
   'on-event',
   '2018-05-25',
   array['breach', 'notification', 'incident-response']::text[]),
  ('gdpr-art-34-breach-communication',
   'Communication of a personal data breach to the data subject',
   'Article 34 GDPR: When a personal data breach is likely to result in a high risk to the rights and freedoms of natural persons, the controller must communicate the breach to the affected data subjects without undue delay. Article 34(3) lists exceptions: appropriate prior protection (encryption), subsequent risk-mitigating measures, or disproportionate effort (in which case a public communication suffices). Independent of the Article 33 supervisory-authority notification: both can apply to the same incident.',
   '32016R0679',
   'article',
   34,
   null,
   null,
   null,
   '{"role":"controller","thresholds":{"high_risk":true}}'::jsonb,
   'high',
   null,
   'on-event',
   '2018-05-25',
   array['breach', 'data-subject', 'incident-response']::text[]),
  ('gdpr-art-37-dpo-appointment',
   'Designation of a Data Protection Officer (DPO)',
   'Article 37 GDPR: A controller or processor must designate a DPO where (a) processing is carried out by a public authority, (b) the core activities require regular and systematic monitoring of data subjects on a large scale, or (c) the core activities consist of large-scale processing of special categories or criminal-conviction data. Member State law may add further mandatory cases. Voluntary designations are permitted but trigger the full Article 38-39 duty stack: Article 39 lists the DPO''s tasks (advising, monitoring compliance, DPIA cooperation, supervisory-authority liaison).',
   '32016R0679',
   'article',
   37,
   null,
   null,
   null,
   '{"role":"controller","thresholds":{"large_scale_monitoring":true},"requires":["dpo"]}'::jsonb,
   'high',
   null,
   'continuous',
   '2018-05-25',
   array['dpo', 'governance', 'accountability']::text[]),
  ('gdpr-art-6-lawful-basis',
   'Lawful basis for processing personal data',
   'Article 6 GDPR, Processing personal data is lawful only if at least one of six bases applies: consent, contract necessity, legal obligation, vital interests, public task, or legitimate interests. The chosen basis must be identified before processing starts and surfaced in the Article 13/14 transparency notice. Switching basis mid-flight is generally disallowed (see EDPB Guidelines 5/2020 on consent §121). Public authorities cannot rely on legitimate interests for their core tasks (Art 6(1)(f) closing sentence).',
   '32016R0679',
   'article',
   6,
   null,
   null,
   null,
   '{"role":"controller"}'::jsonb,
   'high',
   null,
   'continuous',
   '2018-05-25',
   array['lawful-basis', 'consent', 'principles']::text[]),
  ('gdpr-art-7-consent-conditions',
   'Conditions for consent (where consent is the lawful basis)',
   'Article 7 GDPR: Where processing is based on consent, the controller must be able to demonstrate that consent was given (Art 7(1)). Consent must be a freely given, specific, informed, and unambiguous indication via a clear affirmative action (Art 4(11)). Withdrawal must be as easy as giving consent (Art 7(3)); pre-ticked boxes are invalid (Rec. 32). Consent is presumed not freely given where there is a clear imbalance between data subject and controller (employer/employee, public authority).',
   '32016R0679',
   'article',
   7,
   null,
   null,
   null,
   '{"role":"controller","lawful_basis_includes":"consent"}'::jsonb,
   'high',
   null,
   'continuous',
   '2018-05-25',
   array['consent', 'lawful-basis']::text[]),
  ('gdpr-arts-12-22-data-subject-rights',
   'Data subject rights (information, access, rectification, erasure, restriction, portability, objection)',
   'Articles 12-22 GDPR: Controllers must facilitate the exercise of data subject rights: transparent information under Articles 12-14, access under Article 15, rectification under Article 16, erasure ("right to be forgotten") under Article 17, restriction under Article 18, notification of recipients under Article 19, portability under Article 20, objection under Article 21, and safeguards against solely automated decisions under Article 22. Standard response window is one month (Art 12(3)), extendable by two months for complex requests. No fee for manifestly unfounded or excessive requests: a fee or refusal must be justified.',
   '32016R0679',
   'article',
   12,
   null,
   null,
   null,
   '{"role":"controller"}'::jsonb,
   'high',
   30,
   'on-event',
   '2018-05-25',
   array['data-subject-rights', 'dsar', 'transparency']::text[]),
  ('gdpr-art-32-security-of-processing',
   'Security of processing: technical and organisational measures',
   'Article 32 GDPR: Controllers and processors must implement appropriate technical and organisational measures (TOMs) to ensure a level of security appropriate to the risk, taking into account the state of the art, costs, and the nature/scope/context/purposes of processing. The non-exhaustive list in Art 32(1) names pseudonymisation, encryption, confidentiality/integrity/availability/resilience, the ability to restore access after an incident, and a process for regularly testing and evaluating effectiveness. Adherence to an approved Article 40 code of conduct or Article 42 certification can be evidence of compliance.',
   '32016R0679',
   'article',
   32,
   null,
   null,
   null,
   '{"role":"controller"}'::jsonb,
   'high',
   null,
   'continuous',
   '2018-05-25',
   array['security', 'toms', 'encryption']::text[]),
  ('gdpr-chapter-v-international-transfers',
   'International transfers to third countries: Chapter V safeguards',
   'Articles 44-49 GDPR, Personal data transfers to a country outside the EEA require either an adequacy decision (Art 45), appropriate safeguards (Art 46, Standard Contractual Clauses, Binding Corporate Rules, approved codes/certifications), or a narrow Article 49 derogation. Schrems II (CJEU C-311/18) requires a Transfer Impact Assessment to evaluate whether the target country''s surveillance laws undermine SCC protections; supplementary measures may be needed. The EU-US Data Privacy Framework (July 2023 adequacy decision) covers certified US importers.',
   '32016R0679',
   'article',
   44,
   null,
   null,
   null,
   '{"role":"controller","thresholds":{"cross_border_transfers":true},"requires":["transfer_safeguards"]}'::jsonb,
   'high',
   null,
   'continuous',
   '2018-05-25',
   array['transfers', 'scc', 'third-country']::text[]),
  ('gdpr-art-28-processor-contracts',
   'Controller-processor contracts (Article 28 DPAs)',
   'Article 28 GDPR, Where a controller engages a processor, the processing must be governed by a written contract (the Data Processing Agreement) binding the processor to a defined list of duties: process only on documented instructions, ensure confidentiality, take Article 32 security measures, engage sub-processors only with prior controller authorisation, assist with data subject rights and breach handling, and delete or return data at the end of the engagement. The European Commission published SCC-style Article 28 template clauses in Decision 2021/915.',
   '32016R0679',
   'article',
   28,
   null,
   null,
   null,
   '{"role":"controller","engages_processor":true}'::jsonb,
   'high',
   null,
   'continuous',
   '2018-05-25',
   array['processor', 'contracts', 'vendor-management']::text[]),
  ('ai-act-art-4-ai-literacy',
   'AI literacy for staff who operate or are affected by AI systems',
   'Article 4 EU AI Act: Providers and deployers of AI systems must take measures to ensure, to their best extent, a sufficient level of AI literacy of their staff and other persons dealing with the operation and use of AI systems on their behalf. The measures must take into account the persons'' technical knowledge, experience, education, training, the context in which the AI systems are used, and the persons or groups of persons on which the AI systems will be used. In force since 2 February 2025 (one of the earliest AI Act provisions to apply).',
   '32024R1689',
   'article',
   4,
   null,
   null,
   null,
   '{"role":"deployer","requires":["ai_register"]}'::jsonb,
   'medium',
   null,
   'continuous',
   '2025-02-02',
   array['ai-act', 'ai-literacy', 'training']::text[]),
  ('ai-act-art-26-deployer-obligations',
   'Deployer obligations for high-risk AI systems',
   'Article 26 EU AI Act: Deployers of high-risk AI systems must use the system in accordance with the provider''s instructions for use, assign human oversight to natural persons with appropriate competence (Art 26(2)), monitor operation, keep automatically generated logs for at least six months (Art 26(6)), inform workers and their representatives before putting a high-risk system into service in the workplace (Art 26(7)), and conduct a fundamental rights impact assessment under Article 27 where applicable. Most Article 26 duties apply from 2 August 2026.',
   '32024R1689',
   'article',
   26,
   null,
   null,
   null,
   '{"role":"deployer","thresholds":{"high_risk":true}}'::jsonb,
   'high',
   null,
   'continuous',
   '2026-08-02',
   array['ai-act', 'high-risk', 'deployer']::text[]),
  ('ai-act-annex-iii-high-risk-systems',
   'High-risk system classification under Annex III',
   'Annex III EU AI Act, Standalone AI systems falling under one of eight enumerated use-case areas are designated high-risk under Article 6(2): biometrics, critical infrastructure, education and vocational training, employment and worker management, access to essential services, law enforcement, migration and border control, and administration of justice and democratic processes. High-risk classification triggers the full Articles 9-17 obligation stack (risk management, data governance, technical documentation, record-keeping, transparency, human oversight, accuracy/robustness/cybersecurity). Annex III obligations apply from 2 August 2026 (subject to any Digital Omnibus deferral).',
   '32024R1689',
   'annex',
   null,
   null,
   'III',
   null,
   '{"role":"provider","thresholds":{"high_risk":true}}'::jsonb,
   'high',
   null,
   'continuous',
   '2026-08-02',
   array['ai-act', 'high-risk', 'annex-iii']::text[]),
  ('ai-act-art-50-transparency',
   'Transparency obligations for certain AI systems (deepfakes, chatbots, emotion recognition)',
   'Article 50 EU AI Act: Providers of AI systems intended to interact directly with natural persons must design the system so that affected persons are informed they are interacting with an AI (chatbot disclosure). Deployers of emotion-recognition or biometric-categorisation systems must inform exposed persons. Deployers of generative AI producing image/audio/video content constituting deepfakes must disclose the artificial origin; text published on matters of public interest is similarly disclosable unless under editorial control. Article 50 duties apply from 2 August 2026.',
   '32024R1689',
   'article',
   50,
   null,
   null,
   null,
   '{"role":"deployer"}'::jsonb,
   'medium',
   null,
   'continuous',
   '2026-08-02',
   array['ai-act', 'transparency', 'deepfake']::text[])
on conflict (slug) do update set
  title = excluded.title,
  summary = excluded.summary,
  citation_celex = excluded.citation_celex,
  citation_kind = excluded.citation_kind,
  citation_article = excluded.citation_article,
  citation_recital = excluded.citation_recital,
  citation_annex = excluded.citation_annex,
  citation_paragraph = excluded.citation_paragraph,
  applies_when = excluded.applies_when,
  severity = excluded.severity,
  due_within_days = excluded.due_within_days,
  recurrence = excluded.recurrence,
  effective_date = excluded.effective_date,
  topic_tags = excluded.topic_tags;

-- +goose Down
-- A baseline has no down: restore from backup instead.
