-- +goose Up
-- 00009_classify_obligations.sql (ENT-203, closing ENT-165)
--
-- What approving a finding actually does, for the three obligations where the
-- answer is not "a human reads it".
--
-- 00007 built the mechanism and deliberately left every obligation at the
-- default `review`, because which obligation triggers which Executor action is
-- a product decision rather than a schema one. The product owner ruled on
-- 2026-08-15, on the mapping drafted against all fifteen obligations in
-- data/corpus/obligations.json. This is that ruling.
--
-- Three rows change. Twelve keep the default and are deliberately not listed:
-- a statement of `review` for each would be twelve lines saying nothing, and
-- would invite the next person to think the absence of a row means the
-- obligation was overlooked rather than considered.
--
-- WHY THESE THREE
--
--   gdpr-art-30-ropa                    -> create_ropa
--     The obligation IS to hold a record of processing activities. Approving
--     means "this activity belongs in our ROPA", and the row created is
--     precisely the artefact Article 30 requires.
--
--   ai-act-annex-iii-high-risk-systems  -> create_ai_system
--     The finding says a system falls under Annex III. Approving is the
--     classification decision, and the register entry is what makes it
--     auditable afterwards.
--
--   ai-act-art-26-deployer-obligations  -> create_ai_system
--     Deployer obligations attach to a specific high-risk system. Approving
--     without registering the system leaves obligations pointing at nothing.
--
-- WHY NOTHING MAPS TO create_dsar, WHICH IS THE PART TO READ
--
-- The obvious candidate is gdpr-arts-12-22-data-subject-rights, and mapping it
-- would be actively harmful rather than merely untidy.
--
-- Findings against those articles come from a PROFILE GAP: "you have no process
-- for handling data subject rights". No data subject made a request. But
-- executor_create_dsar_on_approval writes
--
--   subject_name    = payload ->> 'requester'   -- null, for a profile gap
--   received_at     = now()
--   response_due_at = now() + interval '30 days'
--
-- so approving one would create a data subject request with no subject and a
-- live 30-day statutory clock, and then show it back to the customer as an
-- obligation they are running out of time on. The product would be inventing a
-- legal deadline.
--
-- That is the asymmetry worth keeping in mind for every future classification:
-- create_ropa and create_ai_system create a record the customer owns and edits,
-- while create_dsar creates a request with a statutory clock attached.
-- Misclassifying into the first two is untidy; misclassifying into the third
-- fabricates an obligation.
--
-- create_dsar is not dead. It belongs to signals from
-- watcher_detect_dsar_escalation, which are about DSARs that already exist and
-- therefore already have a row. It stays unmapped until there is an intake path
-- where a real request arrives with a real received_at, and ENT-224 has to land
-- first: the trigger starts the clock at approval rather than at receipt, which
-- would tell a customer they are on time when they are already late.
--
-- FOUR THAT WERE CONSIDERED AND LEFT AT review
--
-- gdpr-art-6-lawful-basis, gdpr-chapter-v-international-transfers,
-- gdpr-art-28-processor-contracts and ai-act-art-50-transparency. Each is
-- arguably create_ropa or create_ai_system, because each concerns a FIELD on a
-- record rather than the record itself: a lawful basis, a recipient, a
-- transfer. The Executor can only create records, not annotate existing ones,
-- so mapping them would produce a second row alongside the first rather than
-- completing it. They move if an update action is ever added.
--
-- HOW THIS APPLIES TO EXISTING FINDINGS
--
-- It does not, and that is deliberate. analyst_convert_signal refreshes
-- action_type on conflict (00007), so re-running the Analyst reclassifies open
-- findings. Findings already approved are untouched: the executor triggers fire
-- on the status transition, so nothing is created retroactively for a decision
-- taken before the obligation was classified.

update public.obligations set action_type = 'create_ropa'
 where slug = 'gdpr-art-30-ropa';

update public.obligations set action_type = 'create_ai_system'
 where slug in ('ai-act-annex-iii-high-risk-systems',
                'ai-act-art-26-deployer-obligations');

-- The corpus is the source of truth and this migration is its application, so
-- they have to agree. The generator that used to keep 00001's seed in step with
-- data/corpus/obligations.json was deleted with the console (2a5c454) and its
-- drift test with it, which means nothing checks that agreement automatically
-- any more. Until something does, this asserts the one property that matters
-- here: exactly the three intended obligations are classified, and nothing
-- carries create_dsar.
-- +goose StatementBegin
do $$
declare
  v_classified int;
  v_dsar       int;
begin
  select count(*) into v_classified
    from public.obligations
   where action_type <> 'review'
     and slug in ('gdpr-art-30-ropa',
                  'ai-act-annex-iii-high-risk-systems',
                  'ai-act-art-26-deployer-obligations');

  if v_classified <> 3 then
    raise exception
      'expected 3 classified obligations, found %. The corpus and this '
      'migration disagree, or the obligations seed did not run.', v_classified;
  end if;

  select count(*) into v_dsar
    from public.obligations where action_type = 'create_dsar';

  if v_dsar <> 0 then
    raise exception
      '% obligations map to create_dsar. Nothing may, until ENT-224 lands: '
      'the trigger starts the statutory clock at approval rather than at '
      'receipt.', v_dsar;
  end if;
end
$$;
-- +goose StatementEnd

-- +goose Down

update public.obligations set action_type = 'review'
 where slug in ('gdpr-art-30-ropa',
                'ai-act-annex-iii-high-risk-systems',
                'ai-act-art-26-deployer-obligations');
