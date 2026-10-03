// Package gcpsource resolves the client options a GCP source kind's client is
// made from: gcpclient's ambient chain (Application Default Credentials: the
// metadata server, workload identity, GOOGLE_APPLICATION_CREDENTIALS, gcloud's
// login), with the slot's endpoint applied (spec 0204 D18).
package gcpsource

import (
	"context"

	"google.golang.org/api/option"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/gcpclient"
)

// ErrNoProject is a GCP slot with no project. Application Default
// Credentials name a principal, not the project whose data to read, and
// guessing one could read another project's.
var ErrNoProject = errors.NewSentinel("gtb.config.sources.gcp.no_project", "gcp config source has no project")

// Scopes the GCP kinds ask for. gcpclient refuses to guess one, and IAM, not
// the scope, is what narrows access.
const (
	// ScopeCloudPlatform is what Secret Manager and Parameter Manager document.
	ScopeCloudPlatform = "https://www.googleapis.com/auth/cloud-platform"
	// ScopeStorage reads, and writes when a slot is declared writable.
	ScopeStorage = "https://www.googleapis.com/auth/devstorage.read_write"
)

// ClientOptions resolves the client options for a slot, for the service the
// scope names. An endpoint, for an emulator or a regional service, is
// applied over them.
func ClientOptions(ctx context.Context, settings config.Reader, scope string) ([]option.ClientOption, error) {
	opts, err := gcpclient.Ambient(gcpclient.WithScopes(scope)).GCPClientOptions(ctx)
	if err != nil {
		return nil, err
	}

	if endpoint := settings.GetString("endpoint"); endpoint != "" {
		opts = append(opts, option.WithEndpoint(endpoint))
	}

	return opts, nil
}

// Project reads a slot's project, which every GCP kind but gcp-gcs needs.
func Project(settings config.Reader) (string, error) {
	project := settings.GetString("project")
	if project == "" {
		return "", errors.WithHint(ErrNoProject, "set config.sources.<name>.project")
	}

	return project, nil
}
