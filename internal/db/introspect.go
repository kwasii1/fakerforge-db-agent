package db

import (
	"fmt"
	"strings"

	"github.com/kwasii1/fakerforge-db-agent/internal/api"
)

// normalizeType lowercases and strips parameters: "VARCHAR(255)" -> "varchar".
func normalizeType(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if i := strings.Index(t, "("); i >= 0 {
		t = t[:i]
	}
	// pg aliases
	switch t {
	case "character varying":
		return "varchar"
	case "character":
		return "char"
	case "double precision":
		return "float8"
	case "timestamp without time zone", "timestamp with time zone":
		return "timestamp"
	}
	return strings.TrimSpace(t)
}

// ToAPISchema converts introspected columns to the upload shape.
func ToAPISchema(cols []Column) []api.SchemaColumn {
	out := make([]api.SchemaColumn, 0, len(cols))
	for _, c := range cols {
		out = append(out, api.SchemaColumn{
			Name: c.Name, Type: c.Type, Nullable: c.Nullable,
			IsPK: c.IsPK, Unique: c.Unique, FKRef: c.FKRef,
		})
	}
	return out
}

// compatibleTypes reports whether a synthetic value of schemaType fits dbType.
func compatibleTypes(schemaType, dbType string) bool {
	s, d := normalizeType(schemaType), normalizeType(dbType)
	if s == d {
		return true
	}
	ints := map[string]bool{"int": true, "integer": true, "int4": true, "int8": true, "bigint": true, "smallint": true, "serial": true, "bigserial": true, "tinyint": true, "mediumint": true}
	if ints[s] && ints[d] {
		return true
	}
	floats := map[string]bool{"float": true, "float4": true, "float8": true, "double": true, "real": true, "decimal": true, "numeric": true}
	if floats[s] && floats[d] {
		return true
	}
	texts := map[string]bool{"varchar": true, "char": true, "text": true, "mediumtext": true, "longtext": true, "tinytext": true, "citext": true, "enum": true, "set": true}
	if texts[s] && texts[d] {
		return true
	}
	times := map[string]bool{"timestamp": true, "timestamptz": true, "datetime": true, "date": true, "time": true}
	if times[s] && times[d] {
		return true
	}
	bools := map[string]bool{"bool": true, "boolean": true, "tinyint": true}
	if bools[s] && bools[d] {
		return true
	}
	if (s == "json" || s == "jsonb") && (d == "json" || d == "jsonb" || d == "text") {
		return true
	}
	if s == "uuid" && (d == "uuid" || d == "varchar" || d == "text" || d == "char") {
		return true
	}
	return false
}

// CheckSubset verifies schema columns are a subset of target columns with
// compatible types. Returns a human diff when not OK.
func CheckSubset(schemaCols []api.SchemaColumn, dbCols []Column) (bool, string) {
	byName := map[string]Column{}
	for _, c := range dbCols {
		byName[strings.ToLower(c.Name)] = c
	}
	var problems []string
	for _, s := range schemaCols {
		d, ok := byName[strings.ToLower(s.Name)]
		if !ok {
			problems = append(problems, fmt.Sprintf("missing column %q in target table", s.Name))
			continue
		}
		if !compatibleTypes(s.Type, d.Type) {
			problems = append(problems, fmt.Sprintf("type mismatch %q: schema %s vs table %s", s.Name, s.Type, d.Type))
		}
	}
	if len(problems) > 0 {
		return false, strings.Join(problems, "\n")
	}
	return true, ""
}

// CheckSubsetMaps is the same check for streamed row keys (quick sanity).
func CheckSubsetMaps(sample map[string]any, dbCols []Column) (bool, string) {
	set := map[string]bool{}
	for _, c := range dbCols {
		set[strings.ToLower(c.Name)] = true
	}
	var missing []string
	for k := range sample {
		if !set[strings.ToLower(k)] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return false, "row keys not in target table: " + strings.Join(missing, ", ")
	}
	return true, ""
}
