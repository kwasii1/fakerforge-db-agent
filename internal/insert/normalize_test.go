package insert

import "testing"

func TestNormalizeValue(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"nil passthrough", nil, nil},
		{"string passthrough", "a@x.com", "a@x.com"},
		{"float passthrough", 42.0, 42.0},
		{"bool passthrough", true, true},
		// Date values travel as {"date": "..."} maps (server API shape).
		{"date map unwrapped", map[string]any{"date": "2026-09-17 00:00:00"}, "2026-09-17 00:00:00"},
		// PHP DateTime json_encode shape: date + timezone keys.
		{"php datetime unwrapped", map[string]any{"date": "2008-05-11 23:02:01.000000", "timezone_type": 3.0, "timezone": "UTC"}, "2008-05-11 23:02:01.000000"},
		// json() faker columns arrive as real objects → JSON strings.
		{"map to JSON", map[string]any{"b": 2.0, "a": "x"}, `{"a":"x","b":2}`},
		{"nested map to JSON", map[string]any{"n": map[string]any{"x": 1.0}}, `{"n":{"x":1}}`},
		{"slice to JSON", []any{"a", 1.0}, `["a",1]`},
		{"empty map to JSON", map[string]any{}, `{}`},
		// Multi-key maps containing "date" but no "timezone_type" are data.
		{"date plus other keys stays JSON", map[string]any{"date": "x", "note": "y"}, `{"date":"x","note":"y"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeValue(tc.in); got != tc.want {
				t.Fatalf("normalizeValue(%v) = %v (%T), want %v", tc.in, got, got, tc.want)
			}
		})
	}
}
