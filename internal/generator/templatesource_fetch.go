package generator

// Resolving a template source to a readable tree. A local source is read
// directly from the operator's filesystem (validated, contained on the write
// side). A git source is cloned — inertly — into a per-source, SHA-pinned
// cache under the XDG user cache dir, keyed by the resolved commit so a pin is
// immutable and shareable across projects.
//
// The clone step is injected (Generator.cloneTemplate) so tests exercise the
// overlay, cache, and offline behaviour against a local bare remote or a fake
// without any real network. The default implementation uses pkg/vcs/repo's
// provider-aware clone/auth — no second auth path.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// resolvedSource is the outcome of resolving a TemplateSource: a read FS
// rooted at the source tree, the resolved git SHA (git sources) or content
// fingerprint (local sources), and the parsed descriptor.
type resolvedSource struct {
	fs          afero.Fs
	root        string
	resolvedSHA string
	fingerprint string
	descriptor  TemplateDescriptor
}

// templateCacheRoot returns the base directory git template sources are cached
// under: $XDG_CACHE_HOME/gtb/templates (falling back to os.UserCacheDir). The
// FS argument lets tests redirect the cache onto a MemMapFs.
func templateCacheRoot() (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		return filepath.Join(xdg, "gtb", "templates"), nil
	}

	base, err := os.UserCacheDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve user cache dir")
	}

	return filepath.Join(base, "gtb", "templates"), nil
}

// cacheDirFor returns the SHA-keyed cache directory for a git source:
// <cacheRoot>/<host>/<owner>/<repo>@<sha>. The host/owner/repo come from the
// location; sha is the resolved pin.
func cacheDirFor(cacheRoot, host, owner, repo, sha string) string {
	return filepath.Join(cacheRoot, host, owner, repo+"@"+sha)
}

// cloneRequest is the input to a Generator.cloneTemplate call.
type cloneRequest struct {
	// URL is the full clone URL built from the forge host and the location.
	URL string
	// Ref is the operator-specified ref (branch/tag/commit), or "" for the
	// default branch.
	Ref string
	// TargetDir is the on-disk directory the clone should populate.
	TargetDir string
}

// cloneResult is the output of a clone: the resolved commit SHA the ref
// checked out to.
type cloneResult struct {
	ResolvedSHA string
}

// templateCloneFunc clones a git template source into TargetDir at Ref and
// returns the resolved SHA. It must fetch inertly (no hooks, no submodule
// recursion, no filters). Injected on the Generator so tests can supply a
// local-remote or fake implementation.
type templateCloneFunc func(req cloneRequest) (cloneResult, error)

// resolveTemplateSource resolves a single TemplateSource to a readable tree.
// For local sources it reads the operator's path and computes a content
// fingerprint. For git sources it consults the SHA-keyed cache; on a cache
// miss it clones via the injected cloneTemplate. When offline (cloneTemplate
// is nil or errors) and the cache is cold, it returns a clear error rather
// than silently restoring any suppressed embedded scaffold (D9).
func (g *Generator) resolveTemplateSource(ts TemplateSource) (*resolvedSource, error) {
	if err := ValidateTemplateSource(&ts); err != nil {
		return nil, err
	}

	switch ts.Type {
	case TemplateSourceLocal:
		return g.resolveLocalSource(ts)
	case TemplateSourceGit:
		return g.resolveGitSource(ts)
	default:
		return nil, errors.Newf("unknown template source type %q", ts.Type)
	}
}

// resolveLocalSource reads a local template folder directly from the
// generator's filesystem, parses its descriptor, and fingerprints the tree.
func (g *Generator) resolveLocalSource(ts TemplateSource) (*resolvedSource, error) {
	root := ts.Location

	exists, err := afero.DirExists(g.props.FS, root)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to stat local template source %q", root)
	}

	if !exists {
		return nil, errors.WithHintf(
			errors.Newf("local template source %q does not exist", root),
			"check the path, or run from the directory that contains it",
		)
	}

	desc, err := parseTemplateDescriptor(g.props.FS, root)
	if err != nil {
		return nil, err
	}

	fp, err := fingerprintTree(g.props.FS, root)
	if err != nil {
		return nil, err
	}

	return &resolvedSource{
		fs:          g.props.FS,
		root:        root,
		fingerprint: fp,
		descriptor:  desc,
	}, nil
}

