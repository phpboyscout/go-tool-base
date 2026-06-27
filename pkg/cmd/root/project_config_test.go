package root

import (
	"testing"

	"github.com/spf13/afero"
)

func TestDiscoverProjectConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	// a repo with the project config at its root, and a nested working dir
	_ = fs.MkdirAll("/repo/sub/deep", 0o755)
	_ = afero.WriteFile(fs, "/repo/.keryx.yaml", []byte("themes: {}\n"), 0o644)

	// walks up from a nested dir to the repo-root config
	if got := discoverProjectConfig(fs, "keryx", "/repo/sub/deep"); got != "/repo/.keryx.yaml" {
		t.Errorf("nested cwd: got %q, want /repo/.keryx.yaml", got)
	}

	// found at the dir itself
	if got := discoverProjectConfig(fs, "keryx", "/repo"); got != "/repo/.keryx.yaml" {
		t.Errorf("repo root: got %q", got)
	}

	// a different tool name → not matched
	if got := discoverProjectConfig(fs, "othertool", "/repo/sub"); got != "" {
		t.Errorf("wrong tool name should not match: %q", got)
	}

	// no config anywhere above → ""
	_ = fs.MkdirAll("/elsewhere", 0o755)
	if got := discoverProjectConfig(fs, "keryx", "/elsewhere"); got != "" {
		t.Errorf("absent: got %q, want \"\"", got)
	}

	// empty inputs
	if discoverProjectConfig(fs, "", "/repo") != "" || discoverProjectConfig(fs, "keryx", "") != "" {
		t.Error("empty tool/dir should return \"\"")
	}

}
