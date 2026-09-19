package httpapi

import (
	"strings"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestNormalizeSearchField(t *testing.T) {
	tooLong := strings.Repeat("a", maxSearchFieldLength+1)

	cases := []struct {
		name      string
		in        *string
		wantNil   bool
		wantValue string
		wantOK    bool
	}{
		{"nil stays nil", nil, true, "", true},
		{"empty string becomes nil", strPtr(""), true, "", true},
		{"whitespace-only becomes nil", strPtr("   "), true, "", true},
		{"value is trimmed", strPtr("  nurse_j  "), false, "nurse_j", true},
		{"value at the limit is kept", strPtr(strings.Repeat("a", maxSearchFieldLength)), false, strings.Repeat("a", maxSearchFieldLength), true},
		{"value over the limit is rejected", strPtr(tooLong), false, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeSearchField(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("got %q, want nil", *got)
				}
				return
			}
			if got == nil || *got != tc.wantValue {
				t.Fatalf("got %v, want %q", got, tc.wantValue)
			}
		})
	}
}
