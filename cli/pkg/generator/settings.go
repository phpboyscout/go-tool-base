package generator

import (
	"context"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/afero"
	"gitlab.com/phpboyscout/go/errors"
)

// ErrUnknownSetting is a path the author-settings table does not name.
var ErrUnknownSetting = errors.NewSentinel("gtb.generator.unknown_setting", "not an author setting")

// ErrSettingReadOnly is a manifest field the generator records, or one that
// has its own command, which set does not write.
var ErrSettingReadOnly = errors.NewSentinel("gtb.generator.setting_read_only", "not set this way")

// propertiesPrefix is implied when a path names nothing under release_source
// or version, so `chat.default.provider` reads as the author writes it.
const propertiesPrefix = "properties."

// readOnlyReasons names the manifest paths set refuses and where each is
// changed instead. Recorded fields are the generator's; features and
// templates have commands of their own.
var readOnlyReasons = map[string]string{
	"hashes":                      "recorded by the generator",
	"commands":                    "recorded by gtb generate command",
	"properties.features":         "use gtb enable <feature> and gtb disable <feature>",
	"properties.templates":        "use gtb template add, update and remove",
	"properties.docs_layout":      "recorded by the generator",
	"properties.module_published": "recorded by the generator",
	"version.gtb":                 "recorded by the gtb that generates",
	// The direct release channel is withdrawn until its design lands (#90);
	// its keys stay in the table so an older manifest loads, but nothing
	// offers them, set included.
	"release_source.direct": "the direct release channel is withdrawn until its design lands (go-tool-base #90)",
}

// SetSetting writes one author setting into the manifest by its dotted path,
// validates the result as the flags are validated, and runs the derived-file
// sync, so a set never needs a regenerate after it (spec 0197 D6). Nothing is
// written when validation refuses.
func (g *Generator) SetSetting(ctx context.Context, path string, values []string) error {
	return g.editSetting(ctx, path, func(leaf reflect.Value) error { return assignLeaf(leaf, values) })
}

// UnsetSetting zeroes one author setting.
func (g *Generator) UnsetSetting(ctx context.Context, path string) error {
	return g.editSetting(ctx, path, func(leaf reflect.Value) error {
		leaf.Set(reflect.Zero(leaf.Type()))

		return nil
	})
}

// GetSetting reads one author setting back as the strings set would take.
func (g *Generator) GetSetting(path string) ([]string, error) {
	full, err := resolveSettingPath(path)
	if err != nil {
		return nil, err
	}

	m, err := g.loadManifest()
	if err != nil {
		return nil, err
	}

	leaf, err := manifestLeaf(m, full)
	if err != nil {
		return nil, err
	}

	return leafStrings(leaf), nil
}

func (g *Generator) editSetting(ctx context.Context, path string, edit func(reflect.Value) error) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	full, err := resolveSettingPath(path)
	if err != nil {
		return err
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	if err := editManifestSetting(m, full, edit); err != nil {
		return err
	}

	return g.commitSetting(ctx, m)
}

// editManifestSetting applies edit to the field full names and validates the
// result; the manifest is only in memory here, so a refusal writes nothing.
func editManifestSetting(m *Manifest, full string, edit func(reflect.Value) error) error {
	leaf, err := manifestLeaf(m, full)
	if err != nil {
		return err
	}

	if err := edit(leaf); err != nil {
		return err
	}

	if full == "release_source.repo" {
		splitRepoInto(m)
	}

	return ValidateManifest(m)
}

// commitSetting syncs the derived files, writes the manifest and formats the
// touched tree on a real filesystem (skipped on the in-memory fs of tests).
func (g *Generator) commitSetting(ctx context.Context, m *Manifest) error {
	if err := g.syncDerivedFromManifest(m); err != nil {
		return err
	}

	if err := g.writeManifest(m); err != nil {
		return err
	}

	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.runSkeletonPostProcessing(ctx, g.config.Path)
	}

	return nil
}

