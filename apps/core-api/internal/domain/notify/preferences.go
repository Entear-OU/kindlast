package notify

import (
	"fmt"
	"strings"
	"time"
)

// Notification preferences and the rule that decides who hears about a finding
// (ENT-209).
//
// Pure functions over already-loaded data. The database fetches candidates and
// this decides, which is §14.5's split: Postgres keeps invariants, Go decides.
// The alternative, filtering inside `notification_recipients`, would put a
// product decision in plpgsql where it cannot be exercised without a database
// and where a later reader can disagree with it silently.

// Preferences is one person's settings within one organisation.
type Preferences struct {
	Email                 string
	MinSeverityForEmail   string
	WeeklyBriefingEnabled bool
	DeadlineAlertsEnabled bool
	Timezone              string
	QuietHoursStart       string
	QuietHoursEnd         string
}

// DefaultTimezone matches the schema's default so a row written here and a row
// written by the database agree.
const DefaultTimezone = "Europe/Tallinn"

// Defaults are what somebody who has never opened the settings page gets.
//
// Subscribed, at medium and above. The opposite default is the tempting one and
// it is wrong for this product: a compliance workspace whose members hear
// nothing until each individually opts in is one where a critical finding sits
// unread, and the customer's first sign that the product works is an
// enforcement letter.
func Defaults() Preferences {
	return Preferences{
		MinSeverityForEmail:   SeverityMedium,
		WeeklyBriefingEnabled: true,
		DeadlineAlertsEnabled: true,
		Timezone:              DefaultTimezone,
	}
}

