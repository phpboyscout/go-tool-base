package doctor

import (
	"context"
	"fmt"
	"strings"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// credentialResolutionCheck is the check name, used by every arm below.
//
//nolint:gosec // G101: this is the check's display name, not a credential
const credentialResolutionCheck = "Credential resolution"

// checkCredentialResolution reports, for every declared credential, which
// source is actually in effect and which lower-precedence copies are still
// present.
//
// Spec 0189 R1/R2. Before this, `doctor` answered "is there a literal
// credential in config" and nothing else, so two very different situations read
// identically: a literal that is the credential in use, and a literal sitting
// dead underneath a working environment reference. The first is an active
// exposure; the second is untidy. Resolution was reported only for forges
// (spec 0183), so an AI provider key got no such answer at all.
//
// Values never appear. Every line is a key name or a rung name, which is what
// makes this safe to paste into a support bundle.
func checkCredentialResolution(ctx context.Context, props *p.Props) CheckResult {
	if props == nil || props.Config == nil {
		return CheckResult{Name: credentialResolutionCheck, Status: CheckSkip, Message: "no configuration loaded"}
	}

	// Only the credentials of enabled features: a tool with no forge and no
	// ai has nothing to say about GitHub tokens or Anthropic keys (#55).
	results := credentialposture.ReportWhere(ctx, props.Config.View(), func(d credentialposture.Descriptor) bool {
		return descriptorApplies(props.GetFeatures(), d)
	})
	if len(results) == 0 {
		return CheckResult{Name: credentialResolutionCheck, Status: CheckSkip, Message: "no credentials declared"}
	}

	var (
		lines    []string
		broken   int
		shadowed int
		resolved int
	)

	for _, r := range results {
		switch {
		case r.Err != nil:
			broken++

			lines = append(lines, fmt.Sprintf("%s: configured but does not resolve (%v)", r.Posture.Label, r.Err))

		case r.Posture.Origin == credentialposture.OriginNone:
			// Not a failure: a tool may legitimately never use this provider.
			continue

		default:
			resolved++

			if len(r.Posture.Shadowed) > 0 {
				shadowed++
			}

			lines = append(lines, r.Posture.String())
		}
	}

	return resolutionResult(lines, resolved, shadowed, broken)
}

// resolutionResult turns the per-credential findings into one check result.
//
// A shadowed copy is a warning rather than a failure: the credential in use is
// the safer one, and the stale copy is a cleanup rather than an outage. It is
// still worth saying, because it is the difference between dead configuration
// and a secret nobody realises is still on disk.
func resolutionResult(lines []string, resolved, shadowed, broken int) CheckResult {
	if len(lines) == 0 {
		return CheckResult{Name: credentialResolutionCheck, Status: CheckSkip, Message: "no credentials configured"}
	}

	details := strings.Join(lines, "\n       ")

	switch {
	case broken > 0:
		return CheckResult{
			Name:    credentialResolutionCheck,
			Status:  CheckWarn,
			Message: fmt.Sprintf("%d credential(s) configured but not resolving", broken),
			Details: details,
		}

	case shadowed > 0:
		return CheckResult{
			Name:    credentialResolutionCheck,
			Status:  CheckWarn,
			Message: fmt.Sprintf("%d of %d credential(s) have shadowed copies still in config", shadowed, resolved),
			Details: details + "\n       A shadowed copy is not in use, but it is still a secret on disk. Remove it with `config unset <key>`.",
		}

	default:
		return CheckResult{
			Name:    credentialResolutionCheck,
			Status:  CheckPass,
			Message: fmt.Sprintf("%d credential(s) resolve", resolved),
			Details: details,
		}
	}
}

// descriptorApplies is the doctor's predicate for a credential: its feature
// enabled (#55) and, when it names the providers that consume it, at least one
// of them registered in this binary (spec 0196 D12).
func descriptorApplies(set features.Set, d credentialposture.Descriptor) bool {
	if d.Feature != "" && !set.Enabled(p.FeatureID(d.Feature)) {
		return false
	}

	if len(d.Providers) == 0 {
		return true
	}

	for _, provider := range d.Providers {
		if gochat.ProviderRegistered(gochat.Provider(provider)) {
			return true
		}
	}

	return false
}
