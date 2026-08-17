package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/notify"
)

// Notification preferences and the doorbell path (ENT-209, migration 00015).

// Preferences reads the caller's own settings for the active organisation.
//
// Returns defaults when no row exists, rather than pgx.ErrNoRows. Somebody who
// has never opened the settings page has no row and is nonetheless subscribed:
// the product default is that a member is told. Reporting that as "not found"
// would make "I have not changed anything" indistinguishable from a fault, and
// would push the defaulting into every caller.
//
// The row is found by `user_id = t.userID` as well as by policy. Belt and
// braces on purpose: the policy already pins it, and a query that also says so
// cannot accidentally read a colleague's row if that policy is ever loosened.
func (t *Tenant) Preferences(ctx context.Context) (notify.Preferences, error) {
	prefs := notify.Defaults()

	var (
		email      *string
		timezone   *string
		quietStart *string
		quietEnd   *string
	)

	err := t.tx.QueryRow(ctx, `
		select coalesce(email, ''),
		       min_severity_for_email::text,
		       weekly_briefing_enabled,
		       deadline_alerts_enabled,
		       timezone,
		       to_char(quiet_hours_start, 'HH24:MI'),
		       to_char(quiet_hours_end, 'HH24:MI')
		  from notification_preferences
		 where org_id = $1 and user_id = $2
	`, t.orgID, t.userID).Scan(
		&email, &prefs.MinSeverityForEmail, &prefs.WeeklyBriefingEnabled,
		&prefs.DeadlineAlertsEnabled, &timezone, &quietStart, &quietEnd,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return prefs, nil
	}
	if err != nil {
		return notify.Preferences{}, fmt.Errorf("postgres: reading notification preferences: %w", err)
	}

	prefs.Email = deref(email)
	if tz := deref(timezone); tz != "" {
		prefs.Timezone = tz
	}
	prefs.QuietHoursStart = deref(quietStart)
	prefs.QuietHoursEnd = deref(quietEnd)
	return prefs, nil
}

