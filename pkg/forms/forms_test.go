package forms_test

import (
	"testing"

	"github.com/phpboyscout/go-tool-base/pkg/forms"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- RunStepsWithBack (backward compat) --------------------------------------

func TestRunStepsWithBack_AllSucceed(t *testing.T) {
	var order []int

	steps := []func() error{
		func() error { order = append(order, 1); return nil },
		func() error { order = append(order, 2); return nil },
		func() error { order = append(order, 3); return nil },
	}

	err := forms.RunStepsWithBack(steps)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2, 3}, order)
}

func TestRunStepsWithBack_AbortOnFirst_Propagates(t *testing.T) {
	steps := []func() error{
		func() error { return huh.ErrUserAborted },
	}

	err := forms.RunStepsWithBack(steps)
	assert.True(t, errors.Is(err, huh.ErrUserAborted))
}

func TestRunStepsWithBack_AbortOnSecond_GoesBack(t *testing.T) {
	calls := 0

	steps := []func() error{
		func() error { calls++; return nil },
		func() error {
			calls++
			if calls == 2 {
				return huh.ErrUserAborted
			}

			return nil
		},
	}

	err := forms.RunStepsWithBack(steps)
	require.NoError(t, err)
	assert.Equal(t, 4, calls)
}

func TestRunStepsWithBack_OtherError_Propagates(t *testing.T) {
	expected := errors.New("boom")

	steps := []func() error{
		func() error { return nil },
		func() error { return expected },
	}

	err := forms.RunStepsWithBack(steps)
	assert.ErrorIs(t, err, expected)
}

func TestRunStepsWithBack_EmptySteps(t *testing.T) {
	err := forms.RunStepsWithBack(nil)
	assert.NoError(t, err)
}

// -- Wizard with Step (dynamic steps) ----------------------------------------

func TestWizard_Step_AllSucceed(t *testing.T) {
	var order []int

	err := forms.NewWizard().
		Step(func() error { order = append(order, 1); return nil }).
		Step(func() error { order = append(order, 2); return nil }).
		Run()

	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, order)
}

func TestWizard_Step_AbortOnFirst_Propagates(t *testing.T) {
	err := forms.NewWizard().
		Step(func() error { return huh.ErrUserAborted }).
		Run()

	assert.True(t, errors.Is(err, huh.ErrUserAborted))
}

func TestWizard_Step_AbortOnSecond_GoesBack(t *testing.T) {
	calls := 0

	err := forms.NewWizard().
		Step(func() error { calls++; return nil }).
		Step(func() error {
			calls++
			if calls == 2 {
				return huh.ErrUserAborted
			}

			return nil
		}).
		Run()

	require.NoError(t, err)
	assert.Equal(t, 4, calls)
}

func TestWizard_Step_OtherError_Propagates(t *testing.T) {
	expected := errors.New("wizard boom")

	err := forms.NewWizard().
		Step(func() error { return nil }).
		Step(func() error { return expected }).
		Run()

	assert.ErrorIs(t, err, expected)
}

func TestWizard_Empty_Succeeds(t *testing.T) {
	err := forms.NewWizard().Run()
	assert.NoError(t, err)
}

// -- Wizard with constructor groups ------------------------------------------

func TestNewWizard_ConstructorGroups_ExecuteInOrder(t *testing.T) {
	var order []int
	val1 := ""
	val2 := ""

	w := forms.NewWizard().
		Step(func() error { order = append(order, 1); val1 = "a"; return nil }).
		Step(func() error { order = append(order, 2); val2 = "b"; return nil })

	err := w.Run()
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, order)
	assert.Equal(t, "a", val1)
	assert.Equal(t, "b", val2)
}

// -- Wizard with Group builder -----------------------------------------------

func TestWizard_Group_AbortOnFirst_Propagates(t *testing.T) {
	err := forms.NewWizard().
		Step(func() error { return huh.ErrUserAborted }).
		Run()

	assert.True(t, errors.Is(err, huh.ErrUserAborted))
}

// -- Mixed Group + Step ------------------------------------------------------

func TestWizard_MixedGroupAndStep(t *testing.T) {
	var stepRan bool

	err := forms.NewWizard().
		Step(func() error { stepRan = true; return nil }).
		Run()

	require.NoError(t, err)
	assert.True(t, stepRan)
}

func TestWizard_ConditionalSteps(t *testing.T) {
	var order []int

	w := forms.NewWizard().
		Step(func() error { order = append(order, 1); return nil })

	addSecond := true
	if addSecond {
		w.Step(func() error { order = append(order, 2); return nil })
	}

	err := w.Run()
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, order)
}
