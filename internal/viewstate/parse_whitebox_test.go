package viewstate

import "testing"

func TestParseInt64AndItoa(t *testing.T) {
	if parseInt64("") != 0 || parseInt64("  ") != 0 || parseInt64("12a") != 0 || parseInt64("-1") != 0 {
		t.Fatal("invalid")
	}
	if parseInt64("42") != 42 {
		t.Fatal("42")
	}
	if itoa(0) != "0" || itoa(7) != "7" || itoa(100) != "100" {
		t.Fatal("itoa")
	}
}