// SavePreferences replaces the caller's settings for the active organisation.
//
// An upsert, because the row's absence is the common case rather than an error:
// the first time anybody changes anything is also the first time a row exists.
//
// Empty strings become SQL nulls for the nullable columns. The distinction
// matters for `email`, where null means "use the address I sign in with" and an
// empty string would mean an address that is empty, and for the quiet hours,
// where null means "no quiet window" and `00:00` is a real time.
func (t *Tenant) SavePreferences(ctx context.Context, p notify.Preferences) error {
	_, err := t.tx.Exec(ctx, `
		insert into notification_preferences
			(org_id, user_id, email, min_severity_for_email,
			 weekly_briefing_enabled, deadline_alerts_enabled, timezone,
			 quiet_hours_start, quiet_hours_end)
		values ($1, $2, $3, $4::public.severity_level, $5, $6, $7, $8::time, $9::time)
		on conflict (org_id, user_id) do update
		   set email                   = excluded.email,
		       min_severity_for_email  = excluded.min_severity_for_email,
		       weekly_briefing_enabled = excluded.weekly_briefing_enabled,
		       deadline_alerts_enabled = excluded.deadline_alerts_enabled,
		       timezone                = excluded.timezone,
		       quiet_hours_start       = excluded.quiet_hours_start,
		       quiet_hours_end         = excluded.quiet_hours_end
	`,
		t.orgID, t.userID, nullIfEmpty(p.Email), p.MinSeverityForEmail,
		p.WeeklyBriefingEnabled, p.DeadlineAlertsEnabled, p.Timezone,
		nullIfEmpty(p.QuietHoursStart), nullIfEmpty(p.QuietHoursEnd),
	)
	if err != nil {
		return fmt.Errorf("postgres: saving notification preferences: %w", err)
	}
	return nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// Doorbell is one pending notification_outbox row.
type Doorbell struct {
	ID        string
	OrgID     string
	FindingID string
	Attempts  int
}

// Recipient is one candidate for a doorbell, with the raw preference fields the
// decision needs.
//
// Raw, not decided. The database fetches and Go compares (§14.5): putting the
// "should this person be emailed" rule in the SQL would hide a product decision
// somewhere it cannot be unit tested.
type Recipient struct {
	UserID          string
	Email           string
	DisplayName     string
	MinSeverity     string
	FindingSeverity string
	Timezone        string
	QuietHoursStart string
	QuietHoursEnd   string
	// Repeated on every row. The agent has no grant on `organisations`, and
	// giving it one to save a join would reopen the argument 00015's
	// `notification_recipients` exists to close.
	OrgSlug string
	OrgName string
}

// ClaimDoorbell takes the oldest pending notification for delivery.
//
// Same shape as the transactional outbox's drain and for the same reasons: the
// row lock only exists inside a transaction, so the caller is handed a
// transaction to work in rather than a row it thinks is reserved. See the
// header on `Drain` in outbox.go.
//
// Returns pgx.ErrNoRows when the queue is empty, which the caller reads as
// "nothing to do" rather than as a fault.
func (a *AgentStore) ClaimDoorbell(ctx context.Context, tx pgx.Tx) (Doorbell, error) {
	var d Doorbell
	err := tx.QueryRow(ctx, `
		select id::text, org_id::text, finding_id::text, attempts
		  from notification_outbox
		 where status = 'pending'
		 order by created_at
		 limit 1
		 for update skip locked
	`).Scan(&d.ID, &d.OrgID, &d.FindingID, &d.Attempts)
	if err != nil {
		return Doorbell{}, err
	}
	return d, nil
}

// Recipients answers who has asked to hear about one doorbell.
//
// Goes through `notification_recipients`, a SECURITY DEFINER function, rather
// than through table grants. The agent role deliberately holds nothing on
// memberships, preferences or identities (00008), and granting it those would
// mean a compromised agent could enumerate every person in the deployment. The
// function answers one question about one row instead. See 00015's header.
func (a *AgentStore) Recipients(ctx context.Context, tx pgx.Tx, outboxID string) ([]Recipient, error) {
	rows, err := tx.Query(ctx, `
		select user_id::text, email, coalesce(display_name, ''),
		       min_severity::text, finding_severity::text, timezone,
		       coalesce(to_char(quiet_hours_start, 'HH24:MI'), ''),
		       coalesce(to_char(quiet_hours_end, 'HH24:MI'), ''),
		       org_slug, org_name
		  from notification_recipients($1::uuid)
	`, outboxID)
	if err != nil {
		return nil, fmt.Errorf("postgres: resolving notification recipients: %w", err)
	}
	defer rows.Close()

	var out []Recipient
	for rows.Next() {
		var r Recipient
		if err := rows.Scan(&r.UserID, &r.Email, &r.DisplayName, &r.MinSeverity,
			&r.FindingSeverity, &r.Timezone, &r.QuietHoursStart, &r.QuietHoursEnd,
			&r.OrgSlug, &r.OrgName); err != nil {
			return nil, fmt.Errorf("postgres: reading a recipient: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading recipients: %w", err)
	}
	return out, nil
}

// Finding reads the parts of a finding a notification mentions.
//
// Deliberately narrow: a doorbell says that something happened, not what it says
// (§17.1). The recipient follows a link and reads the finding behind their own
// session, where their role and their organisation are checked again. Putting
// the detected text or the proposed action in an email would move a customer's
// compliance exposure into their mailbox and their mail provider's logs.
func (a *AgentStore) FindingSummary(ctx context.Context, tx pgx.Tx, findingID string) (string, string, error) {
	var severity, slug string
	err := tx.QueryRow(ctx, `
		select severity::text, coalesce(obligation_slug, '')
		  from findings where id = $1::uuid
	`, findingID).Scan(&severity, &slug)
	if err != nil {
		return "", "", fmt.Errorf("postgres: reading the finding for a notification: %w", err)
	}
	return severity, slug, nil
}

// MintCapabilityToken stores the hash of a link that acts without a session.
//
// The raw token never reaches this function. Hashing happens at the call site
// through the same helper the redeem path uses, so the two cannot drift, which
// is the arrangement `invitations` already has.
func (a *AgentStore) MintCapabilityToken(
	ctx context.Context, tx pgx.Tx, orgID, userID, kind, tokenHash string, lifetime string,
) error {
	_, err := tx.Exec(ctx, `
		insert into capability_tokens (org_id, kind, token_hash, user_id, expires_at)
		values ($1::uuid, $2, $3, $4::uuid, now() + $5::interval)
	`, orgID, kind, tokenHash, userID, lifetime)
	if err != nil {
		return fmt.Errorf("postgres: minting a capability token: %w", err)
	}
	return nil
}

// MarkDoorbellSent records that a notification went out.
func (a *AgentStore) MarkDoorbellSent(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `
		update notification_outbox
		   set status = 'sent', sent_at = now(), attempts = attempts + 1, last_error = null
		 where id = $1::uuid and status = 'pending'
	`, id)
	if err != nil {
		return fmt.Errorf("postgres: marking a notification sent: %w", err)
	}
	return nil
}

// MarkDoorbellSkipped records that nobody wanted this one.
//
// `skipped` rather than `sent`, and the distinction is not cosmetic: `sent`
// means an email left the building. A doorbell that matched nobody's
// preferences is a legitimate outcome and has to be terminal, or the dispatcher
// re-examines it forever, but recording it as a delivery would put a claim in
// the database that no message supports.
func (a *AgentStore) MarkDoorbellSkipped(ctx context.Context, tx pgx.Tx, id, reason string) error {
	_, err := tx.Exec(ctx, `
		update notification_outbox
		   set status = 'skipped', attempts = attempts + 1, last_error = $2
		 where id = $1::uuid and status = 'pending'
	`, id, reason)
	if err != nil {
		return fmt.Errorf("postgres: marking a notification skipped: %w", err)
	}
	return nil
}

// MarkDoorbellFailed records an attempt that did not deliver, leaving the row
// pending so the next drain retries it.
func (a *AgentStore) MarkDoorbellFailed(ctx context.Context, tx pgx.Tx, id string, cause error) error {
	_, err := tx.Exec(ctx, `
		update notification_outbox
		   set attempts = attempts + 1, last_error = $2
		 where id = $1::uuid
	`, id, cause.Error())
	if err != nil {
		return fmt.Errorf("postgres: recording a failed notification: %w", err)
	}
	return nil
}

// Begin opens a transaction on the agent pool, for callers that need several
// statements under one row lock.
func (a *AgentStore) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: beginning an agent transaction: %w", err)
	}
	return tx, nil
}

