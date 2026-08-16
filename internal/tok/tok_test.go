package tok

import "testing"

func TestCount_Offline(t *testing.T) {
	n, err := Count("hello world")
	if err != nil {
		t.Fatalf("Count returned an error (should be offline, embedded encoding): %v", err)
	}
	if n <= 0 {
		t.Fatalf("expected a positive token count, got %d", n)
	}
	// Deterministic: same input, same count.
	if n2, _ := Count("hello world"); n2 != n {
		t.Fatalf("nondeterministic count: %d vs %d", n, n2)
	}
	// A longer string encodes to more tokens.
	if long, _ := Count("hello world hello world hello world"); long <= n {
		t.Fatalf("expected more tokens for a longer input: %d not > %d", long, n)
	}
}
