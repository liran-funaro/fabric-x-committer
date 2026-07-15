/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type (
	vtInner struct {
		Count int `mapstructure:"count" validate:"required,gt=0"`
	}

	vtConfig struct {
		Name     string        `mapstructure:"name" validate:"required"`
		Mode     string        `mapstructure:"mode" validate:"omitempty,oneof=a b c"`
		Count    int           `mapstructure:"count" validate:"gte=0"`
		Timeout  time.Duration `mapstructure:"timeout" validate:"gt=0"`
		Required *vtInner      `mapstructure:"required" validate:"required"`
		Optional *vtInner      `mapstructure:"optional"`
		Embedded vtInner       `mapstructure:"embedded"`
		List     []*vtInner    `mapstructure:"list"`
	}
)

func validVTConfig() *vtConfig {
	return &vtConfig{
		Name:     "service",
		Mode:     "a",
		Count:    0,
		Timeout:  time.Second,
		Required: &vtInner{Count: 1},
		Embedded: vtInner{Count: 1},
	}
}

func TestValidateStruct(t *testing.T) {
	t.Parallel()

	t.Run("valid baseline", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateStruct(validVTConfig()))
	})

	// nil is a no-op, mirroring the library's tolerance of typed-nil pointers.
	t.Run("nil pointer", func(t *testing.T) {
		t.Parallel()
		var c *vtConfig
		require.NoError(t, validateStruct(c))
	})

	for _, tc := range []struct {
		name    string
		mutate  func(*vtConfig)
		wantErr string // empty means no error expected
	}{
		{name: "required string empty", mutate: func(c *vtConfig) { c.Name = "" }, wantErr: "Name"},
		{name: "required pointer nil", mutate: func(c *vtConfig) { c.Required = nil }, wantErr: "Required"},
		{name: "omitempty skips oneof when empty", mutate: func(c *vtConfig) { c.Mode = "" }},
		{name: "oneof valid", mutate: func(c *vtConfig) { c.Mode = "c" }},
		{name: "oneof invalid", mutate: func(c *vtConfig) { c.Mode = "z" }, wantErr: "must be one of"},
		{name: "gt satisfied", mutate: func(c *vtConfig) { c.Timeout = time.Nanosecond }},
		{name: "gt violated (zero)", mutate: func(c *vtConfig) { c.Timeout = 0 }, wantErr: "greater than 0"},
		{name: "gte allows zero", mutate: func(c *vtConfig) { c.Count = 0 }},
		{name: "gte violated (negative)", mutate: func(c *vtConfig) { c.Count = -1 }, wantErr: "or equal to 0"},
		{
			name:    "nested required pointer field invalid",
			mutate:  func(c *vtConfig) { c.Required = &vtInner{Count: 0} },
			wantErr: "Required.Count",
		},
		{
			name:    "nested embedded struct invalid",
			mutate:  func(c *vtConfig) { c.Embedded = vtInner{Count: 0} },
			wantErr: "Embedded.Count",
		},
		{
			name:    "optional pointer descended into when set",
			mutate:  func(c *vtConfig) { c.Optional = &vtInner{Count: 0} },
			wantErr: "Optional.Count",
		},
		{name: "optional pointer skipped when nil", mutate: func(c *vtConfig) { c.Optional = nil }},
		{
			name:   "slice elements are not validated (no dive)",
			mutate: func(c *vtConfig) { c.List = []*vtInner{{Count: 0}} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validVTConfig()
			tc.mutate(c)
			err := validateStruct(c)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestValidateStructUnsupportedRule(t *testing.T) {
	t.Parallel()
	err := applyValidationRules(reflect.ValueOf(1), "nope", "Field")
	require.ErrorContains(t, err, "unsupported validation rule")
}
