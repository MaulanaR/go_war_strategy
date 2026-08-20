package server

import "testing"

func TestRandomCodeUsesRequestedLength(t *testing.T) {
	code, err := randomCode(6)
	if err != nil {
		t.Fatalf("randomCode: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("len(code) = %d, want 6", len(code))
	}
	if normalizeCode(" ab12cd ") != "AB12CD" {
		t.Fatal("normalizeCode did not trim and uppercase")
	}
}
