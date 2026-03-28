package setup

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestWithTiming(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logger.NewCharm(&buf, logger.WithLevel(logger.DebugLevel))

	mw := WithTiming(log)

	t.Run("Success", func(t *testing.T) {
		buf.Reset()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		})

		err := handler(&cobra.Command{Use: "test-cmd"}, nil)
		require.NoError(t, err)

		out := buf.String()
		assert.Contains(t, out, "command completed")
		assert.Contains(t, out, "command=test-cmd")
		assert.Contains(t, out, "duration=")
		assert.NotContains(t, out, "error=")
	})

	t.Run("Error", func(t *testing.T) {
		buf.Reset()
		expectedErr := fmt.Errorf("handler failed")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return expectedErr
		})

		err := handler(&cobra.Command{Use: "test-cmd"}, nil)
		require.ErrorIs(t, err, expectedErr)

		out := buf.String()
		assert.Contains(t, out, "command completed")
		assert.Contains(t, out, "error=\"handler failed\"")
	})
}

func TestWithRecovery(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logger.NewCharm(&buf, logger.WithLevel(logger.DebugLevel))

	mw := WithRecovery(log)

	t.Run("NoPanic", func(t *testing.T) {
		buf.Reset()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})

	t.Run("Panic", func(t *testing.T) {
		buf.Reset()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			panic("something went terribly wrong")
		})

		err := handler(&cobra.Command{Use: "test-cmd"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic in command \"test-cmd\": something went terribly wrong")

		out := buf.String()
		assert.Contains(t, out, "panic recovered in command")
		assert.Contains(t, out, "command=test-cmd")
		assert.Contains(t, out, "panic=\"something went terribly wrong\"")
		assert.Contains(t, out, "stack=")
	})
}

func TestWithAuthCheck(t *testing.T) {
	// Not parallel because it modifies global viper state

	t.Run("AllKeysPresent", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)

		viper.Set("test.key1", "value1")
		viper.Set("test.key2", "value2")

		mw := WithAuthCheck("test.key1", "test.key2")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		assert.NoError(t, err)
	})

	t.Run("MissingKey", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)

		viper.Set("test.key1", "value1")

		mw := WithAuthCheck("test.key1", "test.missing")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required configuration \"test.missing\" is not set")
	})

	t.Run("EmptyKey", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)

		viper.Set("test.key1", "")

		mw := WithAuthCheck("test.key1")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "required configuration \"test.key1\" is not set")
	})

	t.Run("NoKeys", func(t *testing.T) {
		viper.Reset()
		t.Cleanup(viper.Reset)

		mw := WithAuthCheck()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		assert.NoError(t, err)
	})
}
