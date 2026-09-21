package insert

import "testing"

func TestBuildTruncateSQL(t *testing.T) {
	if got := buildTruncateSQL("postgres", []string{"orgs", "users"}); got != `TRUNCATE TABLE "orgs", "users"` {
		t.Fatalf("got %q", got)
	}
	if got := buildTruncateSQL("mysql", []string{"orgs", "users"}); got != "TRUNCATE TABLE `orgs`, `users`" {
		t.Fatalf("got %q", got)
	}
	if got := buildTruncateSQL("postgres", []string{`we"ird`}); got != `TRUNCATE TABLE "weird"` {
		t.Fatalf("identifiers must be sanitized, got %q", got)
	}
}
