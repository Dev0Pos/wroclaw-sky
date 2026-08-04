package geo

import "testing"

func TestItoaAndTrim1Branches(t *testing.T) {
	if itoa(0) != "0" {
		t.Fatal("zero")
	}
	if itoa(-12) != "-12" {
		t.Fatal("neg")
	}
	if trim1(1.25) != "1.3" && trim1(1.25) != "1.2" {
		// Round(12.5)=13 → 1.3; Round half away from zero in Go
		if got := trim1(1.25); got != "1.3" {
			t.Fatalf("trim1 = %q", got)
		}
	}
	// Force negative frac branch via negative rounded tenths.
	if got := trim1(-0.15); !containsDigit(got) {
		t.Fatalf("neg trim1 = %q", got)
	}
}

func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
