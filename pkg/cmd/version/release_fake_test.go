package version

import (
	"context"
	"io"
	"time"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"
)

// fakeRelease is the smallest forge.Release the version command reads.
type fakeRelease struct{ tag string }

func (r fakeRelease) GetName() string                 { return r.tag }
func (r fakeRelease) GetTagName() string              { return r.tag }
func (r fakeRelease) GetBody() string                 { return "" }
func (r fakeRelease) GetDraft() bool                  { return false }
func (r fakeRelease) GetAssets() []forge.ReleaseAsset { return nil }

// GetReleasedAt is the zero time: a test double has no forge publish time
// (go/forge v0.28.0 notes).
func (r fakeRelease) GetReleasedAt() time.Time { return time.Time{} }

// fakeProvider answers every release query with one tag, or one error. It is
// injected through props.Tool.ReleaseProvider, the seam the self-updater
// already consults before the registry, so these tests exercise the version
// command against the forge contract rather than against one forge's HTTP
// shape.
type fakeProvider struct {
	tag string
	err error
}

func (p fakeProvider) GetLatestRelease(context.Context, string, string) (forge.Release, error) {
	if p.err != nil {
		return nil, p.err
	}

	return fakeRelease{tag: p.tag}, nil
}

func (p fakeProvider) GetReleaseByTag(ctx context.Context, owner, repo, _ string) (forge.Release, error) {
	return p.GetLatestRelease(ctx, owner, repo)
}

func (p fakeProvider) ListReleases(ctx context.Context, owner, repo string, _ int) ([]forge.Release, error) {
	rel, err := p.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	return []forge.Release{rel}, nil
}

func (p fakeProvider) DownloadReleaseAsset(context.Context, string, string, forge.ReleaseAsset) (io.ReadCloser, string, error) {
	return nil, "", errors.WithStack(forge.ErrNotSupported)
}

// releaseProvider reports tag as the latest release.
func releaseProvider(tag string) forge.Provider { return fakeProvider{tag: tag} }

// failingReleaseProvider fails every release query.
func failingReleaseProvider() forge.Provider {
	return fakeProvider{err: errors.New("boom")}
}
