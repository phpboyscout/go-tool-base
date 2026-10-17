package forge

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/credentials"
	credtest "gitlab.com/phpboyscout/go/credentials/test"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/sshagenttest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Spec 0210: a generated key's passphrase goes into the keychain when the
// person asks, or the key is left to ssh-agent, or the stage declines. These
// tests share the process-global keychain backend and SSH_AUTH_SOCK, so none
// of them runs in parallel.

const (
	sshTestHome       = "/home/testuser"
	sshTestPassphrase = "a passphrase long enough"
	sshKeychainRef    = "testtool/github.ssh.passphrase"
)

// countingKeychain is a usable in-memory keychain that counts the reaches
// for the SSH passphrase entry, leaving out the probe's own round trips.
type countingKeychain struct {
	credtest.MemoryBackend

	stores, retrieves int
}

func (k *countingKeychain) Store(ctx context.Context, service, account, secret string) error {
	if account == gitHubProfile.sshPassphraseAccount() {
		k.stores++
	}

	return k.MemoryBackend.Store(ctx, service, account, secret)
}

func (k *countingKeychain) Retrieve(ctx context.Context, service, account string) (string, error) {
	if account == gitHubProfile.sshPassphraseAccount() {
		k.retrieves++
	}

	return k.MemoryBackend.Retrieve(ctx, service, account)
}

func (k *countingKeychain) Available() bool { return true }

func installCountingKeychain(t *testing.T) *countingKeychain {
	t.Helper()

	k := &countingKeychain{}
	credentials.RegisterBackend(k)
	t.Cleanup(func() { credentials.RegisterBackend(noKeychain{}) })

	return k
}

// noKeychain is a build without a linked keychain: the probe fails.
type noKeychain struct{}

func (noKeychain) Store(context.Context, string, string, string) error {
	return credentials.ErrCredentialUnsupported
}

func (noKeychain) Retrieve(context.Context, string, string) (string, error) {
	return "", credentials.ErrCredentialUnsupported
}

func (noKeychain) Delete(context.Context, string, string) error {
	return credentials.ErrCredentialUnsupported
}

func (noKeychain) Available() bool { return false }

func installNoKeychain(t *testing.T) {
	t.Helper()

	credentials.RegisterBackend(noKeychain{})
}

type sshStageRun struct {
	p    *props.Props
	log  interface{ Entries() []logger.Entry }
	init *Initialiser
}

func newSSHStageRun(t *testing.T) sshStageRun {
	t.Helper()
	t.Setenv("HOME", sshTestHome)
	t.Setenv("CI", "")

	p := newTestProps(t)
	buf := logger.NewBuffer()
	p.Logger = buf

	return sshStageRun{
		p:   p,
		log: buf,
		init: New(p, gitHubProfile,
			keyManager(nil, errors.Wrap(forge.ErrNotSupported, "no key API"))),
	}
}

func (r sshStageRun) answer(t *testing.T, lines ...string) {
	t.Helper()

	r.p.IO, _ = answersIO(lines...)
}

func (r sshStageRun) logged() string {
	var b strings.Builder

	for _, e := range r.log.Entries() {
		fmt.Fprintln(&b, e.Level, e.Message, e.Keyvals)
	}

	return b.String()
}

func (r sshStageRun) generatedKeys(t *testing.T) []string {
	t.Helper()

	keys, err := afero.Glob(r.p.FS, filepath.Join(sshTestHome, ".ssh", "id_testtool_*"))
	require.NoError(t, err)

	return keys
}

func configFile(t *testing.T, p *props.Props) string {
	t.Helper()

	raw, err := afero.ReadFile(p.FS, "/cfgdir/config.yaml")
	require.NoError(t, err)

	return string(raw)
}

// D2 and D1: the person says Yes, so the passphrase is stored and config
// carries a reference to it and never the passphrase itself.
func TestSSHStage_StoresThePassphraseWhenAsked(t *testing.T) {
	keychain := installCountingKeychain(t)
	sshagenttest.None(t)

	run := newSSHStageRun(t)
	cfg := newTestEditor(t, run.p, "")
	run.answer(t, sshChoiceNumber(t, run.p, sshChoiceGenerate), "y", sshTestPassphrase)

	require.NoError(t, run.init.configureSSH(t.Context(), run.p, cfg))

	stored, err := keychain.MemoryBackend.Retrieve(t.Context(), "testtool", "github.ssh.passphrase")
	require.NoError(t, err)
	assert.Equal(t, sshTestPassphrase, stored)
	assert.Equal(t, 1, keychain.stores)

	view := cfg.View()
	assert.Equal(t, sshKeychainRef, view.GetString("github.ssh.key.keychain"))
	assert.Equal(t, "file", view.GetString("github.ssh.key.type"))
	assert.Contains(t, view.GetString("github.ssh.key.path"), ".ssh/id_testtool_")

	assert.NotContains(t, configFile(t, run.p), sshTestPassphrase)
	assert.NotContains(t, run.logged(), sshTestPassphrase)
}

