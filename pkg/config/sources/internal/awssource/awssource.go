// Package awssource builds the AWS config a source kind's client is made
// from: awsclient's ambient chain (SSO, profiles, IMDS, IRSA, the standard
// environment variables), with the slot's region, profile and endpoint
// applied (spec 0204 D18).
package awssource

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"

	"gitlab.com/phpboyscout/go/awsclient"
	"gitlab.com/phpboyscout/go/config"
)

// Config resolves the AWS config for a slot. An endpoint, for LocalStack or
// MinIO, applies to every service the client reaches.
func Config(ctx context.Context, settings config.Reader) (aws.Config, error) {
	var opts []awsclient.Option

	if region := settings.GetString("region"); region != "" {
		opts = append(opts, awsclient.WithRegion(region))
	}

	if profile := settings.GetString("profile"); profile != "" {
		opts = append(opts, awsclient.WithLoadOptions(awscfg.WithSharedConfigProfile(profile)))
	}

	cfg, err := awsclient.Ambient(opts...).AWSConfig(ctx)
	if err != nil {
		return aws.Config{}, err
	}

	if endpoint := settings.GetString("endpoint"); endpoint != "" {
		cfg.BaseEndpoint = aws.String(endpoint)
	}

	return cfg, nil
}
