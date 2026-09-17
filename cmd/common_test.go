package cmd

import (
	"reflect"
	"testing"
)

func TestExtractFlag(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		flag  string
		value string
		rest  []string
	}{
		{
			name:  "flag after positional",
			args:  []string{"sch_123", "--table", "users"},
			flag:  "table",
			value: "users",
			rest:  []string{"sch_123"},
		},
		{
			name:  "flag before positional",
			args:  []string{"--table", "users", "sch_123"},
			flag:  "table",
			value: "users",
			rest:  []string{"sch_123"},
		},
		{
			name:  "equals form",
			args:  []string{"sch_123", "--table=users"},
			flag:  "table",
			value: "users",
			rest:  []string{"sch_123"},
		},
		{
			name:  "absent",
			args:  []string{"sch_123"},
			flag:  "table",
			value: "",
			rest:  []string{"sch_123"},
		},
		{
			name:  "other flags untouched",
			args:  []string{"sch_123", "--table", "users", "--api-url", "http://x"},
			flag:  "table",
			value: "users",
			rest:  []string{"sch_123", "--api-url", "http://x"},
		},
		{
			name:  "last occurrence wins",
			args:  []string{"--table", "a", "sch_123", "--table", "b"},
			flag:  "table",
			value: "b",
			rest:  []string{"sch_123"},
		},
		{
			name:  "dangling flag kept as positional",
			args:  []string{"sch_123", "--table"},
			flag:  "table",
			value: "",
			rest:  []string{"sch_123", "--table"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, rest := extractFlag(tc.args, tc.flag)
			if value != tc.value {
				t.Fatalf("value = %q, want %q", value, tc.value)
			}
			if !reflect.DeepEqual(rest, tc.rest) {
				t.Fatalf("rest = %q, want %q", rest, tc.rest)
			}
		})
	}
}
