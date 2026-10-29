package mailmime

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseThreadHeadersAndCopiedRecipients(t *testing.T) {
	raw := []byte("From: sender@example.invalid\r\nTo: to@example.invalid\r\nCc: Copy <cc@example.invalid>\r\nBcc: hidden@example.invalid\r\nMessage-ID: <child@example.invalid>\r\nIn-Reply-To: <parent@example.invalid>\r\nReferences: <root@example.invalid>\r\n <parent@example.invalid>\r\n\r\nbody")
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.InReplyTo != "parent@example.invalid" || m.References != "root@example.invalid parent@example.invalid" || len(m.CC) != 1 || len(m.BCC) != 1 || m.CC[0].Address != "cc@example.invalid" {
		t.Fatal(m)
	}
	m, err = Parse([]byte("From: s@example.invalid\r\nIn-Reply-To: nonsense\r\nReferences: (bad header)\r\n\r\nbody"))
	if err != nil || m.InReplyTo != "" || m.References != "" {
		t.Fatal("optional bad headers blocked mail", m, err)
	}
}
func TestReplyReferencesBoundAndSubjectNormalization(t *testing.T) {
	var ids []string
	for i := 0; i < 100; i++ {
		ids = append(ids, fmt.Sprintf("%d@example.invalid", i))
	}
	result := ReplyReferences(strings.Join(ids, " "), "", "parent@example.invalid")
	if len(result) != 100 || result[0] != "0@example.invalid" || result[99] != "parent@example.invalid" {
		t.Fatal(result)
	}
	for input, want := range map[string]string{"Re: Fwd: 回复: 你好": "你好", "Re: İtest": "İtest", "Same subject": "Same subject"} {
		if got := NormalizeSubject(input); got != want {
			t.Fatal(input, got)
		}
	}
	long := append(ids, "closest@example.invalid", "last@example.invalid")
	trimmed := ThreadHeaderIDs(strings.Join(long, " "))
	if len(trimmed) != 100 || trimmed[0] != ids[0] || trimmed[99] != "last@example.invalid" {
		t.Fatal("lost closest references", trimmed)
	}
	if ids := ThreadHeaderIDs(strings.Repeat("x", (16<<10)+1)); len(ids) != 0 {
		t.Fatal("unbounded header accepted")
	}
}
