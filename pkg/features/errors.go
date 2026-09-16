package features

import "gitlab.com/phpboyscout/go/errors"

var (
	// ErrInvalidDescriptor reports a descriptor with no ID or no kind.
	ErrInvalidDescriptor = errors.NewSentinel("gtb.features.invalid_descriptor", "features: descriptor is incomplete")
	// ErrDuplicateFeature reports a second declaration for one ID.
	ErrDuplicateFeature = errors.NewSentinel("gtb.features.duplicate_feature", "features: feature is already declared")
	// ErrUnknownFeature reports an ID the snapshot does not hold.
	ErrUnknownFeature = errors.NewSentinel("gtb.features.unknown_feature", "features: unknown feature")
	// ErrContributionType reports a contribution that is not the type the
	// reader asked for.
	ErrContributionType = errors.NewSentinel("gtb.features.contribution_type", "features: contribution has an unexpected type")
)