// RedeemCapabilityToken spends a link and applies what it authorises.
//
// On the application pool, not the agent's, because redemption happens on a
// request from a browser. It runs through a SECURITY DEFINER function because
// the caller has no session and therefore no tenancy GUCs: there is no
// organisation set, no user set, and every policy in the schema would refuse.
//
// Returns the organisation the token named, or ErrNoSuchToken for expired,
// already redeemed, wrong kind and never existed alike. Collapsing those is
// deliberate: distinguishing them would make this an oracle for which tokens
// are real, to a caller who has proved nothing.
// Takes the raw token and hashes it here, with the same function the mint side
// uses, so the two halves cannot drift. That is the arrangement `invitations`
// already has.
func (s *Store) RedeemCapabilityToken(ctx context.Context, token, kind string) (string, error) {
	var orgID *string
	err := s.pool.QueryRow(ctx,
		`select redeem_capability_token($1, $2)::text`,
		HashInvitationToken(token), kind).Scan(&orgID)
	if err != nil {
		return "", fmt.Errorf("postgres: redeeming a capability token: %w", err)
	}
	if orgID == nil {
		return "", ErrNoSuchToken
	}
	return *orgID, nil
}

// ErrNoSuchToken is the single answer for every unusable token.
var ErrNoSuchToken = errors.New("postgres: no such capability token")
