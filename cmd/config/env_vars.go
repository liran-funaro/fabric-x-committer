/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package config

import (
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/spf13/viper"
)

type envVarsSetter struct {
	v           *viper.Viper
	envPrefix   string
	envReplacer *strings.Replacer
}

// setEnvVars recursively walks through struct fields and manually sets environment variables in viper.
// This allows environment variables to override nested pointer struct fields.
// E.g. SC_LOGGING_ENABLED=false.
func (e *envVarsSetter) setEnvVars(objType reflect.Type, keyParts ...string) {
	// Dereference pointer types.
	for objType.Kind() == reflect.Ptr {
		objType = objType.Elem()
	}
	if objType.Kind() != reflect.Struct {
		return
	}

	for i := range objType.NumField() {
		field := objType.Field(i)
		if !field.IsExported() {
			continue
		}
		attriburtes := getAttributes(field)
		if len(attriburtes) == 0 {
			continue
		}

		fieldKeyParts := keyParts
		if !slices.Contains(attriburtes[1:], "squash") {
			fieldKeyParts = append(fieldKeyParts, attriburtes[0])
		}
		fieldKey := strings.Join(fieldKeyParts, ".")

		// Check and set environment variable for this field.
		envKey := e.envPrefix + "_" + strings.ToUpper(e.envReplacer.Replace(fieldKey))
		if envValue, ok := os.LookupEnv(envKey); ok && envValue != "" {
			logger.Debugf("Setting config key '%s' from env var '%s=%s'", fieldKey, envKey, envValue)
			e.v.Set(fieldKey, envValue)
		}

		// Handle nested structs.
		e.setEnvVars(field.Type, fieldKeyParts...)
	}
}

func getAttributes(field reflect.StructField) []string {
	mapTag := field.Tag.Get("mapstructure")
	if mapTag == "" || mapTag == "-" {
		return nil
	}
	attributes := strings.Split(mapTag, ",")
	for i, a := range attributes {
		attributes[i] = strings.Trim(a, " ")
	}
	return attributes
}
