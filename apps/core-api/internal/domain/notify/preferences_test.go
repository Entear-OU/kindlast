package notify

import (
	"strings"
	"testing"
	"time"
)

// The rules that decide who hears about a finding (ENT-209).
//
// Every one of these fails silently in production. A severity comparison that
// is off by one, or a quiet window that never matches, produces no error
// anywhere: it produces an email that did not arrive, which nobody reports for
// days and which reads like a spam filter problem when they finally do.

func TestDefaultsAreSubscribed(t *testing.T) {
	// The tempting default is the opposite one, and it is wrong for this
	// product: a compliance workspace whose members hear nothing until each
	// individually opts in is one where a critical finding sits unread.
	d := Defaults()

	if !d.WeeklyBriefingEnabled || !d.DeadlineAlertsEnabled {
		t.Fatal("a member who has never opened the settings page is unsubscribed by default")
	}
	if d.MinSeverityForEmail != SeverityMedium {
		t.Fatalf("default floor is %q, want %q", d.MinSeverityForEmail, SeverityMedium)
	}
	if d.Timezone != DefaultTimezone {
		t.Fatalf("default timezone is %q, want the schema's %q", d.Timezone, DefaultTimezone)
	}
}

func TestMeetsSeverity(t *testing.T) {
	for _, tc := range []struct {
		finding, floor string
		want           bool
	}{
		{SeverityCritical, SeverityMedium, true},
		{SeverityHigh, SeverityMedium, true},
		{SeverityMedium, SeverityMedium, true}, // the floor is inclusive
		{SeverityLow, SeverityMedium, false},
		{SeverityLow, SeverityLow, true},
		{SeverityCritical, SeverityCritical, true},
		{SeverityHigh, SeverityCritical, false},
	} {
		if got := MeetsSeverity(tc.finding, tc.floor); got != tc.want {
			t.Errorf("MeetsSeverity(%q, %q) = %v, want %v", tc.finding, tc.floor, got, tc.want)
		}
	}
}

func TestAnUnknownSeverityIsDeliveredRatherThanSwallowed(t *testing.T) {
	// The safe direction. The most likely reason for an unrecognised severity
	// is somebody adding one above `critical`, and the failure mode of the
	// other choice is that the most serious findings in the system are the ones
	// silently suppressed.
	if !MeetsSeverity("catastrophic", SeverityMedium) {
		t.Fatal("an unrecognised finding severity was suppressed")
	}
	if !MeetsSeverity(SeverityLow, "whatever") {
		t.Fatal("an unrecognised floor suppressed a notification")
	}
}

// Quiet hours that wrap midnight are the normal case, not the exception.
// 22:00 to 07:00 is what people actually set, and a naive
// `start <= now && now < end` comparison silently means "never" for those.
func TestQuietHoursWrappingMidnight(t *testing.T) {
	at := func(h, m int) time.Time {
		return time.Date(2026, 8, 17, h, m, 0, 0, time.UTC)
	}

	for _, tc := range []struct {
		name  string
		now   time.Time
		start string
		end   string
		want  bool
	}{
		{"late evening, inside", at(23, 30), "22:00", "07:00", true},
		{"small hours, inside", at(3, 0), "22:00", "07:00", true},
		{"exactly the start", at(22, 0), "22:00", "07:00", true},
		{"exactly the end is outside", at(7, 0), "22:00", "07:00", false},
		{"mid-morning, outside", at(9, 0), "22:00", "07:00", false},

		{"daytime window, inside", at(13, 0), "12:00", "14:00", true},
		{"daytime window, outside", at(15, 0), "12:00", "14:00", false},

		{"no window set", at(3, 0), "", "", false},
		{"only a start is not a window", at(3, 0), "22:00", "", false},
		// A zero-length window reads as "no quiet hours" rather than "always
		// quiet", because the latter silences somebody permanently through what
		// is almost certainly a typo.
		{"zero-length window", at(3, 0), "22:00", "22:00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := InQuietHours(tc.now, tc.start, tc.end); got != tc.want {
				t.Fatalf("InQuietHours(%s, %q, %q) = %v, want %v",
					tc.now.Format("15:04"), tc.start, tc.end, got, tc.want)
			}
		})
	}
}

