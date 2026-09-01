package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/records"
	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/sweep"
)

// Running the Watcher and the Analyst from Go (ENT-259, inside ENT-225).
//
// # WHAT THIS FILE IS AND IS NOT
//
// It is the reads and the writes. Every judgement is in `domain/sweep`, which
// has no database handle, so this file is queries, a loop, and two upserts.
// That split is the point of the move rather than a style preference: while the
// detectors were plpgsql the reads and the rules were one body of code that
// only ran through a live stack, and the bug PR #223 fixed (a detector reading
// `dsars`, which the producer role had never been granted) could not be seen
// from any test. Here the reads are four queries a grant test walks into on the
// first run, and the rules are a table test.
//
// # THE ORGANISATION IS THE GUC, AND EVERY QUERY STILL NAMES IT WHERE IT MUST
//
// One transaction, one `app.current_org_id`, no user: a sweep is started by a
// schedule and there is no member to name, which is the shape the agent's
// policies were written for (00008). `compliance_profiles`, `watcher_findings`
// and `findings` give the producer an org-equality policy, so a query against
// those may leave the scoping to the GUC. `dsars` is read through the profile's
// organisation for the same reason the plpgsql did it that way, and the corpus
// carries no `org_id` at all.
//
// # ONE PROFILE PER ORGANISATION, THE NEWEST
//
// `run_watcher()` selected `distinct on (org_id)` ordered by `created_at desc`
// and looped, which under the agent's GUC could only ever find one
// organisation. So this reads the newest profile directly and the count it
// returns is 1 or 0. An organisation that onboarded twice is swept against what
// it said most recently, which is the same profile the agentic Watcher is
// offered obligations for.

// runWatcher detects and writes signals for one organisation's newest profile.
//
// # THE COUNT IT RETURNS IS SIGNALS, WHICH IS A DELIBERATE CORRECTION
//
// `run_watcher()` returned the number of PROFILES it swept, and so did
// `run_analyst()`. Under the agent's GUC only one organisation is ever visible,
// so both were always 1, and `RunSweepResponse` has been reporting 1 signal and
// 1 finding for every sweep since it shipped, whatever the sweep actually did.
// The proto calls the fields `signals` and `findings`, the workers' own tests
// assert totals like 6 and 4, and an operator reading a workflow history has
// no way to tell that the number means neither.
//
// So this returns what the field is named after. Nothing branches on it (it is
// summed and reported), and no stored row changes: the equivalence test
// compares the signals and findings both implementations write and requires
// them identical. What changes is that the number an operator reads is now
// true.
func runWatcher(ctx context.Context, tx pgx.Tx, logger *slog.Logger) (int32, error) {
	in, err := sweepInputs(ctx, tx)
	if err != nil {
		return 0, err
	}
	if in.Profile.ID == "" {
		// No profile: nothing to watch and nowhere to hang a signal. The
		// plpgsql's loop simply found no row.
		return 0, nil
	}

	// Counted by deduplication key rather than by emission. Both DSAR
	// detectors write `dsar:{id}`, so an urgent request is emitted twice and
	// is one signal: the second write refreshes the row the first opened.
	// Counting emissions would report two, which is the kind of number that
	// makes somebody go looking for a row that was never there.
	raised := map[string]bool{}
	for _, signal := range sweep.Detect(in) {
		if err := emitSignal(ctx, tx, in.Profile, signal); err != nil {
			return 0, err
		}
		raised[signal.DedupKey] = true
	}

	if _, err := tx.Exec(ctx, `
		update compliance_profiles set watcher_last_run_at = now() where id = $1::uuid
	`, in.Profile.ID); err != nil {
		return 0, fmt.Errorf("postgres: stamping the sweep time: %w", err)
	}

	logger.DebugContext(ctx, "swept a profile",
		"profile_id", in.Profile.ID, "signals", len(raised))
	return int32(len(raised)), nil
}