// resolveGitSource resolves a git source through the SHA-keyed cache. The pin
// is whichever SHA the source already records (Resolved) — preferred so
// regenerate is byte-stable and offline-capable. With no pin yet (first add),
// the injected cloneTemplate resolves Ref to a SHA and populates the cache.
func (g *Generator) resolveGitSource(ts TemplateSource) (*resolvedSource, error) {
	host, owner, repo, err := splitGitLocation(ts.Location, g.templateDefaultHost())
	if err != nil {
		return nil, err
	}

	cacheRoot, err := templateCacheRoot()
	if err != nil {
		return nil, err
	}

	// If we already have a pin, prefer the warm cache: that is what makes
	// regenerate reproducible and offline-capable.
	if ts.Resolved != "" {
		dir := cacheDirFor(cacheRoot, host, owner, repo, ts.Resolved)
		if warm, _ := afero.DirExists(g.props.FS, dir); warm {
			return g.openCachedSource(dir, ts.Resolved)
		}
	}

	// Cache miss (or no pin yet): clone. Offline cold cache ⇒ clear error.
	if g.cloneTemplate == nil {
		return nil, errors.WithHintf(
			errors.Newf("git template source %q is not cached and cloning is unavailable (offline)", ts.Location),
			"restore connectivity to fetch the pinned commit, or remove the source; gtb will not silently fall back to the embedded scaffold",
		)
	}

	sha, err := g.cloneIntoCache(ts, cloneRequest{URL: buildTemplateCloneURL(host, owner, repo), Ref: ts.Ref}, host, cacheDirFor(cacheRoot, host, owner, repo, ""))
	if err != nil {
		return nil, err
	}

	return g.openCachedSource(cacheDirFor(cacheRoot, host, owner, repo, sha), sha)
}

// cloneIntoCache clones the source into a staging dir, validates and (if
// pinned) cross-checks the resolved SHA, and places the tree into the
// SHA-keyed cache directory. cacheDirPrefix is the cache dir built with an
// empty SHA; the real dir is derived by appending the resolved SHA. It returns
// the resolved SHA.
func (g *Generator) cloneIntoCache(ts TemplateSource, req cloneRequest, host, cacheDirPrefix string) (string, error) {
	staging, err := afero.TempDir(g.props.FS, "", "gtb-template-clone-")
	if err != nil {
		return "", errors.Wrap(err, "failed to create template clone staging dir")
	}

	defer func() { _ = g.props.FS.RemoveAll(staging) }()

	req.TargetDir = staging

	res, err := g.cloneTemplate(req)
	if err != nil {
		return "", errors.WithHintf(
			errors.Wrapf(err, "failed to fetch git template source %q", ts.Location),
			"check the ref, your network, and (for private sources) the configured %s forge credentials", host,
		)
	}

	if !templateSHARe.MatchString(res.ResolvedSHA) {
		return "", errors.Newf("clone returned a malformed resolved SHA for %q", ts.Location)
	}

	// If the operator pinned a SHA and the resolved one differs, the cache
	// entry would not match the pin — reject (integrity, D9).
	if ts.Resolved != "" && ts.Resolved != res.ResolvedSHA {
		return "", errors.Newf("resolved commit %s does not match pinned %s for %q", res.ResolvedSHA, ts.Resolved, ts.Location)
	}

	dir := cacheDirPrefix + res.ResolvedSHA
	if err := g.placeCacheEntry(staging, dir); err != nil {
		return "", err
	}

	return res.ResolvedSHA, nil
}

