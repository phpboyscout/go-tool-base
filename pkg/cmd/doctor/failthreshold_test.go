package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Spec 0189 phase 3 (R3, D6). doctor returned success unconditionally — even
// with a [FAIL] on screen — so no pipeline could gate on it and a compatibility
// window became permanent by default.

// reportWith builds a report whose warnings are GATING, so the table below
// exercises the threshold logic itself. Whether an advisory warning gates is a
// separate question, pinned by its own test.
func reportWith(statuses ...CheckStatus) *DoctorReport {
	r := &DoctorReport{Tool: "t", Version: "v"}
	for i, s := range statuses {
		r.Checks = append(r.Checks, CheckResult{
			Name:   string(rune('a' + i)),
			Status: s,
			Gating: s == CheckWarn,
		})
	}

	return r
}

func TestFailThreshold_Exceeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		threshold FailThreshold
		report    *DoctorReport
		want      bool
	}{
		{"none tolerates a failure", FailOnNone, reportWith(CheckFail), false},
		{"fail catches a failure", FailOnFail, reportWith(CheckPass, CheckFail), true},
		{"fail tolerates a warning", FailOnFail, reportWith(CheckWarn), false},
		{"warn catches a warning", FailOnWarn, reportWith(CheckPass, CheckWarn), true},
		{"warn catches a failure too", FailOnWarn, reportWith(CheckFail), true},
		{"a clean report passes", FailOnWarn, reportWith(CheckPass, CheckPass), false},

		// A check that could not run has not found a problem. Failing a
		// pipeline because something was unavailable makes the gate
		// untrustworthy, which is the fastest way to have it switched off.
		{"skip never counts, even at warn", FailOnWarn, reportWith(CheckSkip, CheckPass), false},

		{"a nil report is not a failure", FailOnWarn, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.threshold.Exceeded(tt.report))
		})
	}
}

func TestDefaultFailThreshold(t *testing.T) {
	t.Parallel()

	// Under CI a warning gates the run: deprecated credential storage reports
	// as a warning, and that is the whole point of R3.
	assert.Equal(t, FailOnWarn, DefaultFailThreshold(true))

	// Interactively a warning does not break a developer's terminal on
	// upgrade — but a genuine failure now exits non-zero, which it never did.
	assert.Equal(t, FailOnFail, DefaultFailThreshold(false))
}

func TestParseFailThreshold(t *testing.T) {
	t.Parallel()

	for _, v := range []string{"none", "fail", "warn"} {
		got, err := ParseFailThreshold(v)
		require.NoError(t, err)
		assert.Equal(t, FailThreshold(v), got)
	}

	_, err := ParseFailThreshold("sometimes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "none, fail, warn")
}

func TestFailThreshold_AdvisoryWarningsNeverGate(t *testing.T) {
	t.Parallel()

	advisory := &DoctorReport{Checks: []CheckResult{
		// The real one: a tool that does not use AI is in a perfectly good
		// state, and failing its pipeline over this would turn a diagnostic
		// into a tripwire.
		{Name: "API keys", Status: CheckWarn, Message: "no AI provider API keys configured"},
	}}

	assert.False(t, FailOnWarn.Exceeded(advisory),
		"an advisory warning must not fail a build at any threshold")

	policy := &DoctorReport{Checks: []CheckResult{
		{Name: "Credential storage", Status: CheckWarn, Gating: true},
	}}

	assert.True(t, FailOnWarn.Exceeded(policy),
		"a warning a check marked as a policy violation is what the gate is for")
	assert.False(t, FailOnFail.Exceeded(policy),
		"and it still only gates at the warn threshold")
}

func TestFailThreshold_GatingIsIrrelevantToAFailure(t *testing.T) {
	t.Parallel()

	// There is nothing advisory about a failed check, so it gates either way.
	for _, gating := range []bool{true, false} {
		r := &DoctorReport{Checks: []CheckResult{{Status: CheckFail, Gating: gating}}}
		assert.True(t, FailOnFail.Exceeded(r))
		assert.True(t, FailOnWarn.Exceeded(r))
	}
}