// D4: the person says No and an agent is running, so the key is generated in
// file mode with no reference, and they are told to add it to the agent.
func TestSSHStage_NoStoreWithAnAgentSaysSSHAdd(t *testing.T) {
	keychain := installCountingKeychain(t)
	sshagenttest.Start(t)

	run := newSSHStageRun(t)
	cfg := newTestEditor(t, run.p, "")
	run.answer(t, sshChoiceNumber(t, run.p, sshChoiceGenerate), "n", sshTestPassphrase)

	require.NoError(t, run.init.configureSSH(t.Context(), run.p, cfg))

	assert.Zero(t, keychain.stores)

	view := cfg.View()
	keyPath := view.GetString("github.ssh.key.path")
	assert.Equal(t, "file", view.GetString("github.ssh.key.type"))
	assert.Empty(t, view.GetString("github.ssh.key.keychain"))
	assert.Contains(t, run.logged(), "ssh-add "+keyPath)
	assert.NotContains(t, run.logged(), sshTestPassphrase)
}

// D4: with no usable keychain the store question is never asked. The second
// answer is the passphrase; were the question shown, it would be read as the
// answer to it instead.
func TestSSHStage_NoKeychainWithAnAgentSkipsTheQuestion(t *testing.T) {
	installNoKeychain(t)
	sshagenttest.Start(t)

	run := newSSHStageRun(t)
	cfg := newTestEditor(t, run.p, "")
	run.answer(t, sshChoiceNumber(t, run.p, sshChoiceGenerate), sshTestPassphrase)

	require.NoError(t, run.init.configureSSH(t.Context(), run.p, cfg))

	view := cfg.View()
	keyPath := view.GetString("github.ssh.key.path")
	assert.Equal(t, "file", view.GetString("github.ssh.key.type"))
	assert.Empty(t, view.GetString("github.ssh.key.keychain"))
	assert.Contains(t, run.logged(), "ssh-add "+keyPath)
}

// D4: no keychain and no agent, so nothing could open the key. The stage
// declines before the passphrase page, generates nothing and writes nothing.
func TestSSHStage_DeclinesWithNoKeychainAndNoAgent(t *testing.T) {
	installNoKeychain(t)
	sshagenttest.None(t)

	run := newSSHStageRun(t)
	cfg := newTestEditor(t, run.p, "")
	run.answer(t, sshChoiceNumber(t, run.p, sshChoiceGenerate))

	require.NoError(t, run.init.configureSSH(t.Context(), run.p, cfg))

	assert.Empty(t, run.generatedKeys(t))
	assert.False(t, cfg.View().IsSet("github.ssh.key.type"))
	assert.Contains(t, run.logged(), "ssh-agent")
}

// D4: answering No with no agent follows the row below it, the decline.
func TestSSHStage_NoStoreWithoutAnAgentDeclines(t *testing.T) {
	keychain := installCountingKeychain(t)
	sshagenttest.None(t)

	run := newSSHStageRun(t)
	cfg := newTestEditor(t, run.p, "")
	run.answer(t, sshChoiceNumber(t, run.p, sshChoiceGenerate), "n")

	require.NoError(t, run.init.configureSSH(t.Context(), run.p, cfg))

	assert.Zero(t, keychain.stores)
	assert.Empty(t, run.generatedKeys(t))
	assert.False(t, cfg.View().IsSet("github.ssh.key.type"))
}

// D5: picking ssh-agent removes a reference an earlier run left, and leaves
// the keychain entry itself alone.
func TestSSHStage_PickingTheAgentClearsAStaleReference(t *testing.T) {
	keychain := installCountingKeychain(t)
	sshagenttest.None(t)
	require.NoError(t, keychain.MemoryBackend.Store(t.Context(), "testtool", "github.ssh.passphrase", "old"))

	run := newSSHStageRun(t)
	cfg := newTestEditor(t, run.p,
		"github:\n  ssh:\n    key:\n      type: file\n      path: /old/key\n      keychain: "+sshKeychainRef+"\n")
	run.answer(t, sshChoiceNumber(t, run.p, sshChoiceAgent))

	require.NoError(t, run.init.configureSSH(t.Context(), run.p, cfg))

	view := cfg.View()
	assert.Equal(t, "agent", view.GetString("github.ssh.key.type"))
	assert.False(t, view.IsSet("github.ssh.key.keychain"))
	assert.False(t, view.IsSet("github.ssh.key.path"))

	kept, err := keychain.MemoryBackend.Retrieve(t.Context(), "testtool", "github.ssh.passphrase")
	require.NoError(t, err)
	assert.Equal(t, "old", kept)
}

