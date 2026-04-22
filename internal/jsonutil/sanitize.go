// Package jsonutil provides helpers for JSON encoding that handles
// problematic floating-point values like NaN and +/-Inf.
package jsonutil

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"time"
)

// MarshalNoNaN returns the JSON encoding of v, replacing any NaN or
// +/-Inf float64 values with nil (which encodes as JSON null).
func MarshalNoNaN(v interface{}) ([]byte, error) {
	sanitized := sanitizeNaN(reflect.ValueOf(v))
	return json.Marshal(sanitized)
}

// sanitizeNaN recursively walks a value and replaces NaN/Inf float64s
// with nil. Structs are converted to map[string]interface{} so that
// nil fields encode as JSON null rather than the zero value.
func sanitizeNaN(v reflect.Value) interface{} {
	if !v.IsValid() {
		return nil
	}

	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return nil
		}
		return sanitizeNaN(v.Elem())
	case reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return sanitizeNaN(v.Elem())
	case reflect.Slice:
		result := make([]interface{}, v.Len())
		for i := 0; i < v.Len(); i++ {
			result[i] = sanitizeNaN(v.Index(i))
		}
		return result
	case reflect.Map:
		result := make(map[string]interface{})
		for _, key := range v.MapKeys() {
			if key.Kind() == reflect.String {
				result[key.String()] = sanitizeNaN(v.MapIndex(key))
			}
		}
		return result
	case reflect.Struct:
		// Let time.Time marshal normally — it has no float fields and its
		// unexported internals break our map-based sanitization.
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return v.Interface()
		}
		result := make(map[string]interface{})
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if !field.IsValid() || !field.CanInterface() {
				continue
			}
			jsonName := t.Field(i).Tag.Get("json")
			if jsonName == "-" {
				continue
			}
			name := splitJSONName(jsonName)
			if name == "" {
				// No json tag — use the field name directly
				name = t.Field(i).Name
			}
			result[name] = sanitizeNaN(field)
		}
		return result
	case reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return f
	default:
		return v.Interface()
	}
}

func splitJSONName(tag string) string {
	if i := strings.Index(tag, ","); i >= 0 {
		return tag[:i]
	}
	return tag
}
