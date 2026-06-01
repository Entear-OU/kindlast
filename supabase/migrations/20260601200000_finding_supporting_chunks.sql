-- The detail view's "supporting chunks" for a finding (ENT-64)
--
-- ENT-59 gave every finding a precise citation *label* + source URL derived from
-- the obligation's own citation fields. The finding detail view wants more than a
-- one-line label: it wants the verbatim regulatory text behind the proposal so a
-- founder can read (and audit) exactly what the obligation says. This function is
-- that read model — it assembles the ordered "chunks" the detail view renders.
--
-- Source of truth is the regulatory corpus (ENT-48 / ENT-95), joined by NATURAL
-- KEY off the obligation's citation:
--
--     obligations.citation_celex   → regulatory_documents.celex_number
--     obligations.citation_article → regulatory_articles.article_number
--                                    → regulatory_article_paragraphs (by `ordering`)
--     obligations.citation_recital → regulatory_recitals.recital_number
--
-- For an article, chunk 1 is the article (heading + curated summary) and chunks
-- 2..N are its sub-paragraphs in source order, each labelled like
-- "GDPR Art. 30(1)(b)" via the existing pure builders public.analyst_citation_label
-- / _url (ENT-59). For a recital, chunk 1 is the recital summary. (ENT-97 rotated
-- the corpus from mirrored verbatim `body` text to a curated `summary` routing
-- artifact; verbatim prose lives at the document's source URL.) Otherwise — an
-- annex, or a regulation
-- whose corpus hasn't been ingested yet — we FALL BACK to a single chunk built
-- from the finding's own denormalised citation (regulatory_obligation /
-- supporting_context / citation_url). The fallback is the guarantee that the
-- detail view always has at least one chunk to show, corpus or not.
--
-- Security: SECURITY DEFINER so it can read the public corpus regardless of the
-- caller, but it is safe to expose to a user-scoped client because it resolves
-- the finding by id and returns ZERO rows (no exception) unless the row exists
-- AND belongs to auth.uid(). A caller therefore cannot read another user's
-- chunks, and a missing/foreign id is indistinguishable from "no chunks".
--
-- Idempotent: `create or replace function`.
-- ─────────────────────────────────────────────────────────────────────────────

create or replace function public.finding_supporting_chunks(p_finding_id uuid)
returns table (
  ordinal     int,
  label       text,
  quoted_text text,
  source_url  text
)
language plpgsql
security definer
set search_path = public, pg_temp
as $$
declare
  v_finding public.findings;
  v_obl     public.obligations;
  v_doc     public.regulatory_documents;
  v_article public.regulatory_articles;
  v_recital public.regulatory_recitals;
  v_para    record;
  v_ord     int;
begin
  -- Ownership gate: resolve the finding; bail (no rows, no exception) unless it
  -- exists and is owned by the caller. Makes the RPC safe under a user client.
  select * into v_finding from public.findings where id = p_finding_id;
  if not found or v_finding.user_id <> auth.uid() then
    return;
  end if;

  select * into v_obl from public.obligations where id = v_finding.obligation_id;

  -- Resolve the corpus document by CELEX natural key (may be absent).
  if v_obl.id is not null then
    select * into v_doc
      from public.regulatory_documents
     where celex_number = v_obl.citation_celex;
  end if;

  -- Case 1: article with a matching corpus article row.
  if v_obl.citation_kind = 'article' and v_doc.id is not null then
    select * into v_article
      from public.regulatory_articles
     where document_id = v_doc.id
       and article_number = v_obl.citation_article;

    if found then
      -- Chunk 1: the article itself (heading + body).
      ordinal     := 1;
      label       := public.analyst_citation_label(
                       v_obl.citation_celex, 'article', v_obl.citation_article,
                       null, null, null);
      quoted_text := v_article.heading || E'\n\n' || v_article.summary;
      source_url  := public.analyst_citation_url(
                       v_obl.citation_celex, 'article', v_obl.citation_article,
                       null, null);
      return next;

      -- Chunks 2..N: sub-paragraphs in source (`ordering`) order, each deep-
      -- labelled (e.g. "GDPR Art. 30(1)(b)") but anchored to the same article URL.
      v_ord := 1;
      for v_para in
        select paragraph_label, summary
          from public.regulatory_article_paragraphs
         where article_id = v_article.id
         order by ordering asc
      loop
        v_ord       := v_ord + 1;
        ordinal     := v_ord;
        label       := public.analyst_citation_label(
                         v_obl.citation_celex, 'article', v_obl.citation_article,
                         null, null, v_para.paragraph_label);
        quoted_text := v_para.summary;
        source_url  := public.analyst_citation_url(
                         v_obl.citation_celex, 'article', v_obl.citation_article,
                         null, null);
        return next;
      end loop;

      return;
    end if;
  end if;

  -- Case 2: recital with a matching corpus recital row.
  if v_obl.citation_kind = 'recital' and v_doc.id is not null then
    select * into v_recital
      from public.regulatory_recitals
     where document_id = v_doc.id
       and recital_number = v_obl.citation_recital;

    if found then
      ordinal     := 1;
      label       := public.analyst_citation_label(
                       v_obl.citation_celex, 'recital', null,
                       v_obl.citation_recital, null, null);
      quoted_text := v_recital.summary;
      source_url  := public.analyst_citation_url(
                       v_obl.citation_celex, 'recital', null,
                       v_obl.citation_recital, null);
      return next;
      return;
    end if;
  end if;

  -- Fallback: annex, or no matching corpus document/article/recital. The detail
  -- view still gets one chunk, built from the finding's denormalised citation.
  ordinal     := 1;
  label       := v_finding.regulatory_obligation;
  quoted_text := v_finding.supporting_context;
  source_url  := v_finding.citation_url;
  return next;
  return;
end;
$$;
