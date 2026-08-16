package findings_test

import (
	"testing"

	"github.com/Entear-OU/kindlast/apps/core-api/internal/domain/findings"
)

// The rule is the acceptance criterion, so it is tested exhaustively rather
// than sampled. The legacy suite at db0bf83 did the same and it is why the
// rewrite could be checked against it line by line.

func TestNotAssessedOutranksEverything(t *testing.T) {
	// The ENT-161 case, and the one worth stating first. An organisation whose
	// Watcher has never run has no findings, so every count is zero, and a rule
	// that only counted would call that green. It is not green: nothing looked.
	got := findings.ComputePosture(findings.PostureInputs{Assessed: false})
	if got != findings.PostureNotAssessed {
		t.Fatalf("an unassessed organisation is %q, want not_assessed", got)
	}
}

func TestNotAssessedEvenWithFindings(t *testing.T) {
	// Defensive, and the state is reachable: findings can exist from an import
	// or a manual sweep while watcher_last_run_at is still null. "We have not
	// run" is still the honest answer, and it must not be overridden by data
	// that arrived some other way.
	got := findings.ComputePosture(findings.PostureInputs{
		OpenSeverities: []string{"critical"},
		Assessed:       false,
	})
	if got != findings.PostureNotAssessed {
		t.Fatalf("got %q, want not_assessed", got)
	}
}

func TestPostureBands(t *testing.T) {
	cases := []struct {
		name      string
		in        findings.PostureInputs
		want      findings.Posture
		reasoning string
	}{
		{
			name: "nothing open is green",
			in:   findings.PostureInputs{Assessed: true},
			want: findings.PostureGreen,
		},
		{
			name: "only medium and low open is green",
			in: findings.PostureInputs{
				OpenSeverities: []string{"medium", "low", "medium"},
				Assessed:       true,
			},
			want: findings.PostureGreen,
		},
		{
			name: "an open high is amber",
			in: findings.PostureInputs{
				OpenSeverities: []string{"high", "low"},
				Assessed:       true,
			},
			want: findings.PostureAmber,
		},
		{
			name: "an open critical is red",
			in: findings.PostureInputs{
				OpenSeverities: []string{"critical", "medium"},
				Assessed:       true,
			},
			want: findings.PostureRed,
		},
		{
			name: "an overdue critical deadline is red with no findings at all",
			in: findings.PostureInputs{
				Deadlines: []findings.Deadline{{Severity: "critical", DaysRemaining: -1}},
				Assessed:  true,
			},
			want: findings.PostureRed,
		},
		{
			// The distinction the original AC drew and the one this rewrite had
			// to preserve: pressing is not the same as lapsed, and a founder
			// acts on them differently.
			name: "a near-term critical deadline is amber, not red",
			in: findings.PostureInputs{
				Deadlines: []findings.Deadline{{Severity: "critical", DaysRemaining: 3}},
				Assessed:  true,
			},
			want: findings.PostureAmber,
		},
		{
			name: "a near-term high deadline is amber",
			in: findings.PostureInputs{
				Deadlines: []findings.Deadline{{Severity: "high", DaysRemaining: 30}},
				Assessed:  true,
			},
			want: findings.PostureAmber,
		},
		{
			name: "a critical deadline beyond the window does not break green",
			in: findings.PostureInputs{
				Deadlines: []findings.Deadline{{Severity: "critical", DaysRemaining: 31}},
				Assessed:  true,
			},
			want: findings.PostureGreen,
		},
		{
			name: "an overdue medium deadline does not colour the band",
			in: findings.PostureInputs{
				Deadlines: []findings.Deadline{{Severity: "medium", DaysRemaining: -40}},
				Assessed:  true,
			},
			want: findings.PostureGreen,
		},
		{
			name: "red wins over amber",
			in: findings.PostureInputs{
				OpenSeverities: []string{"high", "critical"},
				Deadlines:      []findings.Deadline{{Severity: "high", DaysRemaining: 1}},
				Assessed:       true,
			},
			want: findings.PostureRed,
		},
		{
			// The exact boundary, both sides. Off-by-one here would silently
			// move a whole day's worth of deadlines between bands.
			name: "the window is inclusive at zero",
			in: findings.PostureInputs{
				Deadlines: []findings.Deadline{{Severity: "high", DaysRemaining: 0}},
				Assessed:  true,
			},
			want: findings.PostureAmber,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := findings.ComputePosture(c.in); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestEveryBandHasAHeadline(t *testing.T) {
	// A band that renders with no sentence next to it is a blank space where
	// the product should be explaining itself.
	for _, p := range []findings.Posture{
		findings.PostureNotAssessed,
		findings.PostureGreen,
		findings.PostureAmber,
		findings.PostureRed,
	} {
		if findings.Headline(p) == "" {
			t.Errorf("%q has no headline", p)
		}
	}
}

func TestUnknownPostureHasNoReassuringHeadline(t *testing.T) {
	// An unrecognised band must not fall back to the green sentence. Better a
	// blank than a false all-clear.
	if got := findings.Headline(findings.Posture("chartreuse")); got != "" {
		t.Fatalf("got %q, want an empty headline", got)
	}
}

func TestCountSeverities(t *testing.T) {
	got := findings.CountSeverities([]string{
		"critical", "high", "high", "medium", "low", "low", "low",
	})

	want := findings.SeverityCounts{Critical: 1, High: 2, Medium: 1, Low: 3}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got.Total() != 7 {
		t.Fatalf("total is %d, want 7", got.Total())
	}
}

func TestCountSeveritiesIgnoresAnUnknownValue(t *testing.T) {
	// Not folded into low, and not counted in the total. The column is
	// check-constrained, so an unknown value means the constraint moved without
	// this function, and quietly bucketing it would under-report exposure in
	// the one direction that matters.
	got := findings.CountSeverities([]string{"critical", "catastrophic"})

	if got.Total() != 1 {
		t.Fatalf("total is %d, want 1", got.Total())
	}
	if got.Low != 0 {
		t.Fatalf("an unknown severity was counted as low")
	}
}
