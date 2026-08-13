package credentialposture

import (
	"context"

	"gitlab.com/phpboyscout/go/credentials"

	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

// ModeEnvironment describes the facts that decide the default storage mode.
//
// It is data rather than something this package discovers for itself, so the
// rule below is a pure function: the callers already establish both facts —
// every wizard calls credentials.Probe to decide whether to *offer* keychain —
// and passing what they know avoids a second probe and keeps the decision
// testable without mocking anything.
type ModeEnvironment struct {
	// CI reports whether this is an automated run.
	//
	// It is checked separately from Interactive because a terminal is NOT
	// evidence of a human: GitLab's runners allocate a TTY, so IsInteractive
	// reports true inside a pipeline. Relying on the terminal alone made a
	// pipeline pick the interactive default, which is exactly backwards.
	CI bool
	// Interactive reports whether a human is at the terminal.
	Interactive bool
	// KeychainUsable reports whether a keychain backend is registered AND
	// accepted a live round-trip. credentials.Probe answers this; a backend
	// that is merely linked is not enough, because an option that fails the
	// moment it is chosen is worse than one never offered.
	KeychainUsable bool
}

// DefaultStorageMode picks the storage mode to use when the operator has not
// said which they want.
//
// An environment-variable reference is an excellent CI interface and a poor
// interactive default: an exported variable is inherited by every process the
// shell spawns, so the secret is readable by everything the developer runs. A
// keychain entry stays put until something asks for it. Where a keychain is
// actually usable and a human is present, that is the better default — and the
// operator should not have to know to ask for it. Spec 0189 R6/D8.
//
// The test is deliberately the same one the setup wizard already applies when
// deciding whether to offer keychain at all, rather than a second rule that can
// disagree with it — plus an explicit CI exclusion.
//
// That exclusion is not belt-and-braces. This design originally rested on "CI
// needs no special case, because a pipeline has no terminal", and that is
// false: GitLab's runners allocate a TTY, so IsInteractive reports true inside
// a pipeline. The keychain probe happened to save it — a runner has no keychain
// — but a rule that is right only because a second condition rescues it is a
// rule waiting to be wrong. A CI run takes the CI default outright.
//
// An explicit choice always wins over this, and so does a configured default —
// this is only consulted when nothing has been stated.
func DefaultStorageMode(env ModeEnvironment) credentials.Mode {
	if !env.CI && env.Interactive && env.KeychainUsable {
		return credentials.ModeKeychain
	}

	return credentials.ModeEnvVar
}

// RecommendedLabel returns the suffix marking whichever mode DefaultStorageMode
// would pick, so a prompt's "(recommended)" follows the actual recommendation
// instead of being pinned to one option.
//
// A prompt that recommends one thing while defaulting to another is worse than
// either alone: it tells the user their considered choice is wrong.
func RecommendedLabel(env ModeEnvironment, mode credentials.Mode) string {
	if DefaultStorageMode(env) == mode {
		return " (recommended)"
	}

	return ""
}

// ModeLabels are the human-facing option labels a wizard shows. The
// "(recommended)" marker is appended by StorageModeOptions rather than being
// written into these, so it can follow the environment.
type ModeLabels struct {
	Env      string
	Keychain string
	Literal  string
}

// StorageModeOptions builds a credential wizard's storage-mode choices and the
// mode it should pre-select.
//
// It establishes the environment once — one keychain probe, not one per
// question — and uses it for both decisions, so the option list and the
// pre-selection cannot disagree. Every GTB wizard went through the same three
// steps with the recommendation hardcoded onto the environment-variable option;
// this is those steps, with the recommendation following what is actually
// recommended here.
//
// The probe is a live round-trip and the caller's context should be bounded;
// credentials.KeychainOpTimeout is the bound the wizards already use.
func StorageModeOptions(ctx context.Context, labels ModeLabels) ([]credentials.ModeChoice, credentials.Mode) {
	env := DiscoverModeEnvironment(ctx)

	choices := credentials.ModeChoices(
		credentials.IsCI(),
		env.KeychainUsable,
		labels.Env+RecommendedLabel(env, credentials.ModeEnvVar),
		labels.Keychain+RecommendedLabel(env, credentials.ModeKeychain),
		labels.Literal,
	)

	return choices, DefaultStorageMode(env)
}

// DiscoverModeEnvironment establishes the environment from the running process.
//
// It lives at the edge on purpose. Library code takes a ModeEnvironment as
// data so its behaviour is a function of its inputs; only a command, which is
// already the process, discovers what the process looks like. A library that
// probed for itself would behave differently under `go test`, under a pipe and
// under a terminal — and would be untestable without faking the world.
//
// The caller should bound ctx: the probe is a live keychain round-trip.
func DiscoverModeEnvironment(ctx context.Context) ModeEnvironment {
	return ModeEnvironment{
		CI:             credentials.IsCI(),
		Interactive:    utils.IsInteractive(),
		KeychainUsable: credentials.Probe(ctx),
	}
}
