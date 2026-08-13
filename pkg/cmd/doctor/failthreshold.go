package doctor

import (
	"log/slog"

	"gitlab.com/phpboyscout/go/errorhandling"
	"gitlab.com/phpboyscout/go/errors"
)

// FailThreshold is the worst check status a run tolerates before it reports a
// non-zero exit.
type FailThreshold string

const (
	// FailOnNone never fails the run. The escape hatch for a pipeline that is
	// not ready to be gated yet.
	FailOnNone FailThreshold = "none"
	// FailOnFail fails only on a check that outright failed.
	FailOnFail FailThreshold = "fail"
	// FailOnWarn fails on a warning too — which is where deprecated credential
	// storage lands, so this is what makes a compatibility window close.
	FailOnWarn FailThreshold = "warn"
)

// ErrDoctorFoundProblems reports a health run that crossed its threshold.
//
// The disposition travels with the error rather than being an os.Exit in the
// command: nothing in the error path exits the process, and pkg/cmd/root's
// Execute owns termination. Reported at WARN because the report itself has
// already been printed in full — a second, louder rendering of "something is
// wrong" adds nothing a reader does not already have on screen.
var ErrDoctorFoundProblems = errorhandling.WithOutcome(
	errors.NewSentinel("gtb.doctor.problems", "health checks reported problems"),
	errorhandling.Outcome{Code: 1, Level: slog.LevelWarn},
)

// DefaultFailThreshold is the threshold used when the operator has not chosen
// one.
//
// Under CI a warning fails the run: deprecated credential storage reports as a
// warning, and R3 exists so a pipeline can stop that becoming permanent instead
// of printing something nobody reads. Interactively it takes FailOnFail, so a
// developer's terminal is not broken by a warning on upgrade — but a check that
// genuinely FAILED still produces a non-zero exit, which it never did before
// (spec 0189 G2: doctor returned success unconditionally, so no script could
// tell a clean run from a broken one).
//
// The CI fact is a parameter rather than something read here, because a rule
// that reads the world is a rule whose tests pass on one machine and fail on
// another. That is not hypothetical: it is how !400 went red.
func DefaultFailThreshold(ci bool) FailThreshold {
	if ci {
		return FailOnWarn
	}

	return FailOnFail
}

// ParseFailThreshold validates an operator-supplied threshold.
func ParseFailThreshold(v string) (FailThreshold, error) {
	switch t := FailThreshold(v); t {
	case FailOnNone, FailOnFail, FailOnWarn:
		return t, nil
	}

	return "", errors.Newf("invalid --fail-on %q; expected one of: none, fail, warn", v)
}

// Exceeded reports whether a report crosses the threshold.
//
// CheckSkip never counts. A check that could not run has not found a problem,
// and failing a pipeline because something was unavailable would make the gate
// untrustworthy — which is the fastest way to have it turned off.
func (t FailThreshold) Exceeded(report *DoctorReport) bool {
	if report == nil || t == FailOnNone {
		return false
	}

	for _, c := range report.Checks {
		switch c.Status {
		case CheckFail:
			return true
		case CheckWarn:
			if t == FailOnWarn {
				return true
			}
		case CheckPass, CheckSkip:
		}
	}

	return false
}