// sweepInputs loads everything one profile's detectors read.
//
// Five reads, and the grants they need are the grants the producer role needs.
// A detector that starts reading a sixth table adds a query here and turns
// `db/tests/agent-role.test.ts` red on the commit that adds it, which is the
// property ENT-259 was filed to buy.
func sweepInputs(ctx context.Context, tx pgx.Tx) (sweep.Inputs, error) {
	var in sweep.Inputs

	// `current_date` and `now()` from the database rather than from Go's
	// clock, so the windows are evaluated in the same time zone the plpgsql
	// evaluated them in. See sweep.Inputs for why both are needed.
	if err := tx.QueryRow(ctx, `select current_date, now()`).
		Scan(&in.Today, &in.Now); err != nil {
		return in, fmt.Errorf("postgres: reading the sweep clock: %w", err)
	}

	err := tx.QueryRow(ctx, `
		select id::text, org_id::text, has_dpo, has_ropa, transfers_outside_eu,
		       ai_systems, transfer_destinations, data_categories, vendor_list
		  from compliance_profiles
		 order by created_at desc
		 limit 1
	`).Scan(
		&in.Profile.ID, &in.Profile.OrgID, &in.Profile.HasDPO, &in.Profile.HasROPA,
		&in.Profile.TransfersOutsideEU, &in.Profile.AISystems,
		&in.Profile.TransferDestinations, &in.Profile.DataCategories,
		&in.Profile.VendorList,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sweep.Inputs{}, nil
	}
	if err != nil {
		return in, fmt.Errorf("postgres: reading the profile to sweep: %w", err)
	}

	if in.Obligations, err = applicableObligations(ctx, tx, in.Profile.ID); err != nil {
		return in, err
	}
	if in.DSARs, err = outstandingDSARs(ctx, tx, in.Profile.OrgID); err != nil {
		return in, err
	}
	if in.DismissedGapKeys, err = dismissedGapKeys(ctx, tx, in.Profile.ID); err != nil {
		return in, err
	}
	return in, nil
}

// applicableObligations reads the obligations that bind this profile.
//
// # WHY THE APPLICABILITY PREDICATE STAYS IN SQL
//
// `watcher_obligation_applies` is not moved by ENT-259 and is not listed among
// the functions it moves. It reads `org_profile_facts`, and it is shared: the
// agentic Watcher's context assembly calls it to decide what a run may cite
// (ENT-258), and the narrative service reads the same declaration. Two
// evaluators of "does this obligation bind this organisation" is the
// arrangement ENT-246 was filed about, and an agent offered a differently
// computed set would disagree with the sweep running beside it in ways nobody
// could explain. Moving it is its own change, with its own equivalence test,
// and moving it halfway is worse than not moving it.
//
// What is decided in Go is everything downstream of that answer: which of the
// obligations that apply is actually a gap, how close a deadline is, and what
// to say about it.
func applicableObligations(ctx context.Context, tx pgx.Tx, profileID string) ([]sweep.Obligation, error) {
	rows, err := tx.Query(ctx, `
		select o.slug, o.title, o.severity, o.effective_date,
		       case
		         when jsonb_typeof(o.applies_when -> 'requires') = 'array'
		           then array(select jsonb_array_elements_text(o.applies_when -> 'requires'))
		         else '{}'::text[]
		       end
		  from obligations o, compliance_profiles p
		 where p.id = $1::uuid
		   and public.watcher_obligation_applies(o.applies_when, p)
		 order by o.slug
	`, profileID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading the obligations to sweep: %w", err)
	}
	defer rows.Close()

	var obligations []sweep.Obligation
	for rows.Next() {
		var o sweep.Obligation
		if err := rows.Scan(&o.Slug, &o.Title, &o.Severity, &o.EffectiveDate, &o.Requires); err != nil {
			return nil, fmt.Errorf("postgres: reading an obligation to sweep: %w", err)
		}
		obligations = append(obligations, o)
	}
	return obligations, rows.Err()
}