func TestShouldNotifySaysWhyNot(t *testing.T) {
	// The reason ends up in the outbox row's `last_error` when nobody wanted a
	// notification, so an operator asking "why did nothing go out" gets an
	// answer rather than silence.
	night := time.Date(2026, 8, 17, 23, 0, 0, 0, time.UTC)

	ok, reason := ShouldNotify(SeverityLow, SeverityHigh, night, "", "")
	if ok {
		t.Fatal("a finding below the floor was notified")
	}
	if !strings.Contains(reason, "below") {
		t.Fatalf("reason %q does not explain the severity floor", reason)
	}

	ok, reason = ShouldNotify(SeverityCritical, SeverityLow, night, "22:00", "07:00")
	if ok {
		t.Fatal("a notification was sent inside quiet hours")
	}
	if !strings.Contains(reason, "quiet") {
		t.Fatalf("reason %q does not mention quiet hours", reason)
	}

	ok, reason = ShouldNotify(SeverityCritical, SeverityLow, night, "", "")
	if !ok {
		t.Fatalf("a critical finding with no quiet hours was refused: %s", reason)
	}
	if reason != "" {
		t.Fatalf("a delivered notification carried a reason: %q", reason)
	}
}

func TestNormaliseFillsGapsAndRefusesNonsense(t *testing.T) {
	got, err := Preferences{}.Normalise()
	if err != nil {
		t.Fatalf("an empty request was refused: %v", err)
	}
	if got.MinSeverityForEmail != SeverityMedium || got.Timezone != DefaultTimezone {
		t.Fatalf("gaps were not filled: %+v", got)
	}

	for _, tc := range []struct {
		name string
		in   Preferences
	}{
		{"unknown severity", Preferences{MinSeverityForEmail: "urgent"}},
		{"unknown timezone", Preferences{Timezone: "Mars/Olympus_Mons"}},
		{"a start with no end", Preferences{QuietHoursStart: "22:00"}},
		{"an end with no start", Preferences{QuietHoursEnd: "07:00"}},
		{"a time that is not one", Preferences{QuietHoursStart: "quarter past", QuietHoursEnd: "07:00"}},
		{"an address that is not one", Preferences{Email: "not-an-address"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.in.Normalise(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestNormaliseCatchesABadTimezoneNowRatherThanAtSendTime(t *testing.T) {
	// The point of validating the zone here. An unloadable zone at send time is
	// a notification that fails hours later, for a reason nobody connects to a
	// settings page they were on that morning.
	if _, err := (Preferences{Timezone: "Europe/Tallinn"}).Normalise(); err != nil {
		t.Fatalf("a real zone was rejected: %v", err)
	}
	if _, err := (Preferences{Timezone: "Not/AZone"}).Normalise(); err == nil {
		t.Fatal("an unloadable zone was accepted and would fail at send time")
	}
}

// The doorbell rule (§17.1): say that something happened, not what it says.
func TestTheNotificationDoesNotCarryTheFinding(t *testing.T) {
	msg := FindingNotification(
		"member@example.invalid", "acme-gmbh", "high",
		"http://localhost:3000/o/acme-gmbh/feed/abc",
		"http://localhost:3000/unsubscribe/tok")

	if !strings.Contains(msg.BodyText, "/o/acme-gmbh/feed/abc") {
		t.Fatal("the notification has no link, so it cannot be acted on")
	}
	if !strings.Contains(msg.BodyText, "/unsubscribe/tok") {
		t.Fatal("the notification offers no way to stop receiving them")
	}
	if !strings.Contains(msg.Subject, "high") {
		t.Fatalf("the subject does not say how serious it is: %q", msg.Subject)
	}

	// Kind names a row in transactional_outbox and a doorbell never goes there.
	// Setting one would imply a persistence path that does not exist.
	if msg.Kind != "" {
		t.Fatalf("a doorbell carries kind %q, implying it is persisted", msg.Kind)
	}
}
