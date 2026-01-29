package tfrunner

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// FormatTerraformVarsAsArgs converts a map of terraform variables to command line arguments.
// Returns a slice of strings in the format ["-var", "key=value", "-var", "key=value", ...].
func FormatTerraformVarsAsArgs(vars map[string]any) []string {
	var args []string

	// Sort keys for deterministic output
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		hclValue := toHCL(vars[k])
		args = append(args, "-var", fmt.Sprintf("%s=%s", k, hclValue))
	}
	return args
}

// toHCL converts a Go value to its HCL representation.
func toHCL(value any) string {
	if value == nil {
		return "null"
	}

	v := reflect.ValueOf(value)

	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)

	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)

	case reflect.String:
		return quoteHCLString(v.String())

	case reflect.Slice, reflect.Array:
		return formatHCLList(v)

	case reflect.Map:
		return formatHCLMap(v)

	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return "null"
		}
		return toHCL(v.Elem().Interface())

	default:
		return quoteHCLString(fmt.Sprintf("%v", value))
	}
}

// quoteHCLString quotes a string for HCL, escaping special characters.
func quoteHCLString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

// formatHCLList formats a slice or array as an HCL list.
func formatHCLList(v reflect.Value) string {
	var elements []string
	for i := 0; i < v.Len(); i++ {
		elements = append(elements, toHCL(v.Index(i).Interface()))
	}
	return "[" + strings.Join(elements, ", ") + "]"
}

// formatHCLMap formats a map as an HCL object.
func formatHCLMap(v reflect.Value) string {
	if v.Len() == 0 {
		return "{}"
	}

	// Sort keys for deterministic output
	keys := v.MapKeys()
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprintf("%v", keys[i].Interface()) < fmt.Sprintf("%v", keys[j].Interface())
	})

	var pairs []string
	for _, key := range keys {
		keyStr := fmt.Sprintf("%v", key.Interface())
		value := v.MapIndex(key)
		pairs = append(pairs, fmt.Sprintf("%s = %s", keyStr, toHCL(value.Interface())))
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}