// placeCacheEntry moves the freshly cloned staging tree to its SHA-keyed cache
// directory if that directory is not already present. A concurrent populate of
// the same pin is harmless — the SHA makes the content immutable.
func (g *Generator) placeCacheEntry(staging, dir string) error {
	if warm, _ := afero.DirExists(g.props.FS, dir); warm {
		return nil
	}

	if err := g.props.FS.MkdirAll(filepath.Dir(dir), DefaultDirMode); err != nil {
		return errors.Wrap(err, "failed to create template cache directory")
	}

	if err := g.props.FS.Rename(staging, dir); err != nil {
		// Rename across devices / MemMapFs quirks: fall back to a copy.
		if copyErr := copyTree(g.props.FS, staging, dir); copyErr != nil {
			return errors.Wrap(copyErr, "failed to populate template cache")
		}
	}

	return nil
}

// openCachedSource parses the descriptor of a warm cache entry and returns the
// resolved source rooted there.
func (g *Generator) openCachedSource(dir, sha string) (*resolvedSource, error) {
	desc, err := parseTemplateDescriptor(g.props.FS, dir)
	if err != nil {
		return nil, err
	}

	return &resolvedSource{
		fs:          g.props.FS,
		root:        dir,
		resolvedSHA: sha,
		descriptor:  desc,
	}, nil
}

// templateDefaultHost returns the host a bare git location (org/repo) resolves
// against: the tool's own forge host, falling back to the release-source host.
func (g *Generator) templateDefaultHost() string {
	if g.props.Tool.ReleaseSource.Host != "" {
		return g.props.Tool.ReleaseSource.Host
	}

	switch strings.ToLower(g.props.Tool.ReleaseSource.Type) {
	case "gitlab":
		return "gitlab.com"
	default:
		return "github.com"
	}
}

// splitGitLocation extracts host/owner/repo from a git source location. A bare
// path (org/repo, or host/group/sub/repo) resolves the host from defaultHost
// when absent. A full https URL has its host taken from the URL. The repo name
// has any trailing ".git" stripped.
func splitGitLocation(location, defaultHost string) (host, owner, repo string, err error) {
	loc := strings.TrimPrefix(strings.TrimSpace(location), "https://")
	loc = strings.TrimSuffix(strings.Trim(loc, "/"), ".git")

	segments := strings.Split(loc, "/")

	const minSegments = 2
	if len(segments) < minSegments {
		return "", "", "", errors.Newf("invalid git template location %q", location)
	}

	// Detect a leading host segment by the presence of a dot or colon.
	host = defaultHost

	first := segments[0]
	if strings.Contains(first, ".") || strings.Contains(first, ":") {
		host = first
		segments = segments[1:]
	}

	if len(segments) < minSegments {
		return "", "", "", errors.Newf("git template location %q must include an owner and repo", location)
	}

	repo = segments[len(segments)-1]
	owner = strings.Join(segments[:len(segments)-1], "/")

	if host == "" || owner == "" || repo == "" {
		return "", "", "", errors.Newf("git template location %q is incomplete", location)
	}

	return host, owner, repo, nil
}

// buildTemplateCloneURL builds the https clone URL from host/owner/repo,
// preserving nested GitLab group paths.
func buildTemplateCloneURL(host, owner, repo string) string {
	return "https://" + host + "/" + owner + "/" + repo + ".git"
}

// copyTree recursively copies the directory tree at src to dst on fs.
func copyTree(fs afero.Fs, src, dst string) error {
	return afero.Walk(fs, src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}

		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return fs.MkdirAll(target, DefaultDirMode)
		}

		content, readErr := afero.ReadFile(fs, p)
		if readErr != nil {
			return readErr
		}

		if mkErr := fs.MkdirAll(filepath.Dir(target), DefaultDirMode); mkErr != nil {
			return mkErr
		}

		return afero.WriteFile(fs, target, content, DefaultFileMode)
	})
}
