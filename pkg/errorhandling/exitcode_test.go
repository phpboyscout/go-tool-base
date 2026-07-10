package errorhandling_test

import (
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestWithExitCode_NilErrorReturnsNil(t *testing.T) {
	t.Parallel()

	assert.NoError(t, errorhandling.WithExitCode(nil, 130))
}

func TestExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "nil error has exit code zero",
			err:  nil,
			want: 0,
		},
		{
			name: "plain error defaults to one",
			err:  errors.New("boom"),
			want: 1,
		},
		{
			name: "attached code is returned",
			err:  errorhandling.WithExitCode(errors.New("interrupted"), 130),
			want: 130,
		},
		{
			name: "code survives further wrapping",
			err:  errors.Wrap(errorhandling.WithExitCode(errors.New("interrupted"), 143), "outer"),
			want: 143,
		},
		{
			name: "outermost attachment wins when re-attached",
			err:  errorhandling.WithExitCode(errorhandling.WithExitCode(errors.New("x"), 143), 130),
			want: 130,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, errorhandling.ExitCode(tt.err))
		})
	}
}

func TestWithExitCode_PreservesMessageAndIdentity(t *testing.T) {
	t.Parallel()

	base := errors.New("interrupted by signal")
	err := errorhandling.WithExitCode(base, 130)

	require.EqualError(t, err, "interrupted by signal")
	assert.ErrorIs(t, err, base, "WithExitCode must be transparent to errors.Is")
}

// TestCheck_FatalUsesAttachedExitCode proves the ErrorHandler's exit path
// honours an exit code attached via WithExitCode (spec D2: signal-terminated
// runs exit 128+signum through the ErrorHandler, not a parallel exit path).
func TestCheck_FatalUsesAttachedExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "plain fatal error exits one",
			err:  errors.New("boom"),
			want: 1,
		},
		{
			name: "SIGINT-style error exits 130",
			err:  errorhandling.WithExitCode(errors.New("interrupted"), 130),
			want: 130,
		},
		{
			name: "SIGTERM-style error exits 143",
			err:  errorhandling.WithExitCode(errors.New("terminated"), 143),
			want: 143,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exitCode := -1
			h := errorhandling.New(logger.ToSlog(logger.NewNoop()), nil,
				errorhandling.WithExitFunc(func(code int) { exitCode = code }))

			h.Check(tt.err, "", errorhandling.LevelFatal)

			assert.Equal(t, tt.want, exitCode)
		})
	}
}

// TestCheck_FatalQuietLogsDebugButStillExits proves the LevelFatalQuiet path
// honours the attached exit code (128+signum) exactly like LevelFatal, while
// demoting the notice from error to debug — an interrupt is a user choice, not
// a failure. The message is still emitted (at debug) so `--debug` surfaces it.
func TestCheck_FatalQuietLogsDebugButStillExits(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	exitCode := -1
	h := errorhandling.New(logger.ToSlog(buf), nil,
		errorhandling.WithExitFunc(func(code int) { exitCode = code }))

	err := errorhandling.WithExitCode(errors.New("interrupted by signal: interrupt"), 130)
	h.Check(err, "", errorhandling.LevelFatalQuiet)

	assert.Equal(t, 130, exitCode, "LevelFatalQuiet must still exit with the attached code")

	entries := buf.Entries()
	require.Len(t, entries, 1, "the interrupt notice must be emitted exactly once")
	assert.Equal(t, logger.DebugLevel, entries[0].Level,
		"the interrupt notice must be logged at debug, not error")
	assert.Equal(t, "interrupted by signal: interrupt", entries[0].Message)

	for _, e := range entries {
		assert.NotEqual(t, logger.ErrorLevel, e.Level,
			"LevelFatalQuiet must never log at error")
	}
}
