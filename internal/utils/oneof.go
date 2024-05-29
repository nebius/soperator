package utils

import (
	"reflect"
)

// ValidateOneOf checks if only one pointer field is specified in the struct
func ValidateOneOf(v any) bool {
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Struct {
		return false
	}

	count := 0

	// Iterate through the struct's fields
	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)

		// Check if the field is a pointer and is not nil
		if field.Kind() == reflect.Ptr && !field.IsNil() {
			count++
			if count > 1 {
				return false
			}
		}
	}

	return count == 1
}
