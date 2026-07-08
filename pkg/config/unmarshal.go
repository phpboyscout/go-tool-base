package config

import (
	"reflect"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

// Section is the result of unmarshalling an optional configuration section.
type Section[T any] struct {
	Value  T
	Exists bool
}

// Unmarshal decodes the full resolved configuration into target.
func (c *Container) Unmarshal(target any) error {
	return c.UnmarshalKey("", target)
}

// UnmarshalKey decodes the resolved configuration section at key into target.
// It overlays field values through Container's typed accessors after Viper's
// decode step so prefixed environment variables and sub-container qualification
// are honoured consistently with GetString/GetBool/etc.
func (c *Container) UnmarshalKey(key string, target any) error {
	if target == nil {
		return errors.New("cannot unmarshal config into nil target")
	}

	resolver := c.resolverViper()
	if key == "" {
		if err := resolver.Unmarshal(target); err != nil {
			return errors.Wrap(err, "failed to unmarshal config")
		}
	} else if err := resolver.UnmarshalKey(c.qualifyKey(key), target); err != nil {
		return errors.Wrapf(err, "failed to unmarshal config key %q", key)
	}

	if err := overlayResolvedFields(c, key, target); err != nil {
		return errors.Wrapf(err, "failed to overlay resolved config key %q", key)
	}

	return nil
}

// SectionExists reports whether key is present in the resolved configuration
// as either a direct value or structural subtree.
func (c *Container) SectionExists(key string) bool {
	if key == "" {
		return true
	}

	return c.IsSet(key) || c.Sub(key) != nil
}

// UnmarshalSection decodes key into T and reports whether the section exists.
func UnmarshalSection[T any](cfg Containable, key string) (Section[T], error) {
	var out T

	if cfg == nil {
		return Section[T]{}, nil
	}

	exists := cfg.SectionExists(key) || targetHasResolvedFields(cfg, key, reflect.TypeOf(out))
	if !exists {
		return Section[T]{}, nil
	}

	if err := cfg.UnmarshalKey(key, &out); err != nil {
		return Section[T]{}, err
	}

	return Section[T]{Value: out, Exists: true}, nil
}

// MustUnmarshalSection decodes key into T and panics if decoding fails.
func MustUnmarshalSection[T any](cfg Containable, key string) Section[T] {
	section, err := UnmarshalSection[T](cfg, key)
	if err != nil {
		panic(err)
	}

	return section
}

func targetHasResolvedFields(cfg Containable, prefix string, typ reflect.Type) bool {
	if typ == nil {
		return false
	}

	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	if typ.Kind() != reflect.Struct {
		return cfg.IsSet(prefix)
	}

	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}

		name, inline, skip := configFieldName(field)
		if skip {
			continue
		}

		fieldKey := prefix
		if !inline {
			fieldKey = joinConfigPath(prefix, name)
		}

		if cfg.IsSet(fieldKey) || targetHasResolvedFields(cfg, fieldKey, field.Type) {
			return true
		}
	}

	return false
}

func overlayResolvedFields(cfg Containable, prefix string, target any) error {
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return errors.New("config unmarshal target must be a non-nil pointer")
	}

	return overlayValue(cfg, prefix, value.Elem())
}

func overlayValue(cfg Containable, prefix string, value reflect.Value) error {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}

		value = value.Elem()
	}

	if value.Kind() != reflect.Struct {
		return nil
	}

	valueType := value.Type()
	for i := range value.NumField() {
		if err := overlayField(cfg, prefix, value.Field(i), valueType.Field(i)); err != nil {
			return err
		}
	}

	return nil
}

func overlayField(cfg Containable, prefix string, fieldValue reflect.Value, fieldType reflect.StructField) error {
	if fieldType.PkgPath != "" {
		return nil
	}

	name, inline, skip := configFieldName(fieldType)
	if skip {
		return nil
	}

	fieldKey := prefix
	if !inline {
		fieldKey = joinConfigPath(prefix, name)
	}

	if isStructLike(fieldValue) && fieldValue.Type() != reflect.TypeOf(time.Duration(0)) {
		return overlayValue(cfg, fieldKey, fieldValue)
	}

	if !cfg.IsSet(fieldKey) || !fieldValue.CanSet() {
		return nil
	}

	setResolvedValue(cfg, fieldKey, fieldValue)

	return nil
}

func setResolvedValue(cfg Containable, key string, value reflect.Value) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			value.Set(reflect.New(value.Type().Elem()))
		}

		value = value.Elem()
	}

	durationType := reflect.TypeOf(time.Duration(0))
	if value.Type() == durationType {
		value.SetInt(int64(cfg.GetDuration(key)))

		return
	}

	switch value.Kind() {
	case reflect.String:
		value.SetString(cfg.GetString(key))
	case reflect.Bool:
		value.SetBool(cfg.GetBool(key))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		setResolvedInt(cfg, key, value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		setResolvedUint(cfg, key, value)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(cfg.GetFloat(key))
	default:
		return
	}
}

func setResolvedInt(cfg Containable, key string, value reflect.Value) {
	value.SetInt(int64(cfg.GetInt(key)))
}

func setResolvedUint(cfg Containable, key string, value reflect.Value) {
	raw := cfg.GetInt(key)
	if raw >= 0 {
		value.SetUint(uint64(raw))
	}
}

func configFieldName(field reflect.StructField) (name string, inline bool, skip bool) {
	tag := field.Tag.Get("mapstructure")
	if tag == "" {
		tag = field.Tag.Get("json")
	}

	if tag == "" {
		tag = field.Tag.Get("yaml")
	}

	if tag != "" {
		parts := strings.Split(tag, ",")
		if parts[0] == "-" {
			return "", false, true
		}

		for _, opt := range parts[1:] {
			if opt == "squash" || opt == "inline" {
				inline = true
			}
		}

		if parts[0] != "" {
			return parts[0], inline, false
		}
	}

	return strings.ToLower(field.Name), inline, false
}

func joinConfigPath(prefix, name string) string {
	if prefix == "" {
		return name
	}

	if name == "" {
		return prefix
	}

	return prefix + "." + name
}

func isStructLike(value reflect.Value) bool {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return value.Type().Elem().Kind() == reflect.Struct
		}

		value = value.Elem()
	}

	return value.Kind() == reflect.Struct
}