// The severity ladder, lowest first. Matches the `severity_level` enum, and the
// order is the whole of the comparison below.
const (
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

var severityRank = map[string]int{
	SeverityLow:      1,
	SeverityMedium:   2,
	SeverityHigh:     3,
	SeverityCritical: 4,
}

// ValidSeverity reports whether a value is one of the four.
func ValidSeverity(value string) bool {
	_, ok := severityRank[value]
	return ok
}

// MeetsSeverity reports whether a finding clears somebody's floor.
//
// An unknown severity on either side counts as clearing it. That is deliberate
// and is the safe direction: the alternative is that a value this code does not
// recognise silently suppresses a notification, and the most likely reason for
// an unrecognised severity is that somebody added one above `critical`.
func MeetsSeverity(findingSeverity, floor string) bool {
	found, ok := severityRank[findingSeverity]
	if !ok {
		return true
	}
	want, ok := severityRank[floor]
	if !ok {
		return true
	}
	return found >= want
}

// Normalise fills in anything empty and rejects anything nonsensical.
//
// Returns an error rather than silently correcting, because these arrive from a
// client and a request that says `min_severity_for_email: "urgent"` is a
// mistake worth reporting rather than quietly reading as medium.
func (p Preferences) Normalise() (Preferences, error) {
	out := p

	if out.MinSeverityForEmail == "" {
		out.MinSeverityForEmail = SeverityMedium
	}
	if !ValidSeverity(out.MinSeverityForEmail) {
		return Preferences{}, fmt.Errorf(
			"min_severity_for_email must be one of %s, %s, %s or %s",
			SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical)
	}

	out.Timezone = strings.TrimSpace(out.Timezone)
	if out.Timezone == "" {
		out.Timezone = DefaultTimezone
	}
	if _, err := time.LoadLocation(out.Timezone); err != nil {
		// Checked here rather than at send time. An unloadable zone at send
		// time is a notification that fails hours later for a reason nobody
		// connects to a settings page they were on this morning.
		return Preferences{}, fmt.Errorf("timezone %q is not a known IANA name", out.Timezone)
	}

	start, err := normaliseClock(out.QuietHoursStart)
	if err != nil {
		return Preferences{}, fmt.Errorf("quiet_hours_start: %w", err)
	}
	end, err := normaliseClock(out.QuietHoursEnd)
	if err != nil {
		return Preferences{}, fmt.Errorf("quiet_hours_end: %w", err)
	}

	// One end alone is not a window. Rejected rather than half-applied, because
	// "quiet from 22:00" with no end reads to a person as "quiet overnight" and
	// would behave as no quiet hours at all.
	if (start == "") != (end == "") {
		return Preferences{}, fmt.Errorf("quiet hours need both a start and an end, or neither")
	}
	out.QuietHoursStart, out.QuietHoursEnd = start, end

	out.Email = strings.TrimSpace(out.Email)
	if out.Email != "" && !strings.Contains(out.Email, "@") {
		// Deliberately shallow, matching InviteMember: anything stricter
		// rejects addresses that are valid under RFC 5321, and the
		// authoritative test of an address is whether mail to it arrives.
		return Preferences{}, fmt.Errorf("email must be an address")
	}

	return out, nil
}

func normaliseClock(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if _, err := time.Parse("15:04", trimmed); err != nil {
		return "", fmt.Errorf("%q is not a time of day in HH:MM", trimmed)
	}
	return trimmed, nil
}

// InQuietHours reports whether a local time falls inside somebody's quiet
// window.
//
// Windows that wrap midnight are the normal case, not the exception: 22:00 to
// 07:00 is what people actually set. A naive `start <= now && now < end`
// comparison silently means "never" for those, which is the bug this function
// exists to not have.
func InQuietHours(now time.Time, start, end string) bool {
	if start == "" || end == "" {
		return false
	}

	from, err := time.Parse("15:04", start)
	if err != nil {
		return false
	}
	to, err := time.Parse("15:04", end)
	if err != nil {
		return false
	}

	minutes := now.Hour()*60 + now.Minute()
	fromMin := from.Hour()*60 + from.Minute()
	toMin := to.Hour()*60 + to.Minute()

	if fromMin == toMin {
		// A zero-length window. Read as "no quiet hours" rather than "always
		// quiet", because the latter silences somebody permanently through what
		// is almost certainly a typo.
		return false
	}
	if fromMin < toMin {
		return minutes >= fromMin && minutes < toMin
	}
	// Wraps midnight.
	return minutes >= fromMin || minutes < toMin
}

// ShouldNotify decides whether one person hears about one finding, and says why
// not when they do not.
//
// The reason is returned rather than logged because it ends up in the outbox
// row's `last_error` when nobody at all wanted a notification, and an operator
// asking "why did nothing go out" deserves an answer better than silence.
func ShouldNotify(findingSeverity, floor string, now time.Time, quietStart, quietEnd string) (bool, string) {
	if !MeetsSeverity(findingSeverity, floor) {
		return false, fmt.Sprintf("severity %s is below the %s floor", findingSeverity, floor)
	}
	if InQuietHours(now, quietStart, quietEnd) {
		return false, "inside quiet hours"
	}
	return true, ""
}

// Doorbell is what one finding notification needs in order to be rendered.
//
// A struct rather than six positional strings, because four of them are text
// that all looks alike at a call site and two of them are links that must not
// be swapped: putting the approve link where the unsubscribe link goes would
// turn "stop sending me these" into an approval.
type Doorbell struct {
	RecipientEmail string
	OrgName        string
	Severity       string
	FindingURL     string
	UnsubscribeURL string
	// ApproveURL is section 8's one-tap link, and empty is the ordinary case
	// rather than a degraded one. It is minted only for a recipient reading
	// mail at an address the IdP said was verified (ENT-249), so a message
	// without one is a message to somebody the schema will not let act from a
	// link.
	ApproveURL string
}

// FindingNotification renders the doorbell.
//
// A doorbell says that something happened, not what it says (§17.1). There is
// no detected text, no proposed action and no obligation summary here on
// purpose: those are a customer's compliance exposure, and putting them in an
// email moves them into a mailbox and into a mail provider's logs. The
// recipient follows the link and reads the finding behind their own session,
// where their role and organisation are checked again.
//
// The approve link does not weaken that rule, and the wording is what keeps it
// honest. It says the person can approve WITHOUT reading, and that the trail
// will record it that way, because that is exactly what `approval_reviewed`
// records and somebody deciding from a mailbox deserves to know it before they
// click rather than afterwards.
func FindingNotification(d Doorbell) Message {
	org := strings.TrimSpace(d.OrgName)
	if org == "" {
		org = "your organisation"
	}

	subject := fmt.Sprintf("A %s compliance finding needs attention in %s", d.Severity, org)

	lines := []string{
		fmt.Sprintf("Kindlast has raised a %s finding for %s.", d.Severity, org),
		"",
		"Open it here to read what was found and decide what to do:",
		d.FindingURL,
	}

	if d.ApproveURL != "" {
		lines = append(lines,
			"",
			"If you already know this one should be approved, you can approve it",
			"from the link below without signing in. It works once, it expires",
			"within the hour, and the audit trail will record that you approved",
			"it from an email without reading the finding first:",
			d.ApproveURL,
		)
	}

	lines = append(lines,
		"",
		"You are receiving this because you are a member of this organisation and",
		"your notification settings include findings at this severity.",
		"",
		"To stop receiving these, use this link:",
		d.UnsubscribeURL,
	)

	// No Kind, deliberately. Kind names a row in `transactional_outbox`, and a
	// doorbell never goes there: it is rendered at dispatch from a
	// `notification_outbox` row and handed straight to the channel. Setting one
	// would imply a persistence path that does not exist and invite somebody to
	// write this message into the wrong table.
	return Message{
		RecipientEmail: d.RecipientEmail,
		Subject:        subject,
		BodyText:       strings.Join(lines, "\n"),
	}
}
