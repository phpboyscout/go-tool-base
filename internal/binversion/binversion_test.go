package binversion_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/internal/binversion"
	"gitlab.com/phpboyscout/go-tool-base/internal/binversion/binversiontest"
)

// Spec 0220 D1, D2.
func TestRead(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		binary []byte
		want   string
	}{
		"tagged":       {binversiontest.Binary(t, "v1.2.3", binversiontest.Tagged), "v1.2.3"},
		"prerelease":   {binversiontest.Binary(t, "v1.2.3-rc.1", binversiontest.Tagged), "v1.2.3-rc.1"},
		"dirty":        {binversiontest.Binary(t, "v1.2.3", binversiontest.Dirty), "v1.2.3+dirty"},
		"pseudo":       {binversiontest.Binary(t, "", binversiontest.Untagged), ""},
		"devel":        {binversiontest.Binary(t, "v1.2.3", binversiontest.NoVCS), ""},
		"not a binary": {[]byte("#!/bin/sh\n"), ""},
		"empty":        {nil, ""},
	} {
		assert.Equal(t, tt.want, binversion.Read(bytes.NewReader(tt.binary)), name)
	}
}

func TestSame(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		v, w string
		want bool
	}{
		{"v1.2.3", "v1.2.3", true},
		{"1.2.3", "v1.2.3", true},
		{"v1.2.3+dirty", "v1.2.3", true},
		{"v1.2.3", "v1.2.4", false},
		{"v1.2.3", "v1.2.30", false},
		{"v1.2.30", "v1.2.3", false},
		{"v1.2.3", "v1.2.3-rc.1", false},
		{"v1.2.3-rc.1", "v1.2.3", false},
		{"v1.2.3-rc.1", "v1.2.3-rc.2", false},
		{"", "", false},
		{"nightly", "nightly", false},
		{"v1.2", "v1.2.0", false},
		{"v1.2", "v1.2", false},
	} {
		assert.Equal(t, tt.want, binversion.Same(tt.v, tt.w), "%q %q", tt.v, tt.w)
	}
}

func TestIsVersion(t *testing.T) {
	t.Parallel()

	for _, v := range []string{"v1.2.3", "1.2.3", "v1.2.3-rc.1", "v1.2.3+dirty", "v1.2.3-rc.1+dirty"} {
		assert.True(t, binversion.IsVersion(v), v)
	}

	for _, v := range []string{
		"", "v", "nightly", "2026-11-24T06:27:32Z", "20261124", "1", "v1", "v1.2", "v1.2.3.4", "v01.2.3", "V1.2.3",
		"vv1.2.3", "v1.2.3 ", " v1.2.3", "cli/v1.2.3", "tool-v1.2.3",
	} {
		assert.False(t, binversion.IsVersion(v), v)
	}
}