// resolveSettingPath turns what the author typed into the table's manifest
// path, or says why it cannot be set.
func resolveSettingPath(path string) (string, error) {
	candidates := []string{path}
	if !strings.HasPrefix(path, propertiesPrefix) && !strings.HasPrefix(path, "release_source.") && !strings.HasPrefix(path, "version.") {
		candidates = append(candidates, propertiesPrefix+path)
	}

	for _, candidate := range candidates {
		if reason := settingReadOnlyReason(candidate); reason != "" {
			return "", errors.WithHint(errors.Wrapf(ErrSettingReadOnly, "%s", candidate), reason+".")
		}

		if _, ok := AuthorSettingByPath(candidate); ok {
			return candidate, nil
		}
	}

	return "", errors.WithHint(errors.Wrapf(ErrUnknownSetting, "%q", path),
		"Settable paths:\n  "+strings.Join(settablePaths(), "\n  "))
}

// AuthorSettingByPath finds the table row for a manifest path.
func AuthorSettingByPath(path string) (AuthorSetting, bool) {
	for _, s := range authorSettings {
		if s.Manifest == path {
			return s, true
		}
	}

	return AuthorSetting{}, false
}

func settingReadOnlyReason(path string) string {
	for prefix, reason := range readOnlyReasons {
		if path == prefix || strings.HasPrefix(path, prefix+".") {
			return reason
		}
	}

	return ""
}

func settablePaths() []string {
	var out []string

	for _, s := range authorSettings {
		if s.Kind == KindSetting && settingReadOnlyReason(s.Manifest) == "" {
			out = append(out, strings.TrimPrefix(s.Manifest, propertiesPrefix))
		}
	}

	sort.Strings(out)

	return out
}

// manifestLeaf walks the manifest by yaml tag names to the addressable field
// a dotted path names.
func manifestLeaf(m *Manifest, path string) (reflect.Value, error) {
	v := reflect.ValueOf(m).Elem()

	for _, part := range strings.Split(path, ".") {
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, errors.Wrapf(ErrUnknownSetting, "%q: %q is not a block", path, part)
		}

		field, ok := fieldByYAMLTag(v, part)
		if !ok {
			return reflect.Value{}, errors.Wrapf(ErrUnknownSetting, "%q: no field %q", path, part)
		}

		v = field
	}

	return v, nil
}

func fieldByYAMLTag(v reflect.Value, name string) (reflect.Value, bool) {
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		tag := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
		if tag == name {
			return v.Field(i), true
		}
	}

	return reflect.Value{}, false
}

// assignLeaf coerces the strings set was given into the leaf's type: a
// string or string-like, a bool, or a slice of either.
func assignLeaf(leaf reflect.Value, values []string) error {
	switch leaf.Kind() {
	case reflect.String:
		leaf.SetString(strings.Join(values, " "))
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.Join(values, ""))
		if err != nil {
			return errors.Wrapf(ErrInvalidInput, "%q: want true or false", strings.Join(values, " "))
		}

		leaf.SetBool(b)
	case reflect.Slice:
		if leaf.Type().Elem().Kind() != reflect.String {
			return errors.Wrapf(ErrSettingReadOnly, "a %s is not set this way", leaf.Type())
		}

		out := reflect.MakeSlice(leaf.Type(), 0, len(values))
		for _, v := range splitList(values) {
			out = reflect.Append(out, reflect.ValueOf(v).Convert(leaf.Type().Elem()))
		}

		leaf.Set(out)
	default:
		return errors.Wrapf(ErrSettingReadOnly, "a %s is not set this way", leaf.Type())
	}

	return nil
}

// splitList accepts values as separate arguments or comma separated.
func splitList(values []string) []string {
	var out []string

	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}

	return out
}

func leafStrings(leaf reflect.Value) []string {
	switch leaf.Kind() {
	case reflect.String:
		if leaf.String() == "" {
			return nil
		}

		return []string{leaf.String()}
	case reflect.Bool:
		return []string{strconv.FormatBool(leaf.Bool())}
	case reflect.Slice:
		out := make([]string, 0, leaf.Len())
		for i := 0; i < leaf.Len(); i++ {
			out = append(out, leaf.Index(i).Convert(reflect.TypeOf("")).String())
		}

		return out
	default:
		return nil
	}
}

// splitRepoInto reads release_source.repo as the org/name the flag takes and
// spreads it over owner and repo, the way the generate path does.
func splitRepoInto(m *Manifest) {
	if owner, name, ok := strings.Cut(m.ReleaseSource.Repo, "/"); ok {
		m.ReleaseSource.Owner, m.ReleaseSource.Repo = owner, name
	}
}
