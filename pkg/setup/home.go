package setup

import (
	"os"
	"os/user"
	"path/filepath"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrNoHomeDir is returned where a home directory is needed and neither HOME nor
// the user's account names one that exists.
var ErrNoHomeDir = errors.NewSentinel("gtb.setup.no_home_dir", "no home directory found")

// UserHomeDir returns the user's home directory, or "" when there is none: HOME
// if it names an existing directory, otherwise the account's. A relative path
// never qualifies, because it would resolve against the working directory.
func UserHomeDir() string {
	return firstUsableHome(os.UserHomeDir, accountHome)
}

func accountHome() (string, error) {
	account, err := user.Current()
	if err != nil {
		return "", errors.Wrap(err, "looking up the current user")
	}

	return account.HomeDir, nil
}

func firstUsableHome(sources ...func() (string, error)) string {
	for _, source := range sources {
		if dir, err := source(); err == nil && usableHome(dir) {
			return dir
		}
	}

	return ""
}

func usableHome(dir string) bool {
	if !filepath.IsAbs(dir) {
		return false
	}

	info, err := os.Stat(dir)

	return err == nil && info.IsDir()
}
