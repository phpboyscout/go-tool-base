package ignore

import (
	"context"
	"fmt"
	"strings"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// init registers a gtb-only doctor check for diverged-and-unignored generated
// files. It is registered under the (default-enabled) doctor feature so it runs
// with `gtb doctor`, but only in the gtb binary — scaffolded tools do not import
// this package, so they never inherit a manifest-aware check. The framework
// doctor cannot host this itself: it is manifest-agnostic and cannot import
// internal/generator. See spec 2026-07-28-ignore-command-and-discoverability §5.
func init() {
	setup.RegisterChecks(props.DoctorCmd, []setup.CheckProvider{
		func(_ *props.Props) []setup.CheckFunc {
			return []setup.CheckFunc{checkDivergedUnignored}
		},
	})
}

// checkDivergedUnignored reports the tracked files that have diverged from their
// manifest hash and are NOT covered by an ignore rule — precisely the set that
// will raise a conflict prompt on the next regenerate. It skips cleanly when the
// working directory is not a generated project (no .gtb/manifest.yaml).
func checkDivergedUnignored(_ context.Context, p *props.Props) setup.CheckResult {
	const name = "Generator ignore coverage"

	gen := generator.New(p, &generator.Config{Path: "."})

	diverged, err := gen.DivergedUnignoredFiles()
	if err != nil {
		// No manifest here means this is not a generated project — nothing to check.
		return setup.CheckResult{Name: name, Status: "skip", Message: "no .gtb/manifest.yaml in the current directory"}
	}

	if len(diverged) == 0 {
		return setup.CheckResult{Name: name, Status: "pass", Message: "no diverged, unignored generated files"}
	}

	return setup.CheckResult{
		Name:    name,
		Status:  "warn",
		Message: fmt.Sprintf("%d generated file(s) diverged and not ignored; regenerate will prompt on each", len(diverged)),
		Details: "mark them hands-off with 'gtb ignore add <path>': " + strings.Join(diverged, ", "),
	}
}
