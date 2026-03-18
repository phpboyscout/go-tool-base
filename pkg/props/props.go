package props

import (
	"github.com/charmbracelet/log"
	"github.com/spf13/afero"

	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/errorhandling"
	"github.com/phpboyscout/gtb/pkg/version"
)

type Props struct {
	Tool         Tool
	Logger       *log.Logger
	Config       config.Containable
	Assets       Assets
	FS           afero.Fs
	Version      version.Version
	ErrorHandler errorhandling.ErrorHandler
}
