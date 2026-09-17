package credentialposture

// SingleTokenKeys is the config layout of a single-token credential under a
// prefix: the three storage-mode keys and the keychain account the wizard
// and the migration write the secret under.
type SingleTokenKeys struct {
	Env, Keychain, Literal, Account string
}

// SingleToken lays out the `<prefix>.auth.*` subtree. Every forge and the
// direct release source keep their token here, so the shape is stated once.
func SingleToken(prefix string) SingleTokenKeys {
	return SingleTokenKeys{
		Env:      prefix + ".auth.env",
		Keychain: prefix + ".auth.keychain",
		Literal:  prefix + ".auth.value",
		Account:  prefix + ".auth",
	}
}

// DualCredentialKeys is the config layout of a username and password pair
// under a prefix. Both halves share one keychain entry, which holds a JSON
// blob rather than a bare secret.
type DualCredentialKeys struct {
	User, UserEnv, Password, PasswordEnv, Keychain, Account string
}

// DualCredential lays out the `<prefix>.username`, `<prefix>.app_password`
// and shared `<prefix>.keychain` keys Bitbucket uses.
func DualCredential(prefix string) DualCredentialKeys {
	return DualCredentialKeys{
		User:        prefix + ".username",
		UserEnv:     prefix + ".username.env",
		Password:    prefix + ".app_password",
		PasswordEnv: prefix + ".app_password.env",
		Keychain:    prefix + ".keychain",
		Account:     prefix + ".auth",
	}
}
