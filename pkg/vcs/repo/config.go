package repo

import (
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

type diagnosticLogger interface {
	Debug(msg string, keyvals ...any)
	Warn(msg string, keyvals ...any)
}

// Settings contains the typed configuration NewRepo needs to resolve git
// authentication without depending on GTB props or config containers.
type Settings struct {
	ReleaseSource release.ReleaseSourceConfig
	Forge         string
	AuthEnabled   bool
	Auth          vcs.TokenConfig
	SSH           SSHSettings
	Logger        diagnosticLogger
	FS            afero.Fs
}

// SSHSettings describes the forge SSH key configuration resolved by adapter
// code. Configured tracks whether the forge's SSH block exists at all; HasKey
// distinguishes a present-but-scalar SSH block from a structured ssh.key block.
type SSHSettings struct {
	Configured bool
	HasKey     bool
	Type       string
	Env        string
	Path       string
}
