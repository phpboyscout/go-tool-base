package root

// Both tools run from this package directory inside the nested cli module.
// --project-root climbs to the repository root (where docs/ and the changelog
// history live) and --target-dir is this package's assets/, which root.go
// embeds. internal/repopolicy resolves these paths in a test, because the
// directives once moved with the package and kept pre-move paths (#39).
//go:generate go tool docs --project-root ../../../.. --target-dir cli/pkg/cmd/root/assets
//go:generate go tool changelog generate --output assets/CHANGELOG.md
