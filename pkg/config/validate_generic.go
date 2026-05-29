package config

import (
	"reflect"
	"sync"

	"github.com/cockroachdb/errors"
)

// schemaCache memoises schemas derived from a type's struct tags. A schema for a
// given type built without extra options is immutable, so it is safe to reuse
// across calls and goroutines.
var schemaCache sync.Map // reflect.Type -> *Schema

// SchemaOf returns a Schema derived from the struct tags of T. For the
// option-free call the result is cached per type, so repeated calls for the same
// T do not re-reflect. When opts are supplied (for example WithStrictMode) the
// schema is built fresh and not cached, since options can change the result.
//
// T must be a struct; see WithStructSchema for the supported tags.
func SchemaOf[T any](opts ...SchemaOption) (*Schema, error) {
	if len(opts) == 0 {
		t := reflect.TypeFor[T]()
		if cached, ok := schemaCache.Load(t); ok {
			return cached.(*Schema), nil
		}

		schema, err := NewSchema(WithStructSchema(*new(T)))
		if err != nil {
			return nil, err
		}

		schemaCache.Store(t, schema)

		return schema, nil
	}

	return NewSchema(append([]SchemaOption{WithStructSchema(*new(T))}, opts...)...)
}

// ValidateStruct validates cfg against the schema derived from T, returning a
// formatted error if any rule fails or nil if the configuration is valid.
//
// It takes the Containable interface, so callers do not need to type-assert
// Props.Config down to the concrete *Container. It is the recommended way to
// validate a command or feature's config slice:
//
//	if err := config.ValidateStruct[MyConfig](props.Config); err != nil {
//		return err
//	}
//
// Schema options such as WithStrictMode may be passed through.
func ValidateStruct[T any](cfg Containable, opts ...SchemaOption) error {
	schema, err := SchemaOf[T](opts...)
	if err != nil {
		return err
	}

	if result := cfg.Validate(schema); !result.Valid() {
		return errors.New(result.Error())
	}

	return nil
}
