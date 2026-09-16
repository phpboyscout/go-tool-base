package main

// Side-effect import: activates OS keychain support for the shipped
// gtb binary. pkg/setup/keychain registers the go-keyring-backed
// backend and declares the keychain feature as a link kind, so doctor
// and the generator's catalogue know this binary carries it.
//
// This file is the single on/off switch for keychain in the shipped
// gtb binary. Regulated builds that must carry no IPC-to-keychain
// code delete it and rebuild — linker dead-code elimination then
// keeps go-keyring, godbus, and wincred out of the artefact.
// Downstream tools built on GTB that want the same regulatory
// posture simply omit the equivalent blank import from their own
// cmd package; consumer binaries only link the keychain chain when
// the consumer opts in.
import _ "gitlab.com/phpboyscout/go-tool-base/pkg/setup/keychain"
