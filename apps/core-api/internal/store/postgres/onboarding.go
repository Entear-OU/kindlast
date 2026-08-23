package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/onboarding"
)

// The first conversation, on the request's transaction (ENT-212).
//
// # NO ORG PREDICATE, AND HERE THAT MEANS RLS
//
// Same rule as `memory.go`: both onboarding tables carry `org_id`, FORCE ROW
// LEVEL SECURITY and the two-GUC policies from 00002, so every query below is
// already scoped by the database. A `where org_id = $1` added here would be a
// second, weaker copy of a check the policies already make.
//
// # EVERY TURN IS A ROW BEFORE IT IS A RESPONSE
//
// The whole reason this service exists rather than a browser talking to a model
// is that a conversation nobody persisted is a conversation a refresh destroys.
// So there is no path through this file where a question is asked or an answer
// accepted without a row, and the ordering is assigned by the database in the
// same statement that inserts, so two tabs racing collide on
// `onboarding_messages_session_id_ordering_key` rather than interleaving.

// ErrNoOnboardingSession is returned when an organisation has never started.
//
// Distinct from an empty session, because "we have not begun" and "we began and
// said nothing" want different screens: one offers a start button and the other
// resumes.
var ErrNoOnboardingSession = errors.New("postgres: this organisation has no onboarding session")

const onboardingTurnColumns = `
	id::text,
	role,
	content,
	coalesce(fact_key, ''),
	coalesce(fact_value::text, ''),
	ordering,
	created_at,
	coalesce(created_by::text, '')
`

// OnboardingSession returns the organisation's most recent interview.
func (t *Tenant) OnboardingSession(ctx context.Context) (onboarding.Session, error) {
	var session onboarding.Session
	err := t.tx.QueryRow(ctx, `
		select id::text, status, started_at, completed_at
		  from public.onboarding_sessions
		 order by started_at desc
		 limit 1`).Scan(
		&session.ID, &session.Status, &session.StartedAt, &session.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return onboarding.Session{}, ErrNoOnboardingSession
	}
	if err != nil {
		return onboarding.Session{}, fmt.Errorf("postgres: reading the onboarding session: %w", err)
	}
	return session, nil
}

// StartOnboardingSession opens the interview, or hands back the open one.
//
// # IT DOES NOT RE-OPEN A COMPLETED INTERVIEW
//
// A completed session is returned as it is, with `created` false. Starting a
// second interview would be a reasonable feature and is not this one: it would
// leave an organisation with two sessions and two profile rows, the console
// would have to decide which is the real one, and the person who clicked
// "start" expecting to review their answers would instead be asked all eleven
// questions again. Correcting a fact is what changing an answer looks like once
// onboarding is done, and that surface already exists.
func (t *Tenant) StartOnboardingSession(ctx context.Context) (onboarding.Session, bool, error) {
	existing, err := t.OnboardingSession(ctx)
	switch {
	case err == nil && existing.Status != onboarding.StatusAbandoned:
		return existing, false, nil
	case err != nil && !errors.Is(err, ErrNoOnboardingSession):
		return onboarding.Session{}, false, err
	}

	var session onboarding.Session
	if err := t.tx.QueryRow(ctx, `
		insert into public.onboarding_sessions (org_id, created_by, status)
		values (
			(select current_setting('app.current_org_id')::uuid),
			nullif($1, '')::uuid,
			$2
		)
		returning id::text, status, started_at, completed_at`,
		t.userID, onboarding.StatusInProgress,
	).Scan(&session.ID, &session.Status, &session.StartedAt, &session.CompletedAt); err != nil {
		return onboarding.Session{}, false, fmt.Errorf("postgres: opening an onboarding session: %w", err)
	}
	return session, true, nil
}

