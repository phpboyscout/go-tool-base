package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// finishWizard accepts every remaining page as offered.
func finishWizard(t *testing.T, f *huh.Form, m huh.Model) {
	t.Helper()

	for i := 0; i < 60 && f.State != huh.StateCompleted; i++ {
		var ok bool
		if m, ok = advance(f, m); !ok {
			break
		}
	}

	require.Equal(t, huh.StateCompleted, f.State)
}

// Spec 0204 D9: the Configuration page asks nothing a tool must answer, so
// accepting it declares no format, no own format and no source.
func TestWizard_ConfigurationPageDefaultsDeclareNothing(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f := driveToCompletion(t, o)
	require.Equal(t, huh.StateCompleted, f.State)
	require.NoError(t, o.afterWizard())

	assert.Empty(t, o.ConfigFormats)
	assert.Empty(t, o.ConfigFormat)
	assert.Empty(t, o.ConfigSources)
	assert.Empty(t, o.ConfigLayers)
}

func TestWizard_ConfigurationPageFormats(t *testing.T) {
	t.Parallel()

	t.Run("the formats are chosen", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Repo: "org/my-app"}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "config-formats")
		require.Equal(t, "config-formats", f.GetFocusedField().GetKey())

		m = typeAnswer(m, "x") // toml, the first row
		m, ok := advance(f, m)
		require.True(t, ok)

		finishWizard(t, f, m)
		require.NoError(t, o.afterWizard())

		assert.Equal(t, []string{"toml"}, o.ConfigFormats)
		assert.Empty(t, o.ConfigFormat, "YAML stays the own format unless chosen")
	})

	// The own format's options follow the formats through a command the
	// test drive does not run, so they are given up front, as on the AI page.
	t.Run("the own format is one of them", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Repo: "org/my-app", ConfigFormats: []string{"toml", "ini"}}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "config-format")
		require.Equal(t, "config-format", f.GetFocusedField().GetKey())

		m = typeAnswer(m, "↓") // yaml, toml; ini is read-only and not offered
		m, ok := advance(f, m)
		require.True(t, ok)

		finishWizard(t, f, m)
		require.NoError(t, o.afterWizard())

		assert.Equal(t, "toml", o.ConfigFormat)
	})
}

// A slot is a kind and a name; the order is placed against the built-in
// layers, and required and writable are per slot (D6, D7).
func TestWizard_ConfigurationPageSources(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-source-0-kind")
	require.Equal(t, "config-source-0-kind", f.GetFocusedField().GetKey())

	m = typeAnswer(m, downTo(t, o, "consul"))
	m, ok := advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-0-name", f.GetFocusedField().GetKey())
	m = typeAnswer(m, "team")
	m, ok = advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-0-required", f.GetFocusedField().GetKey())
	m = typeAnswer(m, "l")
	m, ok = advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-0-access", f.GetFocusedField().GetKey())
	m, ok = advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-0-placement", f.GetFocusedField().GetKey())
	m = typeAnswer(m, "↓") // above the user's files
	m, ok = advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-1-kind", f.GetFocusedField().GetKey(), "another slot may be added")

	finishWizard(t, f, m)
	require.NoError(t, o.afterWizard())

	assert.Equal(t, []string{"team=consul"}, o.ConfigSources)
	assert.Equal(t, []string{"team"}, o.ConfigSourcesOptional)
	assert.Empty(t, o.ConfigSourcesWritable)
	assert.Equal(t, []string{"defaults", "files", "team", "project", "env", "flags"}, o.ConfigLayers)
	require.NoError(t, o.validateFields())
}

// A slot left at the default placement records no layer list: R9 places it.
func TestWizard_ADefaultPlacedSourceRecordsNoLayers(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-source-0-kind")
	m = typeAnswer(m, downTo(t, o, "vault"))

	finishWizard(t, f, m)
	require.NoError(t, o.afterWizard())

	assert.Equal(t, []string{"vault=vault"}, o.ConfigSources, "the name defaults to the kind")
	assert.Empty(t, o.ConfigLayers)
}

// A second slot of a kind defaults to a numbered name rather than colliding,
// so accepting the defaults never locks the page.
func TestWizard_ASecondSlotOfAKindDefaultsToANumberedName(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-source-0-kind")
	m = typeAnswer(m, downTo(t, o, "file"))
	m = driveUntilKey(f, m, o, "config-source-1-kind")
	m = typeAnswer(m, downTo(t, o, "file"))

	finishWizard(t, f, m)
	require.NoError(t, o.afterWizard())

	assert.Equal(t, []string{"file=file", "file2=file"}, o.ConfigSources)
}

