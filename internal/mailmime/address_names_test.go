package mailmime

import (
	"strings"
	"testing"
)

func TestParseDecodesAddressDisplayNames(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"standard B", "=?utf-8?B?5q615rC45oiQ?=", "段永成"},
		{"quoted B", `"=?utf-8?B?5q615rC45oiQ?="`, "段永成"},
		{"quoted Q", `"=?utf-8?Q?=E6=AE=B5=E6=B0=B8=E6=88=90?="`, "段永成"},
		{"encoded ASCII", `"=?utf-8?B?aWNlbmVvMDA0?="`, "iceneo004"},
		{"adjacent words", `"=?utf-8?B?5q61?= =?utf-8?B?5rC45oiQ?="`, "段永成"},
		{"mixed text", `"Team =?utf-8?B?5q615rC45oiQ?="`, "Team 段永成"},
		{"GBK", `"=?GBK?B?ts7TwLPJ?="`, "段永成"},
		{"plain quoted", `"Doe, Jane"`, "Doe, Jane"},
		{"literal Unicode", `"段永成"`, "段永成"},
		{"unnamed", "", ""},
		{"decoded punctuation", `"=?utf-8?Q?Doe=2C_Jane_=3Cteam=3E?="`, "Doe, Jane <team>"},
		{"invalid UTF-8", `"=?utf-8?B?/w==?="`, "=?utf-8?B?/w==?="},
		{"encoded control", `"=?utf-8?Q?Alice=0AInjected?="`, "=?utf-8?Q?Alice=0AInjected?="},
		{"malformed encoding", `"=?utf-8?B?!!!!?="`, "=?utf-8?B?!!!!?="},
		{"unknown charset", `"=?not-a-charset?B?YWxpY2U=?="`, "=?not-a-charset?B?YWxpY2U=?="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte("From: " + tc.raw + " <sender@example.invalid>\r\n" +
				"To: " + tc.raw + " <recipient@example.invalid>, Plain <other@example.invalid>\r\n" +
				"Cc: " + tc.raw + " <copy@example.invalid>\r\n" +
				"Bcc: " + tc.raw + " <hidden@example.invalid>\r\n" +
				"Subject: Reply\r\nMessage-ID: <reply@example.invalid>\r\n" +
				"In-Reply-To: <parent@example.invalid>\r\nReferences: <root@example.invalid> <parent@example.invalid>\r\n" +
				"Date: Mon, 2 Nov 2026 01:52:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello")
			for _, parse := range []struct {
				name string
				fn   func([]byte, int) (Message, error)
			}{{"incoming", ParseWithLimit}, {"draft", ParseDraftWithLimit}} {
				t.Run(parse.name, func(t *testing.T) {
					m, err := parse.fn(raw, MaxMessageBytes)
					if err != nil {
						t.Fatal(err)
					}
					for _, field := range []struct {
						name, address string
						value         Address
					}{{"From", "sender@example.invalid", m.From}, {"To", "recipient@example.invalid", m.To[0]}, {"Cc", "copy@example.invalid", m.CC[0]}, {"Bcc", "hidden@example.invalid", m.BCC[0]}} {
						if field.value.Name == nil || *field.value.Name != tc.want || field.value.Address != field.address {
							t.Errorf("%s: got %+v, want name %q with unchanged address", field.name, field.value, tc.want)
						}
					}
					if len(m.To) != 2 || *m.To[1].Name != "Plain" || m.To[1].Address != "other@example.invalid" {
						t.Fatal("recipient order or cardinality changed", m.To)
					}
					if m.Subject != "Reply" || m.MessageID != "reply@example.invalid" || m.InReplyTo != "parent@example.invalid" || m.References != "root@example.invalid parent@example.invalid" || strings.TrimSpace(m.BodyText) != "hello" {
						t.Fatal("non-name message data changed", m)
					}
				})
			}
		})
	}
}

func TestDecodeAddressNameBestEffortAndSafety(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"empty", "", ""},
		{"literal Unicode", "段永成", "段永成"},
		{"literal punctuation", "Doe, Jane <team>", "Doe, Jane <team>"},
		{"Q spaces", "=?utf-8?Q?Alice_Smith?=", "Alice Smith"},
		{"case insensitive encoding", "=?UTF-8?b?5q615rC45oiQ?=", "段永成"},
		{"legacy charset", "=?iso-8859-1?Q?Andr=E9?=", "André"},
		{"folded adjacent words", "=?utf-8?B?5q61?=\r\n\t=?utf-8?B?5rC45oiQ?=", "段永成"},
		{"decoded punctuation is only a name", "=?utf-8?Q?Doe=2C_Jane_=3Cteam=3E?=", "Doe, Jane <team>"},
		{"no recursive decoding", "=?utf-8?Q?=3D=3Futf-8=3FB=3FYWxpY2U=3D=3F=3D?=", "=?utf-8?B?YWxpY2U=?="},
		{"invalid base64", "=?utf-8?B?!!!!?=", "=?utf-8?B?!!!!?="},
		{"unknown charset", "=?unknown-charset?B?YWxpY2U=?=", "=?unknown-charset?B?YWxpY2U=?="},
		{"failed decoding keeps whole name", "=?utf-8?Q?Alice?= =?unknown-charset?B?YWxpY2U=?=", "=?utf-8?Q?Alice?= =?unknown-charset?B?YWxpY2U=?="},
		{"invalid UTF-8", "=?utf-8?B?/w==?=", "=?utf-8?B?/w==?="},
		{"encoded NUL", "=?utf-8?Q?Alice=00?=", "=?utf-8?Q?Alice=00?="},
		{"encoded newline", "=?utf-8?Q?Alice=0AInjected?=", "=?utf-8?Q?Alice=0AInjected?="},
		{"encoded CRLF", "=?utf-8?Q?Alice=0D=0ABcc=3A_hidden=40example.invalid?=", "=?utf-8?Q?Alice=0D=0ABcc=3A_hidden=40example.invalid?="},
		{"encoded Unicode control", "=?utf-8?B?woU=?=", "=?utf-8?B?woU=?="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecodeAddressName(tc.raw); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
