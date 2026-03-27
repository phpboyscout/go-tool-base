package config

import (
	"maps"
	"reflect"
	"strings"

	"github.com/cockroachdb/errors"
)

// Schema defines the expected structure and constraints for configuration values.
type Schema struct {
	fields map[string]FieldSchema
	strict bool
}

// FieldSchema describes a single configuration field.
type FieldSchema struct {
	// Type is the expected Go type: "string", "int", "float64", "bool", "duration".
	Type string
	// Required indicates the field must be present and non-zero.
	Required bool
	// Description is used in validation error messages.
	Description string
	// Default is the default value for documentation and error hints only.
	// The validation layer does not inject defaults — use embedded assets for that.
	Default any
	// Enum restricts the field to a set of allowed values.
	Enum []any
	// Children defines nested fields for map/object types.
	Children map[string]FieldSchema
}

// SchemaOption configures schema construction.
type SchemaOption func(*schemaConfig)

type schemaConfig struct {
	fields map[string]FieldSchema
	strict bool
}

// WithStrictMode treats unknown keys as errors instead of warnings.
func WithStrictMode() SchemaOption {
	return func(c *schemaConfig) {
		c.strict = true
	}
}

// WithStructSchema derives a schema from a tagged Go struct.
// Supported tags: `config:"key" validate:"required" enum:"a,b,c" default:"value"`.
func WithStructSchema(v any) SchemaOption {
	return func(c *schemaConfig) {
		maps.Copy(c.fields, parseStructTags(reflect.TypeOf(v), ""))
	}
}

// NewSchema creates a Schema from the provided options.
func NewSchema(opts ...SchemaOption) (*Schema, error) {
	cfg := &schemaConfig{
		fields: make(map[string]FieldSchema),
	}

	for _, opt := range opts {
		opt(cfg)
	}

	if len(cfg.fields) == 0 {
		return nil, errors.New("schema has no fields defined")
	}

	return &Schema{
		fields: cfg.fields,
		strict: cfg.strict,
	}, nil
}

// Fields returns the schema field definitions.
func (s *Schema) Fields() map[string]FieldSchema {
	return s.fields
}

// parseStructTags walks a struct type and extracts schema fields from tags.
func parseStructTags(t reflect.Type, prefix string) map[string]FieldSchema {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	fields := make(map[string]FieldSchema)

	for _, f := range reflect.VisibleFields(t) {
		configKey := f.Tag.Get("config")
		if configKey == "" || configKey == "-" {
			recurseUntaggedStruct(f, prefix, fields)

			continue
		}

		if prefix != "" && !strings.Contains(configKey, ".") {
			configKey = prefix + "." + configKey
		}

		fields[configKey] = buildFieldSchema(f)
	}

	return fields
}

// recurseUntaggedStruct handles struct fields that lack a config tag by recursing into them.
func recurseUntaggedStruct(f reflect.StructField, prefix string, fields map[string]FieldSchema) {
	if f.Type.Kind() != reflect.Struct {
		return
	}

	if f.Anonymous {
		maps.Copy(fields, parseStructTags(f.Type, prefix))

		return
	}

	sub := strings.ToLower(f.Name)
	if prefix != "" {
		sub = prefix + "." + sub
	}

	maps.Copy(fields, parseStructTags(f.Type, sub))
}

// buildFieldSchema constructs a FieldSchema from struct field tags.
func buildFieldSchema(f reflect.StructField) FieldSchema {
	field := FieldSchema{
		Type:        goTypeToSchemaType(f.Type),
		Description: f.Tag.Get("description"),
	}

	if validate := f.Tag.Get("validate"); validate != "" {
		for v := range strings.SplitSeq(validate, ",") {
			if v == "required" {
				field.Required = true
			}
		}
	}

	if enumTag := f.Tag.Get("enum"); enumTag != "" {
		for v := range strings.SplitSeq(enumTag, ",") {
			field.Enum = append(field.Enum, v)
		}
	}

	if def := f.Tag.Get("default"); def != "" {
		field.Default = def
	}

	return field
}

func goTypeToSchemaType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if t.String() == "time.Duration" {
			return "duration"
		}

		return "int"
	case reflect.Float32, reflect.Float64:
		return "float64"
	case reflect.Bool:
		return "bool"
	case reflect.Invalid, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Array, reflect.Chan,
		reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice,
		reflect.Struct, reflect.UnsafePointer:
		return "string"
	}

	return "string"
}