// A name typed to collide is refused on the name, and typing another one
// there lets the page move on.
func TestWizard_ATypedDuplicateNameIsRefusedAndCorrectable(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-source-0-kind")
	m = typeAnswer(m, downTo(t, o, "vault"))
	m = driveUntilKey(f, m, o, "config-source-1-kind")
	m = typeAnswer(m, downTo(t, o, "consul"))
	m, ok := advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-1-name", f.GetFocusedField().GetKey())
	m = typeAnswer(m, "vault")
	m, ok = advance(f, m)
	require.False(t, ok, "a name another slot has is refused")
	require.Equal(t, "config-source-1-name", f.GetFocusedField().GetKey(), "and the cursor stays on the name")
	assert.Contains(t, f.GetFocusedField().Error().Error(), "vault")

	for range len("vault") {
		m, _ = m.Update(codeKeypress(tea.KeyBackspace))
	}

	m = typeAnswer(m, "shared")
	m, ok = advance(f, m)
	require.True(t, ok, "a corrected name moves on")

	finishWizard(t, f, m)
	require.NoError(t, o.afterWizard())

	assert.Equal(t, []string{"vault=vault", "shared=consul"}, o.ConfigSources)
}

// The keychain is the one kind writable by default (D12): the access row
// offers that default first.
func TestWizard_AKeychainSlotIsWritableByDefault(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app", Features: []string{"init", "doctor", "keychain"}}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-source-0-kind")
	m = typeAnswer(m, downTo(t, o, "keychain"))

	finishWizard(t, f, m)
	require.NoError(t, o.afterWizard())

	assert.Equal(t, []string{"keychain=keychain"}, o.ConfigSources)
	assert.Empty(t, o.ConfigSourcesWritable, "the kind's default is recorded as nothing")
	assert.Empty(t, o.readOnlySources)
}

// D13 of spec 0197 for this page: a revisit pre-fills every slot and its
// placement, and accepting them changes nothing.
func TestWizard_ConfigurationPageRevisitChangesNothing(t *testing.T) {
	t.Parallel()

	start := &SkeletonOptions{
		Name: "tool", Repo: "org/tool", ForgeBackend: "github", Features: generator.DefaultSelectedFeatures,
		ConfigFormats: []string{"toml"}, ConfigFormat: "toml",
		ConfigLayers:  []string{"defaults", "files", "team", "project", "env", "tokens", "flags"},
		ConfigSources: []string{"team=consul", "tokens=keychain"}, ConfigSourcesOptional: []string{"team"},
	}
	start.readOnlySources = []string{"tokens"}
	require.NoError(t, start.validateFields())

	m := generator.ManifestFromSkeletonConfig(start.skeletonConfig(nil), nil, "v1.0.0")
	o := optionsFromManifest(m)

	f := o.wizardForm()
	f.Update(f.Init())

	var model huh.Model = f
	finishWizard(t, f, model)
	require.NoError(t, o.afterWizard())

	again := generator.ManifestFromSkeletonConfig(o.skeletonConfig(nil), nil, "v1.0.0")
	assert.Equal(t, m.Properties.Config, again.Properties.Config)
}

// Every format and every kind is on the first paint of its list: huh sizes an
// auto-height list to its options and then subtracts the title and
// description, which cut the last two formats off (as #43 did for features).
func TestWizard_ConfigurationListsAreWhollyVisible(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-formats")
	require.Equal(t, "config-formats", f.GetFocusedField().GetKey())

	view := fmt.Sprint(f.View())
	for _, format := range generator.ConfigFormats() {
		assert.Containsf(t, view, format+" ", "format %q is not on the first paint", format)
	}

	driveUntilKey(f, m, o, "config-source-0-kind")
	require.Equal(t, "config-source-0-kind", f.GetFocusedField().GetKey())

	view = fmt.Sprint(f.View())
	for _, kind := range offeredKinds(o)[1:] {
		assert.Containsf(t, view, kindGloss(kind), "kind %q is not on the first paint", kind)
	}
}

// Every docs link the Configuration page prints names a page this repository
// publishes, and its anchor a heading on it, so a moved page fails here
// rather than in front of a user.
func TestWizard_ConfigurationDocsLinksResolve(t *testing.T) {
	t.Parallel()

	link := regexp.MustCompile(regexp.QuoteMeta(docsBase) + `(/[^\s#]*)(#[a-z-]+)?`)
	links := link.FindAllStringSubmatch(configurationBlurb+sourcesBlurb, -1)
	require.Len(t, links, 4)

	for _, l := range links {
		page := strings.Trim(l[1], "/")
		file := filepath.Join("..", "..", "..", "..", "docs", page+".md")

		body, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			file = filepath.Join("..", "..", "..", "..", "docs", page, "index.md")
			body, err = os.ReadFile(file)
		}

		require.NoErrorf(t, err, "%s has no page in docs/", l[0])

		if anchor := strings.TrimPrefix(l[2], "#"); anchor != "" {
			assert.Containsf(t, headingAnchors(string(body)), anchor, "%s has no heading for its anchor", l[0])
		}
	}
}

