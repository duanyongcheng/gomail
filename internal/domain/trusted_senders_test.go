package domain

import "testing"

func TestNormalizeTrustedMailboxOnly(t *testing.T) {
	if value, err := NormalizeTrustedEmail(" Sender@Example.invalid "); err != nil || value != "sender@example.invalid" {
		t.Fatal(value, err)
	}
	for _, value := range []string{"", "*.example.invalid", "*@example.invalid", "Name <sender@example.invalid>", "a@example.invalid,b@example.invalid", "a@example.invalid\r\nBcc:b@example.invalid", "a@域名.invalid", "a b@example.invalid", "@example.invalid", "a@", "sender@example.invalid\n", "\tsender@example.invalid", "\u00a0sender@example.invalid"} {
		if _, err := NormalizeTrustedEmail(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}
