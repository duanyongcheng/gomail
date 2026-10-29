package domain

import (
	"strings"
	"testing"
)

func TestGmailLabelNameValidation(t *testing.T) {
	for _, name := range []string{"", "  ", "INBOX", "sent", "CATEGORY_SOCIAL", "a\nb", strings.Repeat("中", 76), string([]byte{0xff})} {
		if _, err := ValidateGmailLabelName(name); err == nil {
			t.Fatalf("accepted invalid name %q", name)
		}
	}
	for input, want := range map[string]string{"  Project/工作  ": "Project/工作", strings.Repeat("x", 225): strings.Repeat("x", 225)} {
		if name, err := ValidateGmailLabelName(input); err != nil || name != want {
			t.Fatal(name, err)
		}
	}
}
