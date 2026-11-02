package main

import (
	"bufio"
	"bytes"
	"reflect"
	"testing"

	"github.com/bary/pebble-go/internal/mailmime"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"
)

func TestSelftestEncodedAddressNamesPreserveMultipartFixture(t *testing.T) {
	raw, err := mailmime.Build("sender@example.invalid", "Synthetic subject", "fixture text", "<p>fixture HTML</p>", "fixture@example.invalid", []*mail.Address{{Address: "demo@example.invalid"}}, nil, []mailmime.Attachment{{Filename: "fixture.txt", MIMEType: "text/plain", Data: []byte("attachment bytes")}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := selftestEncodedAddressNames(raw)
	if err != nil {
		t.Fatal(err)
	}
	header, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if header.Get("From") != `"=?utf-8?B?5Y2P6K6u5rWL6K+V?=" <sender@example.invalid>` || header.Get("To") != `"=?utf-8?B?5pS25Lu25rWL6K+V?=" <demo@example.invalid>` {
		t.Fatal("fixture does not contain quoted encoded address names")
	}
	before, err := mailmime.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	after, err := mailmime.Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if *after.From.Name != "协议测试" || *after.To[0].Name != "收件测试" || after.From.Address != before.From.Address || after.To[0].Address != before.To[0].Address {
		t.Fatal("synthetic address identities or names changed incorrectly", after)
	}
	before.From, before.To = after.From, after.To
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rewriting address headers changed fixture body, attachments or other headers")
	}
}
