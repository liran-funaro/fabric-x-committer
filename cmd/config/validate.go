/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package config

import (
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
)

// validateStruct validates a config value according to its `validate:"..."` field tags.
//
// It replaces github.com/go-playground/validator, whose transitive dependencies are poorly
// maintained and whose advanced features are unused by the committer configs. It supports
// exactly the rule vocabulary the configs rely on:
//
//   - required      the field must not be its zero value (nil for pointers)
//   - omitempty     if the field is its zero value, skip the remaining rules for that field
//   - gt=<number>   numeric field must be greater than <number>
//   - gte=<number>  numeric field must be greater than or equal to <number>
//   - oneof=a b c   string field must equal one of the space-separated values
//
// Rules are evaluated left to right, so `omitempty` short-circuits any rules that follow it.
// Like the library it replaces, validateStruct descends automatically into nested structs and
// non-nil pointers-to-structs. Slice/map/array elements are not descended into (there is no
// `dive` support), matching the previous behavior.
func validateStruct(c any) error {
	return validateValue(reflect.ValueOf(c), "")
}

func validateValue(val reflect.Value, namespace string) error {
	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return nil
	}
	objType := val.Type()
	if namespace == "" {
		namespace = objType.Name()
	}
	for i := range objType.NumField() {
		if err := validateField(objType.Field(i), val.Field(i), namespace); err != nil {
			return err
		}
	}
	return nil
}

// validateField applies a single field's validate tag and then descends into it.
func validateField(field reflect.StructField, fieldValue reflect.Value, namespace string) error {
	if !field.IsExported() {
		return nil
	}
	fieldName := namespace + "." + field.Name
	if tag := field.Tag.Get("validate"); tag != "" {
		if err := applyValidationRules(fieldValue, tag, fieldName); err != nil {
			return err
		}
	}
	// Descend into nested structs and non-nil pointers-to-structs.
	return validateValue(fieldValue, fieldName)
}

// applyValidationRules evaluates the comma-separated rules of a single field's validate tag.
func applyValidationRules(val reflect.Value, tag, name string) error {
	for _, rule := range strings.Split(tag, ",") {
		rule = strings.TrimSpace(rule)
		if rule == "omitempty" {
			if val.IsZero() {
				return nil
			}
			continue
		}
		if err := applyValidationRule(val, rule, name); err != nil {
			return err
		}
	}
	return nil
}

func applyValidationRule(val reflect.Value, rule, name string) error {
	switch {
	case rule == "":
		return nil
	case rule == "required":
		if val.IsZero() {
			return errors.Newf("field %q is required", name)
		}
		return nil
	case strings.HasPrefix(rule, "gt="):
		return validateNumericBound(val, strings.TrimPrefix(rule, "gt="), name, "gt")
	case strings.HasPrefix(rule, "gte="):
		return validateNumericBound(val, strings.TrimPrefix(rule, "gte="), name, "gte")
	case strings.HasPrefix(rule, "oneof="):
		return validateOneOf(val, strings.TrimPrefix(rule, "oneof="), name)
	default:
		return errors.Newf("unsupported validation rule %q on field %q", rule, name)
	}
}

// validateNumericBound checks a "gt"/"gte" numeric bound against int, uint, and float fields.
func validateNumericBound(val reflect.Value, threshold, name, op string) error {
	limit, err := strconv.ParseFloat(strings.TrimSpace(threshold), 64)
	if err != nil {
		return errors.Wrapf(err, "invalid %q threshold %q on field %q", op, threshold, name)
	}
	var num float64
	switch val.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		num = float64(val.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		num = float64(val.Uint())
	case reflect.Float32, reflect.Float64:
		num = val.Float()
	default:
		return errors.Newf("validation rule %q is not applicable to field %q of kind %s", op, name, val.Kind())
	}
	switch op {
	case "gte":
		if num < limit {
			return errors.Newf("field %q must be greater than or equal to %s", name, threshold)
		}
	default: // "gt"
		if num <= limit {
			return errors.Newf("field %q must be greater than %s", name, threshold)
		}
	}
	return nil
}

// validateOneOf checks that a string field equals one of the space-separated allowed values.
func validateOneOf(val reflect.Value, allowed, name string) error {
	if val.Kind() != reflect.String {
		return errors.Newf("validation rule \"oneof\" is not applicable to field %q of kind %s", name, val.Kind())
	}
	if slices.Contains(strings.Fields(allowed), val.String()) {
		return nil
	}
	return errors.Newf("field %q must be one of [%s], got %q", name, allowed, val.String())
}
