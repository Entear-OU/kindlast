-- +goose Up
-- 00026_onboarding_answers.sql (ENT-212, §2, §24 step 6)
--
-- An onboarding turn can now say which fact it is about, and what we took the
-- answer to mean.
--
-- WHY THE TRANSCRIPT CARRIES THE TYPED VALUE, AND NOT ONLY THE WORDS
--
-- The legacy flow interviewed freely and then ran a second model pass over the
-- whole transcript to extract a structured profile. `compliance_profiles`
-- decides which obligations apply to an organisation, so a field value that
-- pass invented produces wrong findings later, far enough from the mistake that
-- nobody traces it back. ENT-212 names that as the thing to design out rather
-- than to guard.
--
-- These two columns are how it is designed out. A question names the fact it
-- asks about, the answer is parsed into that fact's declared kind by Go at the
-- moment it is given, and the result is stored beside the words the person
-- actually typed. Nothing reads the transcript afterwards to decide what was
-- meant, so there is no later interpretation to be wrong.
--
-- Storing both halves rather than either one is the point. `content` is what
-- the person said, and is what the console renders back to them. `fact_value`
-- is what the product will believe if they confirm it. A reader can hold the
-- two side by side and see whether "Ireland, Spain and sometimes Portugal"
-- became the list we say it did, which is the check that makes the profile
-- worth trusting.
--
-- NULLABLE, BECAUSE MOST TURNS ARE NOT ANSWERS
--
-- Every existing row predates this and has no key, which is correct rather than
-- backfilled: those conversations were extracted the old way and their words
-- are all there ever was. A skip is a row with a key and no value, and that
-- distinction is load-bearing: "we asked and they declined" is a different
-- state from "we never asked", and only the first one means the fact is
-- deliberately absent from the profile.
--
-- NO NEW TABLE, NO NEW POLICY, AND THAT IS THE INTERESTING PART
--
-- `onboarding_sessions` and `onboarding_messages` already carry `org_id`,
-- already have FORCE ROW LEVEL SECURITY, and already hold the four two-GUC
-- policies from 00002. Adding columns to a table inherits every one of them, so
-- the tenancy boundary here is the one the isolation suite already asserts. A
-- new table would have been the version of this change worth reviewing
-- carefully; this one is not.

-- Which fact this turn is about.
--
-- On an assistant turn it is the question being asked. On a user turn it is the
-- question being answered. Unconstrained text for the same reason
-- `org_profile_facts.key` is: which facts the product understands is a decision
-- that changes as it learns, and a check constraint here would make every new
-- question a migration. The closed set is the proto enum and the Go table that
-- validates against it.
alter table public.onboarding_messages
  add column fact_key text;

-- What we took the answer to mean, in the shape `org_profile_facts.value` will
-- hold it. jsonb rather than columns, because a fact is sometimes a boolean,
-- sometimes a list of jurisdictions and sometimes a number, and because
-- confirmation then copies this value across without reinterpreting it.
alter table public.onboarding_messages
  add column fact_value jsonb;

alter table public.onboarding_messages
  add constraint onboarding_messages_fact_key_check
    check (fact_key is null or fact_key <> '');

-- A VALUE ONLY EVER BELONGS TO A PERSON'S ANSWER.
--
-- Two things this refuses, and both of them would be a profile claiming a
-- provenance it does not have. A value with no key is a fact about nothing. A
-- value on an assistant turn is the product having answered its own question,
-- which is exactly the failure the typed-at-answer-time design exists to
-- prevent, so it is refused by the database rather than by the handler that
-- happens to write it today.
alter table public.onboarding_messages
  add constraint onboarding_messages_value_is_an_answer
    check (fact_value is null or (fact_key is not null and role = 'user'));

-- The latest answer to each question, which is the only read that matters.
--
-- A person may go back and answer a question again, and the later answer wins.
-- Partial, because the questions and the conversational turns share this table
-- and neither is worth indexing here.
create index onboarding_messages_answers_idx
  on public.onboarding_messages (session_id, fact_key, ordering desc)
  where fact_value is not null;

-- +goose Down
drop index if exists public.onboarding_messages_answers_idx;

alter table public.onboarding_messages
  drop constraint if exists onboarding_messages_value_is_an_answer;
alter table public.onboarding_messages
  drop constraint if exists onboarding_messages_fact_key_check;

alter table public.onboarding_messages drop column if exists fact_value;
alter table public.onboarding_messages drop column if exists fact_key;
