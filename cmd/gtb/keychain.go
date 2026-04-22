package main

// Side-effect import: activates OS keychain support for the shipped
// gtb binary by registering the go-keyring-backed backend via
// pkg/credentials/keychain's init().
//
// This file is the single on/off switch for keychain in the shipped
// gtb binary. Regulated builds that must carry no IPC-to-keychain
// code delete it and rebuild — linker dead-code elimination then
// keeps go-keyring, godbus, and wincred out of the artefact.
// Downstream tools built on GTB that want the same regulatory
// posture simply omit the equivalent blank import from their own
// cmd package; consumer binaries only link the keychain chain when
// the consumer opts in.
import _ "github.com/phpboyscout/go-tool-base/pkg/credentials/keychain"
