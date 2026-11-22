package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The scaffold ships its own copy of the publish script; both must behave
// the same, so they are kept byte-identical.
var movePointerScripts = map[string]string{
	"repository": "../../scripts/move-pointer.sh",
	"scaffold":   "../../cli/pkg/generator/assets/skeleton/scripts/move-pointer.sh",
}

func TestMovePointerScripts_AreIdentical(t *testing.T) {
	t.Parallel()

	own, err := os.ReadFile(movePointerScripts["repository"])
	require.NoError(t, err)

	scaffold, err := os.ReadFile(movePointerScripts["scaffold"])
	require.NoError(t, err)

	assert.True(t, bytes.Equal(own, scaffold), "scripts/move-pointer.sh and the scaffold's copy have drifted")
}

// A prerelease tag leaves no dist/latest.json (writeDocuments), and the
// publisher still runs the script, so an absent file is a clean no-op. The
// check runs before the credential check: with no credentials set, reaching
// that check would exit 1.
func TestMovePointerScripts_AbsentPointerIsANoOp(t *testing.T) {
	t.Parallel()

	for name, script := range movePointerScripts {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			absent := filepath.Join(t.TempDir(), "latest.json")

			cmd := exec.CommandContext(t.Context(), "bash", script, absent, "https://store.invalid/bucket/latest.json")
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}

			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			assert.Contains(t, string(out), "no pointer to move")
		})
	}
}

// A macOS runner has shasum and no sha256sum (#158). The payload hash is taken
// with whichever is there, and with neither the script stops before any
// request, so it never leaves a tag published and its pointer unmoved.
func TestMovePointerScripts_HashWithWhicheverToolIsThere(t *testing.T) {
	t.Parallel()

	pointerJSON := []byte(`{"schema":1,"tag":"v1.2.3"}` + "\n")
	sum := sha256.Sum256(pointerJSON)
	want := hex.EncodeToString(sum[:])

	for name, script := range movePointerScripts {
		for _, hasher := range []string{"sha256sum", "shasum", ""} {
			label := hasher
			if label == "" {
				label = "none"
			}

			t.Run(name+"/"+label, func(t *testing.T) {
				t.Parallel()

				dir := t.TempDir()
				pointer := filepath.Join(dir, "latest.json")
				require.NoError(t, os.WriteFile(pointer, pointerJSON, 0o600))

				log := filepath.Join(dir, "curl.log")
				cmd := exec.CommandContext(t.Context(), "bash", script, pointer, "https://store.invalid/bucket/latest.json")
				cmd.Env = []string{
					"PATH=" + restrictedPath(t, hasher),
					"CURL_LOG=" + log,
					"AWS_ACCESS_KEY_ID=id",
					"AWS_SECRET_ACCESS_KEY=secret",
				}

				out, err := cmd.CombinedOutput()
				calls, _ := os.ReadFile(log)

				if hasher == "" {
					require.Error(t, err, string(out))
					assert.Contains(t, string(out), "neither sha256sum nor shasum")
					assert.Empty(t, calls, "no request is made without a hasher")

					return
				}

				require.NoError(t, err, string(out))
				assert.Contains(t, string(calls), "x-amz-content-sha256: "+want)
			})
		}
	}
}

// restrictedPath is a PATH holding the tools move-pointer.sh uses, the named
// hasher (none when empty) and a curl that logs its arguments and answers 404
// to the read and 200 to the write.
func restrictedPath(t *testing.T, hasher string) string {
	t.Helper()

	bin := t.TempDir()

	for _, tool := range []string{"bash", "cut", "tr", "awk", "sed", "mktemp", "rm", "head", "cat", hasher} {
		if tool == "" {
			continue
		}

		target, err := exec.LookPath(tool)
		if tool == "sha256sum" && err != nil {
			t.Skip("sha256sum is not on PATH (a Mac without coreutils)")
		}

		if tool == "shasum" && err != nil {
			writeShasumShim(t, bin)

			continue
		}

		require.NoError(t, err)
		require.NoError(t, os.Symlink(target, filepath.Join(bin, tool)))
	}

	curl := "#!/bin/bash\nprintf '%s\\n' \"$@\" >> \"$CURL_LOG\"\ncase \" $* \" in *' GET '*) printf 404 ;; *) printf 200 ;; esac\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "curl"), []byte(curl), 0o700))

	return bin
}

// writeShasumShim stands in for shasum on a Linux image without Perl. It takes
// only the "-a 256 <file>" form the script uses, so a change to that call
// still fails here.
func writeShasumShim(t *testing.T, bin string) {
	t.Helper()

	sha256sum, err := exec.LookPath("sha256sum")
	if err != nil {
		t.Skip("neither shasum nor sha256sum is on PATH")
	}

	shim := "#!/bin/bash\n[ \"$#\" -eq 3 ] && [ \"$1\" = -a ] && [ \"$2\" = 256 ] || { echo \"shasum shim: unexpected arguments: $*\" >&2; exit 2; }\nexec " + sha256sum + " \"$3\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "shasum"), []byte(shim), 0o700))
}
