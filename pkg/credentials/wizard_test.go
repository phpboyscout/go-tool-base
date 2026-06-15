package credentials_test

import (
	"testing"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentials"
)

func TestIsCI(t *testing.T) {
	// Not parallel: mutates the CI environment variable.
	t.Setenv("CI", "true")

	if !credentials.IsCI() {
		t.Fatal("IsCI() = false, want true when CI=true")
	}

	t.Setenv("CI", "1")

	if credentials.IsCI() {
		t.Fatal("IsCI() = true, want false when CI != \"true\"")
	}

	t.Setenv("CI", "")

	if credentials.IsCI() {
		t.Fatal("IsCI() = true, want false when CI empty")
	}
}

func TestValidateEnvVarName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty", input: "", wantErr: true},
		{name: "valid simple", input: "TOKEN", wantErr: false},
		{name: "valid with digits and underscore", input: "GITHUB_TOKEN_2", wantErr: false},
		{name: "lowercase rejected", input: "token", wantErr: true},
		{name: "leading digit rejected", input: "1TOKEN", wantErr: true},
		{name: "leading underscore rejected", input: "_TOKEN", wantErr: true},
		{name: "hyphen rejected", input: "MY-TOKEN", wantErr: true},
		{name: "space rejected", input: "MY TOKEN", wantErr: true},
		{name: "too long rejected", input: "A234567890123456789012345678901234567890123456789012345678901234567890", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := credentials.ValidateEnvVarName(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateEnvVarName(%q) err = %v, wantErr = %v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestRefuseLiteralUnderCI(t *testing.T) {
	// Not parallel: mutates the CI environment variable.
	t.Run("literal under CI is refused", func(t *testing.T) {
		t.Setenv("CI", "true")

		if err := credentials.RefuseLiteralUnderCI(credentials.ModeLiteral); err == nil {
			t.Fatal("RefuseLiteralUnderCI(ModeLiteral) = nil under CI, want error")
		}
	})

	t.Run("env var under CI is allowed", func(t *testing.T) {
		t.Setenv("CI", "true")

		if err := credentials.RefuseLiteralUnderCI(credentials.ModeEnvVar); err != nil {
			t.Fatalf("RefuseLiteralUnderCI(ModeEnvVar) = %v under CI, want nil", err)
		}
	})

	t.Run("keychain under CI is allowed", func(t *testing.T) {
		t.Setenv("CI", "true")

		if err := credentials.RefuseLiteralUnderCI(credentials.ModeKeychain); err != nil {
			t.Fatalf("RefuseLiteralUnderCI(ModeKeychain) = %v under CI, want nil", err)
		}
	})

	t.Run("literal outside CI is allowed", func(t *testing.T) {
		t.Setenv("CI", "")

		if err := credentials.RefuseLiteralUnderCI(credentials.ModeLiteral); err != nil {
			t.Fatalf("RefuseLiteralUnderCI(ModeLiteral) = %v outside CI, want nil", err)
		}
	})
}

func TestStorageModeOptions(t *testing.T) {
	t.Parallel()

	modesOf := func(opts []huh.Option[credentials.Mode]) map[credentials.Mode]bool {
		got := make(map[credentials.Mode]bool, len(opts))
		for _, o := range opts {
			got[o.Value] = true
		}

		return got
	}

	t.Run("non-CI keychain usable offers all three", func(t *testing.T) {
		t.Parallel()

		got := modesOf(credentials.StorageModeOptions(false, true, "env", "keychain", "literal"))
		if !got[credentials.ModeEnvVar] || !got[credentials.ModeKeychain] || !got[credentials.ModeLiteral] {
			t.Fatalf("expected all three modes, got %v", got)
		}
	})

	t.Run("CI hides literal", func(t *testing.T) {
		t.Parallel()

		got := modesOf(credentials.StorageModeOptions(true, true, "env", "keychain", "literal"))
		if got[credentials.ModeLiteral] {
			t.Fatal("literal must be hidden under CI")
		}

		if !got[credentials.ModeEnvVar] {
			t.Fatal("env-var must always be offered")
		}
	})

	t.Run("keychain unusable hides keychain", func(t *testing.T) {
		t.Parallel()

		got := modesOf(credentials.StorageModeOptions(false, false, "env", "keychain", "literal"))
		if got[credentials.ModeKeychain] {
			t.Fatal("keychain must be hidden when not usable")
		}
	})
}
