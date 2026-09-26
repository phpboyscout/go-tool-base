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

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "keychain"

var (
	// ErrNoKeychain is a keychain slot in a binary that links no keychain.
	ErrNoKeychain = errors.NewSentinel("gtb.config.sources.keychain.unavailable", "no keychain is available")
	// ErrNoService is a keychain slot with no service configured.
	ErrNoService = errors.NewSentinel("gtb.config.sources.keychain.no_service", "keychain config source has no service")
)

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryFor(configkeychain.Registered()), initialiser, setup.WritableByDefault())
}

// factoryFor builds keychain sources over a credentials backend: the
// registry in production, a fake in tests. An unavailable keychain fails the
// factory, which is D12's "check Available() and omit the layer" for an
// optional slot and a refusal for a required one.
func factoryFor(backend credentials.Backend) setup.SourceFactory {
	return func(_ context.Context, settings config.Reader, _ setup.ConfigBootstrap) (config.Backend, error) {
		if !backend.Available() {
			return nil, errors.WithHint(ErrNoKeychain,
				"link gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain, or mark the source required: false")
		}

		service := settings.GetString("service")
		if service == "" {
			return nil, errors.WithHint(ErrNoService, "set config.sources.<name>.service")
		}

		var opts []configkeychain.Option

		if timeout := settings.GetString("timeout"); timeout != "" {
			d, err := time.ParseDuration(timeout)
			if err != nil {
				return nil, errors.Wrap(err, "timeout")
			}

			opts = append(opts, configkeychain.WithTimeout(d))
		}

		return configkeychain.New(backend, service, accounts(settings.Get("keys")), opts...), nil
	}
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

func initialiser(p *props.Props, slot props.ConfigSource) setup.Initialiser {
	return setup.SettingsInitialiser(slot, setup.SourceSetting{
		Key:   "service",
		Title: "Keychain service",
		Description: "The service name the tool's entries are stored under. Which keys it holds is " +
			"config.sources." + slot.Name + ".keys, usually shipped in the tool's defaults",
		Default:  p.Tool.Name,
		Required: true,
	})
}