// outstandingDSARs reads the requests that are still owed a response.
//
// Org-scoped rather than profile-scoped: a data-subject request belongs to the
// organisation whoever logged it, which is why the plpgsql read
// `where org_id = v_profile.org_id` and why this does too.
//
// `response_due_at::date` comes back beside the instant. The cast is evaluated
// in the database's time zone, and deriving it in Go would move the day count
// for every request near a midnight boundary; a cast is not a decision, so
// leaving it here costs nothing the move was buying.
func outstandingDSARs(ctx context.Context, tx pgx.Tx, orgID string) ([]sweep.DSAR, error) {
	rows, err := tx.Query(ctx, `
		select d.id::text, d.subject_name, d.response_due_at, d.response_due_at::date as due_date
		  from dsars d
		 where d.org_id = $1::uuid
		   and d.status in ('open', 'in_progress')
		   and d.responded_at is null
		 order by d.response_due_at, d.id
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading outstanding data-subject requests: %w", err)
	}
	defer rows.Close()

	var dsars []sweep.DSAR
	for rows.Next() {
		var d sweep.DSAR
		if err := rows.Scan(&d.ID, &d.SubjectName, &d.ResponseDueAt, &d.DueDate); err != nil {
			return nil, fmt.Errorf("postgres: reading a data-subject request: %w", err)
		}
		dsars = append(dsars, d)
	}
	return dsars, rows.Err()
}

// dismissedGapKeys reads the gap signals this profile has already dismissed.
//
// One query instead of the plpgsql's `exists` per obligation. Same answer, and
// the reason to say so is that the set is small and bounded by the corpus:
// there is one key per obligation, not one per finding.
func dismissedGapKeys(ctx context.Context, tx pgx.Tx, profileID string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `
		select distinct dedup_key
		  from watcher_findings
		 where profile_id = $1::uuid and status = 'dismissed'
	`, profileID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading dismissed signals: %w", err)
	}
	defer rows.Close()

	keys := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("postgres: reading a dismissed signal: %w", err)
		}
		keys[key] = true
	}
	return keys, rows.Err()
}

// emitSignal writes one signal, upserting on the open deduplication key.
//
// # THE SAME UPSERT `emit_watcher_finding` PERFORMED, SPELLED OUT
//
// One row per `(profile_id, dedup_key)` while it is open, refreshed in place
// when the condition is still true. That rule is the partial unique index's,
// and the index is a constraint that stays in Postgres: this statement names it
// and the database enforces it, which is the arrangement ENT-225 asks for.
//
// The organisation comes from the profile that was loaded under the GUC rather
// than from a lookup, which is what the plpgsql's `select org_id from
// compliance_profiles` was doing. If it were ever wrong the insert would be
// refused by the agent's WITH CHECK, so the tenancy boundary does not depend on
// this being right.
//
// `emit_watcher_finding` itself is not removed by this change. The agentic
// Watcher's RaiseSignal still calls it, and one writer is the property that
// file argues for at length; unifying the two is part of dropping the plpgsql,
// which this change deliberately does not do. See the CHANGELOG entry.
//
// # AND IT SAYS `detector`, ON BOTH HALVES OF THE UPSERT (ENT-273)
//
// `source` is stated rather than left to the column default, and it is stated
// in the `do update` too. That looks redundant and is what arms 00039's
// trigger: without the update line, a write landing on a row raised by the
// agent would replace its title and severity while leaving `source` reading
// `agent`, which is the observed failure with a column added and nothing else
// fixed. With it, this writer says who it is and the trigger refuses the
// transition. The `agent:` key prefix means it should never fire from the paths
// that exist today, which is what makes it a guard for the next writer.
func emitSignal(ctx context.Context, tx pgx.Tx, profile sweep.Profile, signal sweep.Signal) error {
	metadata, err := json.Marshal(signal.Metadata)
	if err != nil {
		return fmt.Errorf("postgres: encoding a signal's metadata: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		insert into watcher_findings (
			profile_id, org_id, kind, obligation_slug, severity,
			title, detail, dedup_key, metadata, source
		)
		values ($1::uuid, $2::uuid, $3, nullif($4, ''), $5, $6, nullif($7, ''), $8, $9::jsonb,
		        'detector')
		on conflict (profile_id, dedup_key) where status = 'open'
		do update set
			kind            = excluded.kind,
			obligation_slug = excluded.obligation_slug,
			severity        = excluded.severity,
			title           = excluded.title,
			detail          = excluded.detail,
			metadata        = excluded.metadata,
			source          = excluded.source,
			updated_at      = now()
	`,
		profile.ID, profile.OrgID, signal.Kind, signal.ObligationSlug, signal.Severity,
		signal.Title, signal.Detail, signal.DedupKey, metadata,
	); err != nil {
		return fmt.Errorf("postgres: writing a signal: %w", err)
	}
	return nil
}

// runAnalyst converts every open signal for the newest profile into a finding.
//
// Returns the number converted. `run_analyst()` returned the number of profiles
// it walked instead, which was always 1; see runWatcher for why that is
// corrected here rather than reproduced.
//
// A signal whose obligation slug does not resolve is SKIPPED and counted in
// neither direction, exactly as the plpgsql did, and now says why at a level
// somebody can actually read: `raise log` went to the Postgres log, where
// nobody operating this product is looking.
func runAnalyst(ctx context.Context, tx pgx.Tx, logger *slog.Logger) (int32, error) {
	var profileID, orgID string
	var dataCategories []string
	err := tx.QueryRow(ctx, `
		select id::text, org_id::text, data_categories
		  from compliance_profiles
		 order by created_at desc
		 limit 1
	`).Scan(&profileID, &orgID, &dataCategories)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("postgres: reading the profile to analyse: %w", err)
	}

	signals, err := openSignalsToAnalyse(ctx, tx, profileID)
	if err != nil {
		return 0, err
	}

	// Once for the sweep rather than once per finding: the facts are the
	// organisation's, they do not change inside this transaction, and a sweep
	// converting forty signals would otherwise ask forty times.
	facts, err := draftableFacts(ctx, tx, orgID)
	if err != nil {
		return 0, err
	}

	var converted int32
	for _, signal := range signals {
		obligation, found, err := citedObligation(ctx, tx, signal.slug)
		if err != nil {
			return 0, err
		}
		if !found {
			// Not an error and not a refusal to sweep. A signal citing a slug
			// the corpus does not carry becomes no finding, which is the only
			// safe outcome: a finding whose citation resolves to nothing is
			// the fabrication this product exists not to produce.
			logger.WarnContext(ctx, "skipped a signal citing an unknown obligation",
				"signal_id", signal.signal.ID, "obligation_slug", signal.slug)
			continue
		}

		finding := sweep.Convert(signal.signal, obligation, dataCategories)
		if err := writeFinding(ctx, tx, profileID, signal, obligation.ID, finding); err != nil {
			return 0, err
		}
		if err := draftPayload(ctx, tx, logger, signal, finding.ActionType, facts); err != nil {
			return 0, err
		}
		converted++
	}
	return converted, nil
}

// analysable is one open signal with the raw fields the write needs beside the
// judgement's inputs.
type analysable struct {
	signal   sweep.AnalysedSignal
	slug     string
	orgID    string
	metadata []byte
}

// openSignalsToAnalyse reads the signals waiting to become findings.
//
// Ordered by `created_at, id`, which is the plpgsql's order and is stable: two
// signals raised in the same transaction share a timestamp, and without the id
// the pair would convert in whichever order the planner chose.
func openSignalsToAnalyse(ctx context.Context, tx pgx.Tx, profileID string) ([]analysable, error) {
	rows, err := tx.Query(ctx, `
		select id::text, org_id::text, kind, title, dedup_key, severity,
		       coalesce(obligation_slug, ''), metadata,
		       (metadata ->> 'days_remaining')::int
		  from watcher_findings
		 where profile_id = $1::uuid and status = 'open'
		 order by created_at, id
	`, profileID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading open signals to analyse: %w", err)
	}
	defer rows.Close()

	var out []analysable
	for rows.Next() {
		var a analysable
		if err := rows.Scan(
			&a.signal.ID, &a.orgID, &a.signal.Kind, &a.signal.Title,
			&a.signal.DedupKey, &a.signal.Severity, &a.slug, &a.metadata,
			&a.signal.DaysRemaining,
		); err != nil {
			return nil, fmt.Errorf("postgres: reading an open signal: %w", err)
		}
		a.signal.MetadataJSON = string(a.metadata)
		out = append(out, a)
	}
	return out, rows.Err()
}

// citedObligation resolves the obligation a signal cites.
//
// The corpus has no `org_id` and no tenancy predicate; it is the same
// regulation for every customer. A slug that resolves to nothing returns
// found=false rather than an error, because the caller's response is to skip
// the signal and say so.
func citedObligation(ctx context.Context, tx pgx.Tx, slug string) (sweep.CitedObligation, bool, error) {
	if slug == "" {
		return sweep.CitedObligation{}, false, nil
	}

	var o sweep.CitedObligation
	err := tx.QueryRow(ctx, `
		select id::text, slug, summary, severity, action_type,
		       citation_kind, citation_celex, coalesce(citation_article, 0),
		       coalesce(citation_recital, 0), coalesce(citation_annex, ''),
		       coalesce(citation_paragraph, '')
		  from obligations
		 where slug = $1
	`, slug).Scan(
		&o.ID, &o.Slug, &o.Summary, &o.Severity, &o.ActionType,
		&o.Citation.Kind, &o.Citation.Celex, &o.Citation.ArticleNumber,
		&o.Citation.RecitalNumber, &o.Citation.AnnexLabel, &o.Citation.ParagraphLabel,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sweep.CitedObligation{}, false, nil
	}
	if err != nil {
		return sweep.CitedObligation{}, false, fmt.Errorf(
			"postgres: resolving a signal's obligation: %w", err)
	}
	return o, true, nil
}

// writeFinding upserts one finding on its signal.
//
// # WHAT REFRESHES AND WHAT IS PRESERVED (ENT-60, ENT-165)
//
// Three groups, and the difference between them is what a re-run is allowed to
// undo. `detected`, `proposed_action` and `narrative_generated_at` are
// PRESERVED: they are what the person was shown, and a sweep quietly rewriting
// the sentence somebody has already read is the product changing its story.
// `severity` refreshes because proximity genuinely changes as a date
// approaches. `action_type` refreshes with the obligation, so classifying an
// obligation reclassifies its open findings.
//
// Note what that deliberately does not do: refreshing `action_type` on a
// finding that was already approved fires no executor trigger, because those
// are `after update of status` and gated on the transition. Records are never
// created retroactively for decisions taken before the obligation was
// classified, which is the safe direction to be wrong in.
//
// # AND `metadata` MERGES RATHER THAN REPLACES, WHICH IS A FIX (ENT-287)
//
// This upsert used to set `metadata = excluded.metadata`, and
// `excluded.metadata` is built here out of exactly three keys: the signal's
// kind, its deduplication key and its own metadata. Everything else a finding
// had accumulated under `metadata` was therefore deleted by the next sweep.
//
// That was not theoretical and it was not cosmetic. `metadata.payload` is the
// record an approval creates (00036) and `metadata.approval_plan` is the
// provenance a person reads before approving (ENT-261). Both are written by
// other code paths, hours or days after the finding was raised, and both were
// silently erased by the next scheduled sweep, which is a customer's prepared
// record disappearing with nobody told.
//
// So the three signal keys refresh and everything else is preserved. Written
// as `existing || excluded` rather than the other way round because the
// excluded row is the fresh one and must win for the keys it carries.
func writeFinding(
	ctx context.Context, tx pgx.Tx, profileID string,
	signal analysable, obligationID string, finding sweep.Finding,
) error {
	if _, err := tx.Exec(ctx, `
		insert into findings (
			profile_id, org_id, watcher_finding_id, obligation_id, obligation_slug,
			detected, severity, proposed_action, regulatory_obligation, citation_url,
			supporting_context, effort_estimate, action_type, metadata
		)
		values (
			$1::uuid, $2::uuid, $3::uuid, $4::uuid, $5,
			$6, $7::public.severity_level, $8, nullif($9, ''), nullif($10, ''),
			$11, $12::public.effort_level, $13,
			jsonb_build_object(
				'signal_kind',      $14::text,
				'signal_dedup_key', $15::text,
				'signal_metadata',  $16::jsonb
			)
		)
		on conflict (watcher_finding_id) do update set
			obligation_id         = excluded.obligation_id,
			obligation_slug       = excluded.obligation_slug,
			severity              = excluded.severity,
			regulatory_obligation = excluded.regulatory_obligation,
			citation_url          = excluded.citation_url,
			supporting_context    = excluded.supporting_context,
			action_type           = excluded.action_type,
			metadata              = coalesce(findings.metadata, '{}'::jsonb) || excluded.metadata,
			updated_at            = now()
	`,
		profileID, signal.orgID, signal.signal.ID, obligationID, signal.slug,
		finding.Detected, finding.Severity, finding.ProposedAction,
		finding.RegulatoryObligation, finding.CitationURL,
		finding.SupportingContext, finding.Effort, finding.ActionType,
		signal.signal.Kind, signal.signal.DedupKey, signal.metadata,
	); err != nil {
		return fmt.Errorf("postgres: writing a finding: %w", err)
	}
	return nil
}

// The draft the sweep leaves on a finding (ENT-287).
//
// # WHY THE DRAFT IS WRITTEN HERE AND NOT WHEN SOMEBODY APPROVES
//
// Approving a `create_ropa` finding creates a `processing_activities` row out
// of `metadata.payload`, and until something puts a payload there it creates a
// row with a name taken from the finding and every other column empty. ENT-261
// built the path that fills it, and it only runs when a person presses "what
// approving will do", so a finding nobody explains is approved into the empty
// row.
//
// Drafting at execution instead was rejected: the Executor runs behind the
// event boundary, so a person would be approving an artefact nobody has read,
// and a refusal there has nowhere to go. `findings.CheckReceipt` and
// `findings.RequiresReview` both moved to the approval for that reason.
//
// # WHAT IT WILL NOT OVERWRITE, WHICH IS THE PART TO GET RIGHT
//
// Three guards, in the predicate rather than in Go, so a concurrent write
// loses the race rather than the row:
//
//	an existing payload    a person's or the Hands' proposal is theirs. This
//	                       fills an empty payload and never edits one.
//	an existing plan       same reasoning, one key over: a plan is what
//	                       somebody has already been shown.
//	an enqueued execution  `ErrAlreadyEnqueued`'s argument exactly (00036,
//	                       store/postgres/hands.go). The payload stops being a
//	                       proposal the moment something is going to act on it,
//	                       and a sweep running a second later must not rewrite
//	                       what a person approved.
//
// The status test is the console's own rule (`awaitingADecision`): a finding
// that has been approved or rejected is a decision that was taken, and its
// payload is a record of what was decided rather than a proposal.
//
// # AND IT IS VALIDATED, EVEN THOUGH IT IS DETERMINISTIC
//
// `ValidatePrepared` is the invariant `hands.go` describes: no finding's
// payload carries a field the register does not have or a value attributed to
// a fact this organisation never recorded, WHOEVER wrote it. A deterministic
// drafter is easier to trust than a model and that is not a reason to exempt
// it, because the thing being protected is the record rather than the writer.
// A draft that fails is skipped with a warning and the sweep carries on: a
// finding with no payload is the state we are improving on, not a reason to
// fail the sweep that raised it.
func draftPayload(
	ctx context.Context, tx pgx.Tx, logger *slog.Logger,
	signal analysable, actionType string, facts []records.Fact,
) error {
	register, creates := records.RegisterFor(actionType)
	if !creates {
		// `review`, which is most findings: approving records the decision and
		// creates nothing, so there is nothing to propose and nothing about
		// this finding changes.
		return nil
	}

	draft := records.DraftFromFacts(register, facts)
	if draft.Empty() {
		// Nothing could be filled honestly. Deliberately not written as a plan
		// of six empty columns: that is a proposal to create an empty record,
		// and the person is better served by the Hands, which can reach what a
		// mapping cannot.
		return nil
	}

	known := make(map[string]bool, len(facts))
	for _, f := range facts {
		known[f.Key] = true
	}
	if err := records.ValidatePrepared(register, draft.Fields, known); err != nil {
		logger.WarnContext(ctx, "skipped a drafted payload the validator refused",
			"signal_id", signal.signal.ID, "action_type", actionType, "error", err)
		return nil
	}

	payload := map[string]any{}
	for _, f := range draft.Fields {
		field, known := register.Field(f.Name)
		if !known {
			// Unreachable: the validator above refuses a field the register
			// does not have. Skipped rather than written, for the reason
			// `PrepareRecord` gives: the one thing worse than refusing an
			// unknown field is writing it into a customer's proposed record.
			continue
		}
		payload[f.Name] = payloadValue(field, f)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("postgres: encoding a drafted payload: %w", err)
	}

	left := make([]map[string]any, 0, len(draft.LeftForYou))
	for _, l := range draft.LeftForYou {
		left = append(left, map[string]any{"name": l.Name, "why": l.Why})
	}
	planJSON, err := json.Marshal(map[string]any{
		"prepared_at": time.Now().UTC().Format(time.RFC3339),
		// WHICH WRITER PRODUCED THIS, WHICH A PERSON IS ENTITLED TO KNOW.
		// A plan a model wrote and a plan an organisation's own onboarding
		// answers wrote are not owed the same trust, and a surface that
		// rendered them identically would be deciding that for the customer.
		"source":       records.DraftSource,
		"explanation":  draft.Explanation(register),
		"fields":       provenanceFields(draft.Fields),
		"left_for_you": left,
	})
	if err != nil {
		return fmt.Errorf("postgres: encoding a drafted plan: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		update findings f
		   set metadata = coalesce(f.metadata, '{}'::jsonb)
		                  || jsonb_build_object('payload', $2::jsonb, 'approval_plan', $3::jsonb)
		 where f.watcher_finding_id = $1::uuid
		   and f.status not in ('approved', 'rejected')
		   and coalesce(f.metadata -> 'payload', '{}'::jsonb) = '{}'::jsonb
		   and f.metadata -> 'approval_plan' is null
		   and not exists (
		         select 1 from executor_jobs j where j.finding_id = f.id
		       )
	`, signal.signal.ID, payloadJSON, planJSON); err != nil {
		return fmt.Errorf("postgres: writing a drafted payload: %w", err)
	}
	return nil
}

// draftableFacts reads what the organisation has told us about itself, as the
// drafter takes it.
//
// `org_id` in the predicate, not left to the policy, for the reason
// `watchedFacts` gives at length: the producer's select policy on
// `org_profile_facts` is `using (true)` (00023), so the scoping here is the
// query's. A sweep that read every organisation's facts would draft one
// customer's record out of another's answers, which is the worst failure this
// file could have.
//
// Open facts only (`valid_to is null`): a fact that has been superseded is
// what the organisation used to believe, and a record drafted from it would be
// current in every way except the one that matters.
func draftableFacts(ctx context.Context, tx pgx.Tx, orgID string) ([]records.Fact, error) {
	rows, err := tx.Query(ctx, `
		select key, value::text
		  from org_profile_facts
		 where org_id = $1::uuid and valid_to is null
		 order by key
	`, orgID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading the facts to draft from: %w", err)
	}
	defer rows.Close()

	var facts []records.Fact
	for rows.Next() {
		var key, valueJSON string
		if err := rows.Scan(&key, &valueJSON); err != nil {
			return nil, fmt.Errorf("postgres: reading a fact to draft from: %w", err)
		}
		// `decodeValues` is the Hands' decoder (store/postgres/hands.go), used
		// here rather than a second one: a text fact is one value, a list fact
		// is several, and anything else decodes to nothing rather than to its
		// JSON spelling. A number rendered as "4" into a ROPA column would be
		// a value the plan claims was recorded in that form, and it was not.
		facts = append(facts, records.Fact{Key: key, Values: decodeValues([]byte(valueJSON))})
	}
	return facts, rows.Err()
}
