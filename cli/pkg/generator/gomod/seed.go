package gomod

import (
	"bytes"
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// Latest is the version of a requirement nothing knows a version for. Seed
// writes no line for it: tidy resolves it, which is what would have happened
// anyway, and a floor of "latest" is no floor.
const Latest = "latest"

// Requirement is one direct requirement the generator's output implies.
type Requirement struct {
	Path    string
	Version string
	// Floor marks a version GTB declares as its compatibility baseline (D8):
	// a present line below it is raised. Without it a present line is never
	// touched.
	Floor bool
}

// Raise records a present line moved up to a declared floor.
type Raise struct {
	Path, From, To string
}

// Report says what Seed did, so the run can say it too.
type Report struct {
	Added   []string
	Dropped []string
	Raised  []Raise
	// DroppedTools names the tool directives removed because the tool is
	// installed now rather than run through go tool (#86).
	DroppedTools []string
	// Unpinned names the wanted modules that had no version to seed; tidy
	// resolves them.
	Unpinned []string
}

// Replace is the development replace directive GTB_FRAMEWORK_REPLACE asks for.
type Replace struct {
	Path string
	Dir  string
}

// replaceComment is the note the development replace carries, so a reader of
// the file knows it is not for publishing.
const replaceComment = "// Development only: GTB_FRAMEWORK_REPLACE pointed this scaffold at a framework\n" +
	"// working tree. Regenerate without the variable set before publishing."

type options struct {
	owned     []string
	module    string
	goVersion string
	tools     []string
	dropTools []string
}

// Option adjusts Seed.
type Option func(*options)

// Owned names the modules the generator writes require lines for. A present
// line for one of them that is no longer wanted is dropped; a line for any
// other module is never dropped, whoever added it.
func Owned(paths ...string) Option {
	return func(o *options) { o.owned = append(o.owned, paths...) }
}

// WithModule sets the module path and go directive on a file that has none,
// which is how a fresh scaffold's go.mod comes to exist.
func WithModule(path, goVersion string) Option {
	return func(o *options) { o.module, o.goVersion = path, goVersion }
}

// WithTools adds tool directives that are missing.
func WithTools(paths ...string) Option {
	return func(o *options) { o.tools = append(o.tools, paths...) }
}

// WithoutTools drops the tool directives named, when present: the tools an
// older scaffold ran through go tool that are installed now (spec 0197 D12).
func WithoutTools(paths ...string) Option {
	return func(o *options) { o.dropTools = append(o.dropTools, paths...) }
}

// Seed edits src, a go.mod (nil for a new file), so that every wanted
// requirement has a line, every owned line nothing wants any more is gone,
// and the development replace is present or absent as asked. Everything else
// in the file is left as it was.
func Seed(src []byte, want []Requirement, replace *Replace, opts ...Option) ([]byte, Report, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	f, err := modfile.Parse("go.mod", src, nil)
	if err != nil {
		return nil, Report{}, errors.Wrap(err, "parse go.mod")
	}

	if err := applyModule(f, o); err != nil {
		return nil, Report{}, err
	}

	var report Report

	dropTools(f, o.dropTools, &report)

	for _, w := range want {
		seedOne(f, w, &report)
	}

	dropOrphans(f, want, o.owned, &report)

	if err := applyReplace(f, replace); err != nil {
		return nil, Report{}, err
	}

	f.Cleanup()

	out, err := f.Format()
	if err != nil {
		return nil, Report{}, errors.Wrap(err, "format go.mod")
	}

	if replace != nil {
		out = annotateReplace(out)
	}

	return out, report, nil
}

func applyModule(f *modfile.File, o options) error {
	if f.Module == nil && o.module != "" {
		if err := f.AddModuleStmt(o.module); err != nil {
			return errors.Wrap(err, "add module")
		}
	}

	if f.Go == nil && o.goVersion != "" {
		if err := f.AddGoStmt(o.goVersion); err != nil {
			return errors.Wrap(err, "add go directive")
		}
	}

	for _, tool := range o.tools {
		if !slices.ContainsFunc(f.Tool, func(t *modfile.Tool) bool { return t.Path == tool }) {
			if err := f.AddTool(tool); err != nil {
				return errors.Wrap(err, "add tool")
			}
		}
	}

	return nil
}

// seedOne adds a missing line, raises one below a declared floor, and
// otherwise leaves the present line alone at whatever version it holds.
func seedOne(f *modfile.File, w Requirement, report *Report) {
	if w.Version == Latest || w.Version == "" {
		report.Unpinned = append(report.Unpinned, w.Path)

		return
	}

	present := requireFor(f, w.Path)
	if present == nil {
		_ = f.AddRequire(w.Path, w.Version) // AddRequire only errors on a malformed path, checked by the caller's tables

		report.Added = append(report.Added, w.Path)

		return
	}

	if w.Floor && semver.Compare(present.Mod.Version, w.Version) < 0 {
		report.Raised = append(report.Raised, Raise{Path: w.Path, From: present.Mod.Version, To: w.Version})
		_ = f.AddRequire(w.Path, w.Version) // an existing path is updated in place
	}
}

func requireFor(f *modfile.File, path string) *modfile.Require {
	for _, r := range f.Require {
		if r.Mod.Path == path {
			return r
		}
	}

	return nil
}

// dropTools removes the tool directives asked for, reporting the ones that
// were there as the file spelt them. A directive that is absent is nothing to
// report. The match ignores a major-version path segment, so an ask for
// golangci-lint/cmd/golangci-lint drops the v2 line an older scaffold carries
// (#92) and mockery/v3 drops any major.
func dropTools(f *modfile.File, paths []string, report *Report) {
	for _, path := range paths {
		want := withoutMajor(path)

		for _, t := range slices.Clone(f.Tool) {
			if withoutMajor(t.Path) != want {
				continue
			}

			// DropTool clears the entry it removes, so the path is kept first.
			dropped := t.Path
			_ = f.DropTool(dropped) // DropTool only errors on a malformed path, and this one parsed

			report.DroppedTools = append(report.DroppedTools, dropped)
		}
	}
}

// withoutMajor is path with any /vN major-version segment removed.
func withoutMajor(path string) string {
	parts := strings.Split(path, "/")
	kept := parts[:0]

	for _, p := range parts {
		if isMajorSegment(p) {
			continue
		}

		kept = append(kept, p)
	}

	return strings.Join(kept, "/")
}

func isMajorSegment(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}

	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// dropOrphans removes the lines of owned modules nothing wants any more.
func dropOrphans(f *modfile.File, want []Requirement, owned []string, report *Report) {
	wanted := func(path string) bool {
		return slices.ContainsFunc(want, func(w Requirement) bool { return w.Path == path })
	}

	for _, path := range owned {
		if wanted(path) || requireFor(f, path) == nil {
			continue
		}

		_ = f.DropRequire(path)

		report.Dropped = append(report.Dropped, path)
	}
}

// applyReplace adds the development replace when asked and drops the one
// this package wrote when not. A replace the developer wrote for the same
// path without the comment is theirs and stays.
func applyReplace(f *modfile.File, want *Replace) error {
	if want != nil {
		if err := f.AddReplace(want.Path, "", want.Dir, ""); err != nil {
			return errors.Wrap(err, "add replace")
		}

		return nil
	}

	for _, r := range f.Replace {
		if isDevelopmentReplace(r) {
			if err := f.DropReplace(r.Old.Path, r.Old.Version); err != nil {
				return errors.Wrap(err, "drop replace")
			}
		}
	}

	return nil
}

func isDevelopmentReplace(r *modfile.Replace) bool {
	if r.Syntax == nil {
		return false
	}

	for _, c := range r.Syntax.Before {
		if strings.Contains(c.Token, "GTB_FRAMEWORK_REPLACE") {
			return true
		}
	}

	return false
}

// annotateReplace puts the development comment above the replace directive.
// modfile carries comments in its syntax tree but offers no way to add one
// with AddReplace, so the note is placed on the formatted text.
func annotateReplace(out []byte) []byte {
	if bytes.Contains(out, []byte("GTB_FRAMEWORK_REPLACE")) {
		return out
	}

	i := bytes.Index(out, []byte("\nreplace "))
	if i < 0 {
		return out
	}

	return append(append(append([]byte{}, out[:i+1]...), []byte(replaceComment+"\n")...), out[i+1:]...)
}
