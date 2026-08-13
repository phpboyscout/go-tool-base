package templates

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandInitializer_ConfigureMatchesInterface pins the generated
// Initialiser to setup.Initialiser's actual Configure signature.
//
// The interface gained a leading context.Context when credential stages were
// scoped per operation, but this template was not updated with it — so every
// project scaffolded with --with-initializer since then has emitted an init.go
// that does not compile:
//
//	*FooInitialiser does not implement setup.Initialiser
//	  have Configure(*props.Props, setup.Editor) error
//	  want Configure(context.Context, *props.Props, setup.Editor) error
//
// A golden-string test is the right shape here: the generator emits source, and
// the defect is a signature mismatch invisible to the generator's own tests
// because nothing compiles the output.
func TestCommandInitializer_ConfigureMatchesInterface(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, CommandInitializer(CommandData{
		Package:    "foo",
		Name:       "foo",
		PascalName: "Foo",
	}).Render(&buf))

	got := buf.String()

	assert.Contains(t, got, "func (i *FooInitialiser) Configure(ctx context.Context, p *props.Props, cfg setup.Editor) error",
		"Configure must match setup.Initialiser, which takes a leading context.Context")
	assert.Contains(t, got, `"context"`, "the generated file must import context")
	assert.Contains(t, got, "InitFoo(ctx, p, cfg)",
		"the context must reach the user's Init stub, not be discarded")
}

// TestCommandInitializer_InitStubTakesContext pins the user-editable Init<Name>
// stub to the same shape, so the generated call site and the stub agree.
//
// PreRun<Name> already takes a context (see stubs.go); Init<Name> matching it is
// what lets a scaffolded initialiser honour cancellation during setup rather
// than discarding the deadline the interface exists to carry.
func TestCommandInitializer_InitStubTakesContext(t *testing.T) {
	t.Parallel()

	data := CommandData{
		Package:         "foo",
		Name:            "foo",
		PascalName:      "Foo",
		WithInitializer: true,
	}

	body := CommandExecution(data)

	assert.Contains(t, body, "func InitFoo(ctx context.Context, p *props.Props, cfg setup.Editor) error",
		"the Init stub must accept the context the Configure call passes it")
}
