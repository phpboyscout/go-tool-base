package doctor

import (
	"context"
	"fmt"

	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/signing/verify"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// checkKeyRotation reports whether a tool that can follow a release-key
// rotation will. Turned off, a key the maintainers revoke stays trusted, so it
// is a warning while it lasts (spec 0101 D18).
func checkKeyRotation(ctx context.Context, props *p.Props) CheckResult {
	const name = "Key rotation"

	if props == nil || len(props.Tool.Signing.RotationAuthorityKeys) == 0 {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no rotation authority keys"}
	}

	if !props.GetFeatures().Enabled(p.UpdateCmd) {
		return CheckResult{Name: name, Status: CheckSkip, Message: "self-update disabled"}
	}

	return rotationPosture(ctx, props, name)
}

// rotationPosture is the state of a tool that rotation applies to.
func rotationPosture(ctx context.Context, props *p.Props, name string) CheckResult {
	if _, err := verify.NewRotationAuthority(props.Tool.Signing.RotationAuthorityKeys...); err != nil {
		return CheckResult{Name: name, Status: CheckFail, Message: "an authority key does not parse, so update refuses to run: " + err.Error()}
	}

	if len(props.Tool.Signing.EmbeddedKeys) == 0 && (props.Config == nil || props.Config.View().GetString(setup.ConfigKeyUpdateExternalKeyEmail) == "") {
		return CheckResult{Name: name, Status: CheckWarn, Message: "authority keys are set but no release key is, so there is nothing to rotate"}
	}

	if props.Config != nil && !setup.RotationEnabled(props.Config.View()) {
		return CheckResult{
			Name:    name,
			Status:  CheckWarn,
			Message: setup.ConfigKeyUpdateRotationEnabled + " is false, so a release key the maintainers revoke stays trusted",
		}
	}

	if props.FS != nil {
		if err := setup.CheckRotationRecord(ctx, props.FS, props.Tool.Name); err != nil {
			return CheckResult{Name: name, Status: CheckFail, Message: "update refuses to run: " + err.Error(), Details: errors.FlattenHints(err)}
		}
	}

	return CheckResult{
		Name:    name,
		Status:  CheckPass,
		Message: fmt.Sprintf("on, %d authority key(s)", len(props.Tool.Signing.RotationAuthorityKeys)),
	}
}
