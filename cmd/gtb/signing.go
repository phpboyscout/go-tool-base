package main

// Side-effect imports: activate the signing backends shipped with
// the standard gtb binary by registering them via each backend's
// init().
//
// This file is the single on/off switch for signing backends in the
// shipped gtb binary, mirroring the keychain on/off pattern in
// keychain.go. Regulated downstream builds that need to drop a
// particular SDK (e.g. the AWS SDK for FIPS-only environments) omit
// the corresponding blank import here and rebuild — linker dead-code
// elimination then keeps that SDK out of the artefact.
//
// The companion cmd/gtb-no-aws-smoke binary deliberately omits the
// kms import to prove that the AWS SDK is not pulled in transitively
// by anything inside internal/cmd/keys or internal/cmd/root.
import (
	_ "gitlab.com/phpboyscout/go/signing-aws-kms"
	_ "gitlab.com/phpboyscout/go/signing/local"
)
