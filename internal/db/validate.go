package db

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode/utf8"
)

// signedIntRanges / unsignedIntRanges bound every integer family both
// drivers can report after normalizeType. Keys are the normalized base
// type; values are inclusive [min, max] as decimal strings so the unsigned
// bigint maximum (which overflows int64) is exact.
var signedIntRanges = map[string][2]string{
	"tinyint":   {"-128", "127"},
	"int2":      {"-32768", "32767"},
	"smallint":  {"-32768", "32767"},
	"mediumint": {"-8388608", "8388607"},
	"int":       {"-2147483648", "2147483647"},
	"integer":   {"-2147483648", "2147483647"},
	"int4":      {"-2147483648", "2147483647"},
	"serial":    {"-2147483648", "2147483647"},
	"bigint":    {"-9223372036854775808", "9223372036854775807"},
	"int8":      {"-9223372036854775808", "9223372036854775807"},
	"bigserial": {"-9223372036854775808", "9223372036854775807"},
}

var unsignedIntRanges = map[string][2]string{
	"tinyint":   {"0", "255"},
	"int2":      {"0", "65535"},
	"smallint":  {"0", "65535"},
	"mediumint": {"0", "16777215"},
	"int":       {"0", "4294967295"},
	"integer":   {"0", "4294967295"},
	"int4":      {"0", "4294967295"},
	"serial":    {"0", "4294967295"},
	"bigint":    {"0", "18446744073709551615"},
	"int8":      {"0", "18446744073709551615"},
	"bigserial": {"0", "18446744073709551615"},
}

// RowValidator checks generated row values against the target table's
// introspected constraints — integer range, string length, enum/set
// membership, NOT NULL, and single-column uniqueness — before the rows are
// handed to the database. It is the value-level companion to CheckSubset,
// which only proves the target table is shaped like the schema.
type RowValidator struct {
	cols   map[string]Column
	unique map[string]map[string]int // column -> stringified value -> first row index
}

// NewRowValidator builds a validator from introspected target columns.
func NewRowValidator(dbCols []Column) *RowValidator {
	cols := make(map[string]Column, len(dbCols))
	unique := make(map[string]map[string]int)
	for _, c := range dbCols {
		key := strings.ToLower(c.Name)
		cols[key] = c
		if c.Unique || c.IsPK {
			unique[key] = map[string]int{}
		}
	}
	return &RowValidator{cols: cols, unique: unique}
}

// ValidateRow checks one generated row. rowIndex is zero-based and only
// used for human-readable error messages. It returns the first violation
// found, naming the row, column, offending value, and constraint.
func (v *RowValidator) ValidateRow(rowIndex int, row map[string]any) error {
	for name, raw := range row {
		key := strings.ToLower(name)
		col, ok := v.cols[key]
		if !ok {
			return fmt.Errorf("row %d: column %q is not in the target table", rowIndex+1, name)
		}
		if raw == nil {
			if !col.Nullable {
				return fmt.Errorf("row %d: column %q is NOT NULL but the generated value is null", rowIndex+1, name)
			}
			continue
		}
		if err := v.checkValue(rowIndex, col, raw); err != nil {
			return err
		}
		if seen, isUnique := v.unique[key]; isUnique {
			val := fmt.Sprintf("%v", raw)
			if first, dup := seen[val]; dup {
				return fmt.Errorf("row %d: duplicate value %q for unique column %q (first seen at row %d)",
					rowIndex+1, val, name, first+1)
			}
			seen[val] = rowIndex
		}
	}
	return nil
}

func (v *RowValidator) checkValue(rowIndex int, col Column, raw any) error {
	base := normalizeType(col.Type)

	if (base == "enum" || base == "set") && len(col.Values) > 0 {
		if err := checkEnumValue(rowIndex, col, base, raw); err != nil {
			return err
		}
	}

	if min, max, ok := integerBounds(base, col.Unsigned); ok && exactInFloat64(max) {
		if n, isInt := toBigInt(raw); isInt {
			if n.Cmp(min) < 0 || n.Cmp(max) > 0 {
				return fmt.Errorf("row %d: column %q value %s is out of range %s..%s",
					rowIndex+1, col.Name, n.String(), min.String(), max.String())
			}
		}
	}

	if col.Length > 0 {
		if s, ok := raw.(string); ok {
			if n := utf8.RuneCountInString(s); n > col.Length {
				return fmt.Errorf("row %d: column %q value is %d characters, exceeds max length %d",
					rowIndex+1, col.Name, n, col.Length)
			}
		}
	}

	return nil
}

func checkEnumValue(rowIndex int, col Column, base string, raw any) error {
	s, ok := raw.(string)
	if !ok {
		return fmt.Errorf("row %d: column %q expects one of %v, got %T", rowIndex+1, col.Name, col.Values, raw)
	}
	if base == "enum" {
		if !containsString(col.Values, s) {
			return fmt.Errorf("row %d: column %q value %q is not one of %v", rowIndex+1, col.Name, s, col.Values)
		}
		return nil
	}
	for _, part := range strings.Split(s, ",") {
		if !containsString(col.Values, strings.TrimSpace(part)) {
			return fmt.Errorf("row %d: column %q set value %q is not one of %v", rowIndex+1, col.Name, s, col.Values)
		}
	}
	return nil
}

// maxExactFloatInt is the largest integer float64 represents without loss
// (2^53). Decoded JSON numbers are float64, so range checks above this
// bound would reject legitimate bigint values that rounded up.
var maxExactFloatInt = new(big.Int).Lsh(big.NewInt(1), 53)

func exactInFloat64(max *big.Int) bool {
	return max.Cmp(maxExactFloatInt) <= 0
}

// integerBounds returns the inclusive bounds for a normalized integer type.
func integerBounds(base string, unsigned bool) (*big.Int, *big.Int, bool) {
	var table map[string][2]string
	if unsigned {
		table = unsignedIntRanges
	} else {
		table = signedIntRanges
	}
	r, ok := table[base]
	if !ok {
		return nil, nil, false
	}
	min, _ := new(big.Int).SetString(r[0], 10)
	max, _ := new(big.Int).SetString(r[1], 10)
	if min == nil || max == nil {
		return nil, nil, false
	}
	return min, max, true
}

// toBigInt converts a decoded JSONL value to an exact integer when it is
// integral. Floats that carry a fractional part, booleans, and objects are
// not integers and report false so range checks never fire on them.
func toBigInt(v any) (*big.Int, bool) {
	switch n := v.(type) {
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n {
			return nil, false
		}
		bi, _ := new(big.Float).SetFloat64(n).Int(nil)
		return bi, true
	case float32:
		return toBigInt(float64(n))
	case int:
		return big.NewInt(int64(n)), true
	case int8:
		return big.NewInt(int64(n)), true
	case int16:
		return big.NewInt(int64(n)), true
	case int32:
		return big.NewInt(int64(n)), true
	case int64:
		return big.NewInt(n), true
	case uint:
		return new(big.Int).SetUint64(uint64(n)), true
	case uint8:
		return new(big.Int).SetUint64(uint64(n)), true
	case uint16:
		return new(big.Int).SetUint64(uint64(n)), true
	case uint32:
		return new(big.Int).SetUint64(uint64(n)), true
	case uint64:
		return new(big.Int).SetUint64(n), true
	case json.Number:
		bi, ok := new(big.Int).SetString(string(n), 10)
		return bi, ok
	case string:
		bi, ok := new(big.Int).SetString(strings.TrimSpace(n), 10)
		return bi, ok
	default:
		return nil, false
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
