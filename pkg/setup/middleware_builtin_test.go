package setup

import (
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

	log := logger.NewBuffer()

	mw := WithTiming(log)

	t.Run("Success", func(t *testing.T) {
		log.Reset()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			time.Sleep(10 * time.Millisecond)
			return nil
		})

		err := handler(&cobra.Command{Use: "test-cmd"}, nil)
		require.NoError(t, err)

		entries := log.Entries()
		require.Len(t, entries, 1)
		assert.Equal(t, logger.DebugLevel, entries[0].Level)
		assert.Equal(t, "command completed", entries[0].Message)
		assert.Contains(t, entries[0].Keyvals, "command")
		assert.Contains(t, entries[0].Keyvals, "test-cmd")
		assert.Contains(t, entries[0].Keyvals, "duration")
		// error key should not be present on success
		assert.NotContains(t, entries[0].Keyvals, "error")
	})

	t.Run("Error", func(t *testing.T) {
		log.Reset()
		expectedErr := fmt.Errorf("handler failed")
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return expectedErr
		})

		err := handler(&cobra.Command{Use: "test-cmd"}, nil)
		require.ErrorIs(t, err, expectedErr)

		entries := log.Entries()
		require.Len(t, entries, 1)
		assert.Equal(t, "command completed", entries[0].Message)
		assert.Contains(t, entries[0].Keyvals, "error")
		assert.Contains(t, entries[0].Keyvals, "handler failed")
	})
}

func TestWithRecovery(t *testing.T) {
	t.Parallel()

	log := logger.NewBuffer()

	mw := WithRecovery(log)

	t.Run("NoPanic", func(t *testing.T) {
		log.Reset()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			return nil
		})

		err := handler(&cobra.Command{}, nil)
		require.NoError(t, err)
		assert.Equal(t, 0, log.Len())
	})

	t.Run("Panic", func(t *testing.T) {
		log.Reset()
		handler := mw(func(cmd *cobra.Command, args []string) error {
			panic("something went terribly wrong")
		})

		err := handler(&cobra.Command{Use: "test-cmd"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panic in command \"test-cmd\": something went terribly wrong")

		entries := log.Entries()
		require.Len(t, entries, 1)
		assert.Equal(t, logger.ErrorLevel, entries[0].Level)
		assert.Equal(t, "panic recovered in command", entries[0].Message)
		assert.Contains(t, entries[0].Keyvals, "command")
		assert.Contains(t, entries[0].Keyvals, "test-cmd")
		assert.Contains(t, entries[0].Keyvals, "panic")
		assert.Contains(t, entries[0].Keyvals, "something went terribly wrong")
		assert.Contains(t, entries[0].Keyvals, "stack")
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
