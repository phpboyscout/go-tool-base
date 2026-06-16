package credentials

// KeyWriter is the minimal config-write surface needed to clear stale
// credential keys. It is satisfied by both config.Containable and
// *viper.Viper, so the setup wizards can enforce the single-credential
// invariant on either the live config or a file-write viper without a
// dependency on the config package.
type KeyWriter interface {
	Set(key string, value any)
}

// ClearKeysExcept blanks every config key in all that is not present in
// keep. Setup wizards call it after writing the selected storage mode's
// key(s) so that re-running the wizard in a different mode never leaves
// a prior secret — or a stale reference — behind. This enforces the
// credential-storage hardening spec's invariant that exactly one of the
// env / literal / keychain key paths carries a value, so a prior mode
// cannot mask the new one at resolve time.
//
// Keys are blanked (Set to "") rather than deleted because viper has no
// unset primitive; the runtime resolution chain and the doctor
// `credentials.no-literal` check both treat an empty value as absent.
// Empty strings in all and keep are ignored.
func ClearKeysExcept(w KeyWriter, all []string, keep ...string) {
	keepSet := make(map[string]struct{}, len(keep))

	for _, k := range keep {
		if k != "" {
			keepSet[k] = struct{}{}
		}
	}

	for _, k := range all {
		if k == "" {
			continue
		}

		if _, ok := keepSet[k]; ok {
			continue
		}

		w.Set(k, "")
	}
}