// D6 and D2: an existing encrypted key, picked from the list, is never asked
// about and gets the ssh-add warning.
func TestSSHStage_ExistingEncryptedKeyFromTheListWarns(t *testing.T) {
	keychain := installCountingKeychain(t)
	sshagenttest.None(t)

	run := newSSHStageRun(t)
	keyPath := filepath.Join(sshTestHome, ".ssh", "id_existing")
	require.NoError(t, run.p.FS.MkdirAll(filepath.Dir(keyPath), 0o700))
	_, err := generateAndSaveSSHKey(run.p.FS, keyPath, sshTestPassphrase)
	require.NoError(t, err)

	cfg := newTestEditor(t, run.p, "")
	run.answer(t, sshChoiceNumber(t, run.p, keyPath))

	require.NoError(t, run.init.configureSSH(t.Context(), run.p, cfg))

	assert.Zero(t, keychain.stores)
	assert.Equal(t, keyPath, cfg.View().GetString("github.ssh.key.path"))
	assert.Contains(t, run.logged(), "ssh-add "+keyPath)
}

// D6: the same warning for an encrypted key whose path is typed in.
func TestSSHStage_ExistingEncryptedKeyByPathWarns(t *testing.T) {
	installCountingKeychain(t)
	sshagenttest.None(t)

	run := newSSHStageRun(t)
	keyPath := "/keys/id_elsewhere"
	require.NoError(t, run.p.FS.MkdirAll("/keys", 0o700))
	_, err := generateAndSaveSSHKey(run.p.FS, keyPath, sshTestPassphrase)
	require.NoError(t, err)

	cfg := newTestEditor(t, run.p, "")
	run.answer(t, sshChoiceNumber(t, run.p, sshChoiceOther), keyPath)

	require.NoError(t, run.init.configureSSH(t.Context(), run.p, cfg))

	assert.Equal(t, keyPath, cfg.View().GetString("github.ssh.key.path"))
	assert.Contains(t, run.logged(), "ssh-add "+keyPath)
}

// D9: init <forge> runs the SSH stage again with a key already recorded, so a
// person can replace a key generated before its passphrase could be stored.
func TestRunForgeInit_RerunsTheSSHStage(t *testing.T) {
	installNoKeychain(t)
	sshagenttest.None(t)
	t.Setenv("HOME", sshTestHome)
	t.Setenv("CI", "")

	p := newTestProps(t)
	cfg := newTestEditor(t, p, "gitea:\n  ssh:\n    key:\n      type: file\n      path: /old/key\n")

	p.IO, _ = answersIO(modeNumber(t, credentials.ModeEnvVar), "GITEA_TOKEN", "n",
		sshChoiceNumber(t, p, sshChoiceAgent))

	require.NoError(t, RunForgeInit(t.Context(), p, cfg, giteaProfile))

	assert.Equal(t, "agent", cfg.View().GetString("gitea.ssh.key.type"))
}

// D9: init bitbucket does the same.
func TestRunBitbucketInit_RerunsTheSSHStage(t *testing.T) {
	installNoKeychain(t)
	sshagenttest.None(t)
	t.Setenv("HOME", sshTestHome)
	t.Setenv("CI", "")

	p := newDualTestProps(t)
	cfg := newTestEditor(t, p, "bitbucket:\n  ssh:\n    key:\n      type: file\n      path: /old/key\n")

	p.IO, _ = answersIO(modeNumber(t, credentials.ModeEnvVar), "BB_USER", "BB_APP_PW",
		sshChoiceNumber(t, p, sshChoiceAgent))

	require.NoError(t, RunBitbucketInit(t.Context(), p, cfg))

	assert.Equal(t, "agent", cfg.View().GetString("bitbucket.ssh.key.type"))
}

// D9: plain init still skips a recorded stage.
func TestConfigure_StillSkipsARecordedSSHStage(t *testing.T) {
	installNoKeychain(t)
	sshagenttest.None(t)
	t.Setenv("HOME", sshTestHome)
	t.Setenv("CI", "")

	p := newDualTestProps(t)
	cfg := newTestEditor(t, p, "bitbucket:\n  ssh:\n    key:\n      type: file\n      path: /old/key\n")

	p.IO = dualEnvIO(t, "BB_USER", "BB_APP_PW")

	require.NoError(t, NewBitbucketInitialiser(p).Configure(t.Context(), p, cfg))

	assert.Equal(t, "/old/key", cfg.View().GetString("bitbucket.ssh.key.path"))
}

// D1: a keychain that refuses the write fails the stage before any key is
// generated, naming the way out.
func TestStoreSSHPassphrase_Failures(t *testing.T) {
	installNoKeychain(t)

	_, err := storeSSHPassphrase(t.Context(), gitHubProfile, "", sshTestPassphrase)
	require.ErrorContains(t, err, "tool name")

	_, err = storeSSHPassphrase(t.Context(), gitHubProfile, "testtool", sshTestPassphrase)
	require.ErrorIs(t, err, credentials.ErrCredentialUnsupported)
	assert.NotContains(t, err.Error(), sshTestPassphrase)
}
