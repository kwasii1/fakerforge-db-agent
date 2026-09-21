package db

import "testing"

func validatorFor(cols ...Column) *RowValidator {
	return NewRowValidator(cols)
}

func TestValidateRowAllowsValidValues(t *testing.T) {
	v := validatorFor(
		Column{Name: "id", Type: "bigint", IsPK: true},
		Column{Name: "attempts", Type: "tinyint", Unsigned: true},
		Column{Name: "email", Type: "varchar", Length: 255, Unique: true},
		Column{Name: "status", Type: "enum", Values: []string{"draft", "published"}},
	)
	rows := []map[string]any{
		{"id": float64(1), "attempts": float64(0), "email": "a@example.com", "status": "draft"},
		{"id": float64(2), "attempts": float64(255), "email": "b@example.com", "status": "published"},
	}
	for i, row := range rows {
		if err := v.ValidateRow(i, row); err != nil {
			t.Fatalf("row %d should be valid: %v", i, err)
		}
	}
}

func TestValidateRowCatchesIntegerOverflow(t *testing.T) {
	v := validatorFor(Column{Name: "attempts", Type: "tinyint", Unsigned: true})
	err := v.ValidateRow(2, map[string]any{"attempts": float64(256)})
	if err == nil {
		t.Fatal("expected out-of-range error for tinyint unsigned 256")
	}
}

func TestValidateRowCatchesNegativeUnsigned(t *testing.T) {
	v := validatorFor(Column{Name: "count", Type: "int", Unsigned: true})
	if err := v.ValidateRow(0, map[string]any{"count": float64(-1)}); err == nil {
		t.Fatal("expected out-of-range error for unsigned -1")
	}
}

func TestValidateRowAllowsNegativeSigned(t *testing.T) {
	v := validatorFor(Column{Name: "balance", Type: "int"})
	if err := v.ValidateRow(0, map[string]any{"balance": float64(-500)}); err != nil {
		t.Fatalf("signed int should allow negatives: %v", err)
	}
}

func TestValidateRowSkipsBigintRangeCheck(t *testing.T) {
	// Values above 2^53 are not exactly representable as float64, so a
	// decoded JSON number can round past the real bound. The validator must
	// not reject those; the database is the backstop.
	v := validatorFor(Column{Name: "huge", Type: "bigint", Unsigned: true})
	if err := v.ValidateRow(0, map[string]any{"huge": "18446744073709551616"}); err != nil {
		t.Fatalf("bigint range must be skipped, got: %v", err)
	}
}

func TestValidateRowCatchesUnsignedIntOverflow(t *testing.T) {
	v := validatorFor(Column{Name: "count", Type: "int", Unsigned: true})
	if err := v.ValidateRow(0, map[string]any{"count": float64(4294967296)}); err == nil {
		t.Fatal("expected overflow above unsigned int max")
	}
}

func TestValidateRowCatchesLengthOverflow(t *testing.T) {
	v := validatorFor(Column{Name: "code", Type: "varchar", Length: 5})
	if err := v.ValidateRow(0, map[string]any{"code": "abcdef"}); err == nil {
		t.Fatal("expected length error")
	}
}

func TestValidateRowCatchesInvalidEnum(t *testing.T) {
	v := validatorFor(Column{Name: "status", Type: "enum", Values: []string{"draft", "published"}})
	if err := v.ValidateRow(0, map[string]any{"status": "archived"}); err == nil {
		t.Fatal("expected enum membership error")
	}
}

func TestValidateRowAcceptsValidSet(t *testing.T) {
	v := validatorFor(Column{Name: "tags", Type: "set", Values: []string{"a", "b", "c"}})
	if err := v.ValidateRow(0, map[string]any{"tags": "a,c"}); err != nil {
		t.Fatalf("valid set should pass: %v", err)
	}
	if err := v.ValidateRow(1, map[string]any{"tags": "a,z"}); err == nil {
		t.Fatal("expected invalid set member error")
	}
}

func TestValidateRowCatchesDuplicateUnique(t *testing.T) {
	v := validatorFor(Column{Name: "slug", Type: "varchar", Length: 255, Unique: true})
	if err := v.ValidateRow(0, map[string]any{"slug": "hello-world"}); err != nil {
		t.Fatalf("first occurrence should pass: %v", err)
	}
	err := v.ValidateRow(4, map[string]any{"slug": "hello-world"})
	if err == nil {
		t.Fatal("expected duplicate unique error")
	}
}

func TestValidateRowAllowsRepeatedNullInUnique(t *testing.T) {
	v := validatorFor(Column{Name: "code", Type: "varchar", Unique: true, Nullable: true})
	for i := 0; i < 3; i++ {
		if err := v.ValidateRow(i, map[string]any{"code": nil}); err != nil {
			t.Fatalf("nullable unique null should pass: %v", err)
		}
	}
}

func TestValidateRowCatchesNullForNotNull(t *testing.T) {
	v := validatorFor(Column{Name: "name", Type: "varchar", Nullable: false})
	if err := v.ValidateRow(0, map[string]any{"name": nil}); err == nil {
		t.Fatal("expected NOT NULL error")
	}
}

func TestValidateRowCatchesUnknownColumn(t *testing.T) {
	v := validatorFor(Column{Name: "id", Type: "bigint", IsPK: true})
	if err := v.ValidateRow(0, map[string]any{"nope": float64(1)}); err == nil {
		t.Fatal("expected unknown column error")
	}
}

func TestToBigIntRejectsFractionalAndBool(t *testing.T) {
	if _, ok := toBigInt(1.5); ok {
		t.Fatal("fractional float is not an integer")
	}
	if _, ok := toBigInt(true); ok {
		t.Fatal("bool is not an integer")
	}
	if n, ok := toBigInt(float64(42)); !ok || n.String() != "42" {
		t.Fatalf("float64 42 should convert, got %v %v", n, ok)
	}
}
