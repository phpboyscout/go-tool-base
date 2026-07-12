// Command gtb-no-aws-smoke is a compile-time fixture that proves
// internal/cmd/keys and internal/cmd/root do not pull the AWS SDK
// into the linked binary transitively.
//
// It is the same gtb binary minus the kms signing backend. A
// regulated downstream that needs FIPS-only or air-gapped builds
// follows this exact pattern: omit the kms blank-import, keep
// everything else.
//
// The smoke is enforced by the test in cmd/gtb-no-aws-smoke/smoke_test.go
// — that test imports nothing and just asserts at runtime that the
// signing.Names() registry returns {"local"} alone. Combined with a
// go.sum / nm check in CI it gives us two angles:
//
//   - compile-time: this package builds without aws-sdk-go-v2 in the
//     dependency closure (broken if some internal package starts
//     importing the AWS SDK directly).
//   - link-time: the resulting binary does not contain any
//     github.com/aws/* symbols (broken if the kms package's init
//     side-effect sneaks in via another path).
//
// See docs/components/signing.md "Compile-time backend opt-out".
package main

import (
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/internal/version"
	pkgRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"

	// Only the local backend; no kms. If the AWS SDK ends up linked
	// despite this omission, that's the bug we want CI to catch.
	_ "gitlab.com/phpboyscout/go/signing/local"
)

func main() {
	rootCmd, p := root.NewCmdRoot(version.Get())
	pkgRoot.Execute(rootCmd, p)
}