// OnboardingTranscript returns every turn, oldest first.
//
// Ordered by `ordering` rather than `created_at`, because two turns written in
// the same transaction share a transaction timestamp and would tie. The unique
// index on `(session_id, ordering)` is what makes this a total order.
func (t *Tenant) OnboardingTranscript(ctx context.Context, sessionID string) ([]onboarding.Turn, error) {
	rows, err := t.tx.Query(ctx, `
		select `+onboardingTurnColumns+`
		  from public.onboarding_messages
		 where session_id = $1::uuid
		 order by ordering`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading the onboarding transcript: %w", err)
	}
	defer rows.Close()

	turns := make([]onboarding.Turn, 0, 32)
	for rows.Next() {
		var turn onboarding.Turn
		if err := rows.Scan(
			&turn.ID,
			&turn.Role,
			&turn.Content,
			&turn.Key,
			&turn.ValueJSON,
			&turn.Ordering,
			&turn.CreatedAt,
			&turn.CreatedBy,
		); err != nil {
			return nil, fmt.Errorf("postgres: scanning an onboarding turn: %w", err)
		}
		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

// AppendOnboardingTurn records one line of the interview.
//
// # THE ORDERING IS THE DATABASE'S, IN THE SAME STATEMENT
//
// Reading the maximum and then inserting one more would be two statements with
// a window between them, and two tabs answering at once would produce two turns
// claiming the same position. Computing it in the insert closes the window: the
// loser collides on `onboarding_messages_session_id_ordering_key` and gets an
// error it can retry, rather than silently overwriting a position.
//
// `created_by` comes from the verified subject on the transaction, never from
// the request, so a turn cannot be attributed to a colleague.
func (t *Tenant) AppendOnboardingTurn(
	ctx context.Context,
	sessionID, role, content, factKey, factValueJSON string,
) (onboarding.Turn, error) {
	var turn onboarding.Turn
	if err := t.tx.QueryRow(ctx, `
		insert into public.onboarding_messages
			(org_id, session_id, created_by, role, content, ordering, fact_key, fact_value)
		select (select current_setting('app.current_org_id')::uuid),
		       $1::uuid,
		       nullif($2, '')::uuid,
		       $3,
		       $4,
		       coalesce(max(ordering), -1) + 1,
		       nullif($5, ''),
		       nullif($6, '')::jsonb
		  from public.onboarding_messages
		 where session_id = $1::uuid
		returning `+onboardingTurnColumns,
		sessionID, t.userID, role, content, factKey, factValueJSON,
	).Scan(
		&turn.ID,
		&turn.Role,
		&turn.Content,
		&turn.Key,
		&turn.ValueJSON,
		&turn.Ordering,
		&turn.CreatedAt,
		&turn.CreatedBy,
	); err != nil {
		return onboarding.Turn{}, fmt.Errorf("postgres: recording an onboarding turn: %w", err)
	}
	return turn, nil
}

// HasComplianceProfile reports whether the Watcher has anything to read.
//
// The question every authenticated route asks before deciding whether to route
// a person into onboarding. Deliberately about the profile rather than about a
// completed session: a profile could arrive another way, and what decides
// whether the console has anything to show is the profile.
func (t *Tenant) HasComplianceProfile(ctx context.Context) (bool, error) {
	var exists bool
	if err := t.tx.QueryRow(ctx,
		`select exists (select 1 from public.compliance_profiles)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("postgres: looking for a compliance profile: %w", err)
	}
	return exists, nil
}

// ConfirmOnboarding records the interview's answers as what the organisation
// believes, and marks the session finished.
//
// # THIS IS THE ONLY PLACE ONBOARDING WRITES A FACT
//
// Until it is called, the interview is a conversation: rows in
// `onboarding_messages` and nothing else. No fact exists, no profile row
// exists, and therefore no finding can have been reasoned from any of it. That
// is how "the person sees and confirms the profile before it drives anything"
// is enforced structurally rather than by a screen somebody could skip.
//
// # THE FACTS ARE THE RECORD; THE PROFILE ROW IS A PROJECTION
//
// Each answer goes through `CorrectFact` with `source = 'onboarding'`, which is
// the same close-then-insert path a human correction takes. Reusing it is the
// point rather than a shortcut: onboarding is the organisation memory's first
// feeder, so its writes must be the same writes, with the same provenance, the
// same history and the same `recorded_by`.
//
// Then `compliance_profiles` is written from the full set of open facts, not
// only from this session's answers, so a fact corrected before confirmation is
// not overwritten by an older answer.
//
// # IDEMPOTENT, IN BOTH THE WAYS THAT BITE
//
// Confirming twice writes no second profile: `compliance_profiles_session_id_key`
// makes the insert an upsert on the session. And a fact confirmed at the value
// it already holds writes no history row, because `CorrectFact` reports no
// change and does nothing. So a double-clicked button, a retried request and a
// person reloading the confirmation page all leave one profile and one history
// entry per answer.
func (t *Tenant) ConfirmOnboarding(
	ctx context.Context,
	sessionID string,
	facts map[string]string,
) (string, error) {
	for _, fact := range onboarding.OrderedFacts(facts) {
		if _, _, err := t.CorrectFact(ctx, fact.Key, fact.ValueJSON, "onboarding", ""); err != nil {
			return "", err
		}
	}

	profileID, err := t.writeProfileProjection(ctx, sessionID)
	if err != nil {
		return "", err
	}

	if _, err := t.tx.Exec(ctx, `
		update public.onboarding_sessions
		   set status = $2,
		       completed_at = coalesce(completed_at, now()),
		       updated_at = now()
		 where id = $1::uuid`, sessionID, onboarding.StatusCompleted); err != nil {
		return "", fmt.Errorf("postgres: completing the onboarding session: %w", err)
	}

	// The trigger ENT-212 shipped without (00035): in the same transaction as
	// the facts and the completed session, so the relay can never list this
	// row before the profile it names is visible to any other connection. See
	// the migration header for why that ordering has to be structural rather
	// than "the worker waits a bit".
	if err := t.EnqueueSweepTrigger(ctx, onboarding.ReasonOnboardingConfirmed); err != nil {
		return "", err
	}

	return profileID, nil
}

// openFactValues is every fact the organisation currently believes, as JSON.
func (t *Tenant) openFactValues(ctx context.Context) (map[string]string, error) {
	stored, err := t.ProfileFacts(ctx)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(stored))
	for _, fact := range stored {
		values[fact.Key] = fact.ValueJSON
	}
	return values, nil
}

// writeProfileProjection inserts or refreshes the row the Watcher reads.
//
// Upsert on `session_id`, which the unique constraint from 00001 already
// enforces, so a second confirmation of the same session refreshes rather than
// duplicating.
func (t *Tenant) writeProfileProjection(ctx context.Context, sessionID string) (string, error) {
	values, err := t.openFactValues(ctx)
	if err != nil {
		return "", err
	}
	projected, err := onboarding.Project(values)
	if err != nil {
		return "", err
	}

	var profileID string
	if err := t.tx.QueryRow(ctx, `
		insert into public.compliance_profiles (
			org_id, session_id, created_by,
			industry, eu_jurisdictions, data_categories, data_subjects,
			ai_systems, has_dpo, has_ropa, transfers_outside_eu,
			transfer_destinations, vendor_list, staff_count
		) values (
			(select current_setting('app.current_org_id')::uuid),
			$1::uuid, nullif($2, '')::uuid,
			$3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
		on conflict (session_id) do update set
			industry = excluded.industry,
			eu_jurisdictions = excluded.eu_jurisdictions,
			data_categories = excluded.data_categories,
			data_subjects = excluded.data_subjects,
			ai_systems = excluded.ai_systems,
			has_dpo = excluded.has_dpo,
			has_ropa = excluded.has_ropa,
			transfers_outside_eu = excluded.transfers_outside_eu,
			transfer_destinations = excluded.transfer_destinations,
			vendor_list = excluded.vendor_list,
			staff_count = excluded.staff_count,
			updated_at = now()
		returning id::text`,
		sessionID, t.userID,
		projected.Industry,
		projected.EUJurisdictions,
		projected.DataCategories,
		projected.DataSubjects,
		projected.AISystems,
		projected.HasDPO,
		projected.HasROPA,
		projected.TransfersOutsideEU,
		projected.TransferDestinations,
		projected.VendorList,
		projected.StaffCount,
	).Scan(&profileID); err != nil {
		return "", fmt.Errorf("postgres: writing the compliance profile: %w", err)
	}
	return profileID, nil
}

// refreshProfileProjection keeps the Watcher's view in step with a fact change.
//
// # WHY A CORRECTION HAS TO REACH THIS TABLE AT ALL
//
// `run_watcher()` reads `compliance_profiles`, in plpgsql, and knows nothing
// about `org_profile_facts`. Without this, a customer who corrects "do you keep
// a record of processing activities" from unsure to yes on the memory page
// would watch the console agree with them and the Watcher go on raising the
// same gap forever. Two representations of one thing is a real cost and a
// temporary one; the only safe arrangement while both exist is that changing
// one changes the other in the same transaction.
//
// A no-op when the organisation has no profile row yet, which is the state
// during onboarding itself: the row is written once, at confirmation, from the
// complete set of facts.
func (t *Tenant) refreshProfileProjection(ctx context.Context) error {
	var sessionID string
	err := t.tx.QueryRow(ctx, `
		select session_id::text
		  from public.compliance_profiles
		 order by created_at desc
		 limit 1`).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("postgres: finding the profile to refresh: %w", err)
	}

	_, err = t.writeProfileProjection(ctx, sessionID)
	return err
}
