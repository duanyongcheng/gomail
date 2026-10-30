package main

import (
	"bytes"
	"testing"
)

func TestLifecycleSelftestChecksBccAttachmentAndReplyHeaders(t *testing.T) {
	raw, err := lifecycleMIME(lifecycleSubject)
	if err != nil || !lifecycleContent(raw, lifecycleSubject) {
		t.Fatalf("invalid lifecycle fixture: %v", err)
	}
	for _, original := range []string{"hidden@example.invalid", "parent@example.invalid", "ancestor@example.invalid", "remote-selftest.txt"} {
		changed := bytes.Replace(raw, []byte(original), []byte("changed@example.invalid"), 1)
		if bytes.Equal(changed, raw) {
			t.Fatalf("fixture is missing %s", original)
		}
		if lifecycleContent(changed, lifecycleSubject) {
			t.Fatalf("lifecycle verification accepted changed %s", original)
		}
	}
	if lifecycleContent(raw, editedLifecycleSubject) {
		t.Fatal("lifecycle verification accepted an unchanged subject")
	}
}
