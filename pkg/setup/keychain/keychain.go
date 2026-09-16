// Package keychain links the OS keychain credential backend and declares it as
// a feature. A tool activates keychain support by blank-importing this package
// from its main (the generator writes cmd/<name>/keychain.go to do so); the
// import registers go/credentials/keychain's backend and declares the
// "keychain" feature as a link kind, so the running tool's feature set, doctor
// and the generator's catalogue all know the binary carries it (spec 0199 OQ3,
// spec 0197 D8).
//
// Deleting the import produces a regulated build: linker dead-code elimination
// keeps go-keyring, godbus and wincred out of the artefact, and the feature is
// simply not declared.
package keychain

import (
	_ "gitlab.com/phpboyscout/go/credentials/keychain"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// KeychainFeature is the feature this import declares.
const KeychainFeature = props.FeatureID("keychain")

// PackagePath is this package's import path, the ConstPackage the descriptor
// carries so generated source can qualify KeychainFeature.
const PackagePath = "gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain"

func init() {
	props.RegisterFeature(props.FeatureDescriptor{
		ID:           KeychainFeature,
		ConstName:    "KeychainFeature",
		ConstPackage: PackagePath,
		Kind:         props.KindLink,
	})
}
