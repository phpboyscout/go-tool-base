// Package keychain links the keychain config source kind: tokens a tool
// keeps in the OS keychain, read and written as configuration (spec 0204
// D12). It reaches the keychain only through the credentials registry, so it
// needs the keychain feature linked too (gitlab.com/phpboyscout/go-tool-base/
// pkg/setup/keychain); without it the source cannot be built. It is writable
// by default, the one kind that is, and it resolves when the store is built,
// which can prompt for an unlock.
package keychain

import (
	"context"
	"fmt"
	"time"

	"gitlab.com/phpboyscout/go/config"
	configkeychain "gitlab.com/phpboyscout/go/config-keychain"
	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "keychain"

var (
	// ErrNoKeychain is a keychain slot in a binary that links no keychain.
	ErrNoKeychain = errors.NewSentinel("gtb.config.sources.keychain.unavailable", "no keychain is available")
	// ErrNoService is a keychain slot with no service configured.
	ErrNoService = errors.NewSentinel("gtb.config.sources.keychain.no_service", "keychain config source has no service")
	// ErrKeychainUnreachable is a linked keychain that failed its reachability
	// probe, such as one behind a wedged D-Bus session or awaiting an unlock.
	ErrKeychainUnreachable = errors.NewSentinel("gtb.config.sources.keychain.unreachable", "keychain did not answer its probe")
)

// DefaultProbeTimeout bounds the reachability probe when the source sets no
// timeout. A working keychain answers in milliseconds; this keeps a hung one
// from holding every command for config-keychain's ten-second per-call bound.
const DefaultProbeTimeout = 2 * time.Second

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryFor(configkeychain.Registered(), credentials.Probe), setup.ConfigSourceInitialiserFor(Kind), setup.WritableByDefault())
}

// factoryFor builds keychain sources over a credentials backend and a probe of
// it: the registry and credentials.Probe in production, fakes in tests. A
// keychain that is not linked, or does not answer the probe, fails the factory,
// which is D12's "omit the layer" for an optional slot and a refusal for a
// required one. Available() alone cannot tell a hung keychain from a working
// one, since the OS backend reports a constant true.
func factoryFor(backend credentials.Backend, probe func(context.Context) bool) setup.SourceFactory {
	return func(ctx context.Context, settings config.Reader, _ setup.ConfigBootstrap) (config.Backend, error) {
		if !backend.Available() {
			return nil, errors.WithHint(ErrNoKeychain,
				"link gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain, or mark the source required: false")
		}

		service := settings.GetString("service")
		if service == "" {
			return nil, errors.WithHint(ErrNoService, "set config.sources.<name>.service")
		}

		var opts []configkeychain.Option

		probeTimeout := DefaultProbeTimeout

		if timeout := settings.GetString("timeout"); timeout != "" {
			d, err := time.ParseDuration(timeout)
			if err != nil {
				return nil, errors.Wrap(err, "timeout")
			}

			opts = append(opts, configkeychain.WithTimeout(d))
			probeTimeout = d
		}

		if !probeWithin(ctx, probe, probeTimeout) {
			return nil, errors.WithHintf(errors.Wrapf(ErrKeychainUnreachable, "no answer within %s", probeTimeout),
				"unlock the keychain or check its D-Bus session; raise config.sources.<name>.timeout if an unlock prompt needs longer")
		}

		return configkeychain.New(backend, service, accounts(settings.Get("keys")), opts...), nil
	}
}

// probeWithin runs probe under a deadline. credentials.Probe enforces the
// deadline itself even when the backend ignores its context.
func probeWithin(ctx context.Context, probe func(context.Context) bool, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return probe(ctx)
}

// accounts reads the keys setting: config path to keychain account.
func accounts(raw any) map[string]string {
	out := map[string]string{}

	if m, ok := raw.(map[string]any); ok {
		for key, account := range m {
			out[key] = fmt.Sprint(account)
		}
	}

	return out
}
