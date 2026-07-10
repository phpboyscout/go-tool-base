package config

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// --- container.go typed accessors (GetFloat/GetTime/GetDuration) ---

func TestContainer_TypedAccessors(t *testing.T) {
	t.Parallel()

	c := NewReaderContainer(
		afero.NewMemMapFs(),
		WithLogger(logger.ToSlog(logger.NewNoop())),
		WithConfigFormat("yaml"),
		WithConfigReaders(strings.NewReader(
			"ratio: 1.5\n"+
				"when: 2021-01-02T03:04:05Z\n"+
				"timeout: 1m30s\n",
		)),
	)

	assert.InEpsilon(t, 1.5, c.GetFloat("ratio"), 1e-9)
	assert.Equal(t, 90*time.Second, c.GetDuration("timeout"))

	got := c.GetTime("when")
	assert.Equal(t, 2021, got.Year())
	assert.Equal(t, time.January, got.Month())
}

// --- config.go: NewContainerFromViper ---

func TestNewContainerFromViper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		log  logger.Logger
	}{
		{name: "nil logger falls back to noop", log: nil},
		{name: "explicit logger", log: logger.NewNoop()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v := viper.New()
			v.Set("foo.bar", "baz")

			c := NewContainerFromViper(logger.ToSlog(tt.log), v)
			require.NotNil(t, c)
			assert.Equal(t, "viper", c.ID)
			assert.NotNil(t, c.logger)
			assert.Equal(t, "baz", c.GetString("foo.bar"))
		})
	}
}

// --- config.go: loadFilesContainer error and merge branches ---

func TestLoadFilesContainer_NoConfigFiles(t *testing.T) {
	t.Parallel()

	o := applyOptions([]ContainerOption{WithLogger(logger.ToSlog(logger.NewNoop()))})

	c, err := loadFilesContainer(afero.NewMemMapFs(), o)
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "no config files specified")
}

func TestLoadFilesContainer_PrimaryMissingReturnsNilNil(t *testing.T) {
	t.Parallel()

	o := applyOptions([]ContainerOption{
		WithLogger(logger.ToSlog(logger.NewNoop())),
		WithConfigFiles("/does-not-exist.yaml"),
	})

	c, err := loadFilesContainer(afero.NewMemMapFs(), o)
	require.NoError(t, err)
	assert.Nil(t, c)
}

func TestLoadFilesContainer_ReadError(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/bad.yaml", []byte("key: [unterminated\n"), 0o644))

	o := applyOptions([]ContainerOption{
		WithLogger(logger.ToSlog(logger.NewNoop())),
		WithConfigFiles("/bad.yaml"),
	})

	c, err := loadFilesContainer(fs, o)
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadFilesContainer_MergesSecondaryAndSkipsMissing(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/a.yaml", []byte("a: 1\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/b.yaml", []byte("b: 2\n"), 0o644))

	o := applyOptions([]ContainerOption{
		WithLogger(logger.ToSlog(logger.NewNoop())),
		// /missing.yaml is silently skipped; /b.yaml is merged.
		WithConfigFiles("/a.yaml", "/missing.yaml", "/b.yaml"),
	})

	c, err := loadFilesContainer(fs, o)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, 1, c.GetInt("a"))
	assert.Equal(t, 2, c.GetInt("b"))
}

func TestLoadFilesContainer_MergeFailureLogsWarning(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/a.yaml", []byte("a: 1\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/b.yaml", []byte("b: [bad\n"), 0o644))

	o := applyOptions([]ContainerOption{
		WithLogger(logger.ToSlog(logger.NewNoop())),
		WithConfigFiles("/a.yaml", "/b.yaml"),
	})

	// A malformed secondary file is tolerated (warning only); the container
	// is still returned with the primary file's values.
	c, err := loadFilesContainer(fs, o)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, 1, c.GetInt("a"))
}

// --- container.go: handleReadFileError other-error branch ---

func TestHandleReadFileError_NonPathError(t *testing.T) {
	t.Parallel()

	c := &Container{logger: logger.ToSlog(logger.NewNoop())}

	// A non-PathError (e.g. a parse error) exercises the "other errors" branch.
	c.handleReadFileError(errFromString("some parse failure"))
	// nil is a no-op and must not panic.
	c.handleReadFileError(nil)
}

// --- container.go: Dump writes the JSON rendering ---

func TestContainer_DumpWritesJSON(t *testing.T) {
	t.Parallel()

	c := NewReaderContainer(
		afero.NewMemMapFs(),
		WithLogger(logger.ToSlog(logger.NewNoop())),
		WithConfigFormat("yaml"),
		WithConfigReaders(strings.NewReader("key: value\n")),
	)

	var buf bytes.Buffer
	c.Dump(&buf)
	assert.Contains(t, buf.String(), "\"key\":\"value\"")
}

// --- validate.go: type predicates ---

func TestTypeMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		value    any
		want     bool
	}{
		{"string ok", "string", "x", true},
		{"string mismatch", "string", 1, false},
		{"int ok", "int", 5, true},
		{"int8 ok", "int", int8(5), true},
		{"int16 ok", "int", int16(5), true},
		{"int32 ok", "int", int32(5), true},
		{"int64 ok", "int", int64(5), true},
		{"int mismatch", "int", "x", false},
		{"float64 ok", "float64", 1.5, true},
		{"float32 ok", "float64", float32(1.5), true},
		{"float mismatch", "float64", "x", false},
		{"bool ok", "bool", true, true},
		{"bool mismatch", "bool", 1, false},
		{"duration from Duration", "duration", time.Second, true},
		{"duration from string ok", "duration", "1m30s", true},
		{"duration from string bad", "duration", "nope", false},
		{"duration mismatch", "duration", 5, false},
		{"unknown type passes", "weird", struct{}{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, typeMatches(tt.expected, tt.value))
		})
	}
}

func TestIsZeroValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{"empty string", "", true},
		{"non-empty string", "x", false},
		{"zero int", 0, true},
		{"non-zero int", 1, false},
		{"zero int64", int64(0), true},
		{"non-zero int64", int64(2), false},
		{"zero float64", float64(0), true},
		{"non-zero float64", 1.5, false},
		{"false bool", false, true},
		{"true bool", true, false},
		{"other type never zero", []string{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isZeroValue(tt.value))
		})
	}
}

// --- validate.go: validateField type-mismatch and optional-nil paths ---

func TestValidateField_TypeMismatch(t *testing.T) {
	t.Parallel()

	result := &ValidationResult{}
	validateField("port", FieldSchema{Type: "int"}, "not-an-int", result)

	require.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0].Message, "expected type int")
}

func TestValidateField_NilOptionalSkips(t *testing.T) {
	t.Parallel()

	result := &ValidationResult{}
	validateField("opt", FieldSchema{Type: "string"}, nil, result)
	assert.True(t, result.Valid())
}

// --- schema.go: goTypeToSchemaType across kinds ---

func TestSchemaOf_AllScalarKinds(t *testing.T) {
	t.Parallel()

	type allKinds struct {
		S   string        `config:"s"`
		I   int           `config:"i"`
		I64 int64         `config:"i64"`
		F   float64       `config:"f"`
		B   bool          `config:"b"`
		D   time.Duration `config:"d"`
		// Unmapped kinds fall through to "string".
		Sl []string          `config:"sl"`
		M  map[string]string `config:"m"`
		U  uint              `config:"u"`
	}

	schema, err := SchemaOf[allKinds]()
	require.NoError(t, err)

	fields := schema.Fields()
	assert.Equal(t, "string", fields["s"].Type)
	assert.Equal(t, "int", fields["i"].Type)
	assert.Equal(t, "int", fields["i64"].Type)
	assert.Equal(t, "float64", fields["f"].Type)
	assert.Equal(t, "bool", fields["b"].Type)
	assert.Equal(t, "duration", fields["d"].Type)
	assert.Equal(t, "string", fields["sl"].Type)
	assert.Equal(t, "string", fields["m"].Type)
	assert.Equal(t, "string", fields["u"].Type)
}

// --- schema.go: recurseUntaggedStruct (named untagged + anonymous embedded) ---

type embeddedConfig struct {
	Token string `config:"token"`
}

func TestParseStructTags_UntaggedAndEmbedded(t *testing.T) {
	t.Parallel()

	type withNestedAndEmbedded struct {
		embeddedConfig          // anonymous: keys promoted into the prefix
		Server         struct { // named untagged: recursed under "server"
			Host string `config:"host"`
		}
		Skipped int `config:"-"` // explicitly skipped
	}

	schema, err := SchemaOf[withNestedAndEmbedded]()
	require.NoError(t, err)

	fields := schema.Fields()
	_, hasToken := fields["token"]
	_, hasHost := fields["server.host"]
	_, hasSkipped := fields["-"]

	assert.True(t, hasToken, "embedded struct field should be promoted")
	assert.True(t, hasHost, "named untagged struct should recurse under server.host")
	assert.False(t, hasSkipped, "config:\"-\" field should be skipped")
}

// --- validate_generic.go: ValidateStruct error and success paths ---

type genericConfig struct {
	Token string `config:"token" validate:"required"`
}

func TestValidateStruct_Success(t *testing.T) {
	t.Parallel()

	c := NewReaderContainer(
		afero.NewMemMapFs(),
		WithLogger(logger.ToSlog(logger.NewNoop())),
		WithConfigFormat("yaml"),
		WithConfigReaders(strings.NewReader("token: present\n")),
	)

	require.NoError(t, ValidateStruct[genericConfig](c))
}

func TestValidateStruct_ValidationFailure(t *testing.T) {
	t.Parallel()

	c := NewReaderContainer(
		afero.NewMemMapFs(),
		WithLogger(logger.ToSlog(logger.NewNoop())),
		WithConfigFormat("yaml"),
		WithConfigReaders(strings.NewReader("other: x\n")),
	)

	err := ValidateStruct[genericConfig](c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
}

// errFromString builds a plain non-PathError value for handleReadFileError.
func errFromString(s string) error {
	return &stringError{s}
}

type stringError struct{ msg string }

func (e *stringError) Error() string { return e.msg }
