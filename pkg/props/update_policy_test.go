package props_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestUpdatePolicy_Normalize(t *testing.T) {
	t.Parallel()

	cases := map[string]props.UpdatePolicy{
		"":         props.UpdatePolicyDisabled, // zero value / framework default
		"disabled": props.UpdatePolicyDisabled,
		"prompt":   props.UpdatePolicyPrompt,
		"enabled":  props.UpdatePolicyEnabled,
		"  Prompt": props.UpdatePolicyPrompt, // trimmed + lowercased
		"ENABLED":  props.UpdatePolicyEnabled,
	}

	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, props.UpdatePolicy(in).Normalize())
		})
	}
}

func TestUpdatePolicy_Valid(t *testing.T) {
	t.Parallel()

	assert.True(t, props.UpdatePolicyDisabled.Valid())
	assert.True(t, props.UpdatePolicyPrompt.Valid())
	assert.True(t, props.UpdatePolicyEnabled.Valid())
	assert.False(t, props.UpdatePolicy("nonsense").Valid())
	// Empty is NOT a valid stored value, but Normalize() maps it to disabled.
	assert.False(t, props.UpdatePolicy("").Valid())
}

func TestResolveUpdatePolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		toolDefault props.UpdatePolicy
		configValue string
		want        props.UpdatePolicy
	}{
		{"both empty -> framework default disabled", "", "", props.UpdatePolicyDisabled},
		{"tool baseline prompt, no config", props.UpdatePolicyPrompt, "", props.UpdatePolicyPrompt},
		{"config overrides tool baseline", props.UpdatePolicyPrompt, "enabled", props.UpdatePolicyEnabled},
		{"config wins over disabled baseline", props.UpdatePolicyDisabled, "prompt", props.UpdatePolicyPrompt},
		{"invalid config falls back to baseline", props.UpdatePolicyPrompt, "garbage", props.UpdatePolicyPrompt},
		{"invalid config + empty baseline -> disabled", "", "garbage", props.UpdatePolicyDisabled},
		{"config case-insensitive", props.UpdatePolicyDisabled, "ENABLED", props.UpdatePolicyEnabled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, props.ResolveUpdatePolicy(tc.toolDefault, tc.configValue))
		})
	}
}
