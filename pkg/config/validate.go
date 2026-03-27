package config

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// ValidationError contains details about a single validation failure.
type ValidationError struct {
	// Key is the dot-separated config key.
	Key string
	// Message is a human-readable description of the failure.
	Message string
	// Hint is an actionable fix suggestion.
	Hint string
}

func (e ValidationError) String() string {
	s := fmt.Sprintf("%s: %s", e.Key, e.Message)
	if e.Hint != "" {
		s += fmt.Sprintf(" (hint: %s)", e.Hint)
	}

	return s
}

// ValidationResult holds the outcome of schema validation.
type ValidationResult struct {
	Errors   []ValidationError
	Warnings []ValidationError
}

// Valid returns true if no errors were found. Warnings do not affect validity.
func (r *ValidationResult) Valid() bool {
	return len(r.Errors) == 0
}

// Error returns a formatted multi-line error string, or empty string if valid.
func (r *ValidationResult) Error() string {
	if r.Valid() {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("config validation failed:\n")

	for _, e := range r.Errors {
		fmt.Fprintf(&sb, "  %s\n", e.String())
	}

	return sb.String()
}

func (r *ValidationResult) addError(key, message, hint string) {
	r.Errors = append(r.Errors, ValidationError{Key: key, Message: message, Hint: hint})
}

func (r *ValidationResult) addWarning(key, message, hint string) {
	r.Warnings = append(r.Warnings, ValidationError{Key: key, Message: message, Hint: hint})
}

// Validate checks the current configuration against the provided schema.
// Returns a ValidationResult; callers should check result.Valid().
func (c *Container) Validate(schema *Schema) *ValidationResult {
	result := &ValidationResult{}

	for key, field := range schema.fields {
		value := c.viper.Get(key)
		validateField(key, field, value, result)
	}

	detectUnknownKeys(c.viper.AllKeys(), schema.fields, result, schema.strict)

	return result
}

func validateField(key string, field FieldSchema, value any, result *ValidationResult) {
	// Check required
	if field.Required {
		if value == nil || isZeroValue(value) {
			envKey := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
			result.addError(key, "required field is missing",
				fmt.Sprintf("add %s to your config file or set the %s environment variable", key, envKey))

			return
		}
	}

	if value == nil {
		return
	}

	// Check type
	if field.Type != "" && !typeMatches(field.Type, value) {
		result.addError(key, fmt.Sprintf("expected type %s but got %T", field.Type, value),
			fmt.Sprintf("ensure %s has a value of type %s", key, field.Type))
	}

	// Check enum
	if len(field.Enum) > 0 {
		strVal := fmt.Sprintf("%v", value)

		allowed := make([]string, len(field.Enum))
		for i, e := range field.Enum {
			allowed[i] = fmt.Sprintf("%v", e)
		}

		if !slices.Contains(allowed, strVal) {
			result.addError(key, fmt.Sprintf("value %q is not allowed", strVal),
				fmt.Sprintf("allowed values: %s", strings.Join(allowed, ", ")))
		}
	}
}

func isZeroValue(v any) bool {
	switch val := v.(type) {
	case string:
		return val == ""
	case int:
		return val == 0
	case int64:
		return val == 0
	case float64:
		return val == 0
	case bool:
		return !val
	default:
		return false
	}
}

func typeMatches(expected string, value any) bool {
	switch expected {
	case "string":
		return isString(value)
	case "int":
		return isInt(value)
	case "float64":
		return isFloat(value)
	case "bool":
		return isBool(value)
	case "duration":
		return isDuration(value)
	default:
		return true
	}
}

func isString(v any) bool {
	_, ok := v.(string)

	return ok
}

func isInt(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64:
		return true
	default:
		return false
	}
}

func isFloat(v any) bool {
	switch v.(type) {
	case float32, float64:
		return true
	default:
		return false
	}
}

func isBool(v any) bool {
	_, ok := v.(bool)

	return ok
}

func isDuration(v any) bool {
	switch val := v.(type) {
	case time.Duration:
		_ = val

		return true
	case string:
		_, err := time.ParseDuration(val)

		return err == nil
	default:
		return false
	}
}

func detectUnknownKeys(allKeys []string, fields map[string]FieldSchema, result *ValidationResult, strict bool) {
	for _, key := range allKeys {
		if _, known := fields[key]; known {
			continue
		}

		if isKnownKeyPrefix(key, fields) {
			continue
		}

		msg := "unknown configuration key"
		hint := fmt.Sprintf("check for typos; remove %s if it is not needed", key)

		if strict {
			result.addError(key, msg, hint)
		} else {
			result.addWarning(key, msg, hint)
		}
	}
}

// isKnownKeyPrefix returns true if key is a parent prefix of any known schema field.
func isKnownKeyPrefix(key string, fields map[string]FieldSchema) bool {
	prefix := key + "."

	for known := range fields {
		if strings.HasPrefix(known, prefix) {
			return true
		}
	}

	return false
}