// headingAnchors are the ids zensical gives a page's headings: lower case,
// punctuation dropped, spaces as hyphens.
func headingAnchors(markdown string) []string {
	var anchors []string

	drop := regexp.MustCompile("[^a-z0-9 -]")

	for _, line := range strings.Split(markdown, "\n") {
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			anchors = append(anchors, strings.ReplaceAll(drop.ReplaceAllString(strings.ToLower(heading), ""), " ", "-"))
		}
	}

	return anchors
}

// R1: the wizard asks no source setting, so each slot page says which
// settings its kind reads and where they come from.
func TestSettingsNote(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "mytool", Features: []string{"init"}, sourceSlots: []wizardSource{{Kind: "vault"}, {Kind: "vault", Name: "shared"}, {Kind: "etcd"}}}

	note := shownNote(t, o.settingsNote(0))
	assert.Contains(t, note, "Config is not set here")
	assert.Contains(t, note, `"mytool init config vault"`)
	assert.Contains(t, note, "~/.mytool/config.yaml")
	assert.Contains(t, note, "  config:\n    sources:\n      vault:\n        address: \"\"\n        mount: \"secret\"\n",
		"the block init config writes, with the kind's defaults")
	assert.Contains(t, note, "        auth:\n          env: \"\"", "a dotted key nests")
	assert.Contains(t, note, "pkg/cmd/root/assets/config.yaml")
	assert.Contains(t, note, configSourcesDocs)

	assert.Contains(t, shownNote(t, o.settingsNote(1)), `"mytool init config shared"`, "the slot's own name")

	etcd := shownNote(t, o.settingsNote(2))
	assert.Contains(t, etcd, "Your own code")
	assert.Contains(t, etcd, overrideHowTo)

	for _, line := range strings.Split(note, "\n") {
		if strings.HasPrefix(line, "https://") {
			continue
		}

		assert.LessOrEqualf(t, len(line), noteWidth, "%q is wider than the note", line)
	}
}

// shownNote is what huh's Note shows for text: it fails on any markup huh
// would act on, an unescaped _, * or backtick, and returns the text unescaped.
func shownNote(t *testing.T, text string) string {
	t.Helper()

	var shown strings.Builder

	escaped := false

	for _, r := range text {
		switch {
		case escaped:
			shown.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '_' || r == '*' || r == '`':
			t.Errorf("unescaped %q in a note: %q", r, text)
		default:
			shown.WriteRune(r)
		}
	}

	return shown.String()
}

// D12 from the wizard's side: the keychain kind is offered only with the OS
// Keychain feature selected, the way validation already requires.
func TestWizard_TheKeychainKindFollowsTheFeature(t *testing.T) {
	t.Parallel()

	without := &SkeletonOptions{Features: []string{"init", "doctor"}}
	assert.NotContains(t, offeredKinds(without), "keychain")
	assert.Contains(t, offeredKinds(without), "vault")

	with := &SkeletonOptions{Features: []string{"init", "keychain"}}
	assert.Contains(t, offeredKinds(with), "keychain")
}

// A keychain slot added before the feature was unticked is dropped.
func TestWizard_AKeychainSlotGoesWithItsFeature(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{
		Name: "my-app", Features: []string{"init"},
		sourceSlots: []wizardSource{{Kind: "keychain", Required: true}, {Kind: "vault", Required: true}, {}},
	}

	o.applySourceSlots()
	assert.Equal(t, []string{"vault=vault"}, o.ConfigSources)
}

func offeredKinds(o *SkeletonOptions) []string {
	var kinds []string
	for _, opt := range o.kindOptions() {
		kinds = append(kinds, opt.Value)
	}

	return kinds
}

// downTo is the arrow presses from "No more config sources" to kind, in the
// list o offers.
func downTo(t *testing.T, o *SkeletonOptions, kind string) string {
	t.Helper()

	i := slices.Index(offeredKinds(o), kind)
	require.Positivef(t, i, "%q is not offered", kind)

	return strings.Repeat("↓", i)
}

// Without the Initialization feature there is no init config, so the note
// sends the author to the embedded defaults instead.
func TestSettingsNote_WithoutInit(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "moo", Features: []string{"doctor"}, sourceSlots: []wizardSource{{Kind: "vault"}}}
	note := shownNote(t, o.settingsNote(0))

	assert.NotContains(t, note, "init config")
	assert.Contains(t, note, "no init command")
	assert.Contains(t, note, "pkg/cmd/root/assets/config.yaml, where they")
	assert.Contains(t, note, "      vault:\n")
	assert.Contains(t, note, "~/.moo/config.yaml", "a user may still override them")
}

// The note follows the features and the name as well as the slot, since
// they are answered pages earlier and may be changed after.
func TestSettingsNoteBinding_TracksTheFeatures(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Name: "moo", Features: []string{"init"}, sourceSlots: []wizardSource{{Kind: "vault"}}}
	b := textBinding{func() string { return o.settingsNote(0) }}

	before, err := b.Hash()
	require.NoError(t, err)

	o.Features = []string{"doctor"}
	after, err := b.Hash()
	require.NoError(t, err)

	assert.NotEqual(t, before, after)
}
