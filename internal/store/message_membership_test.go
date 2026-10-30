package store

import (
	"encoding/json"
	"testing"

	"github.com/bary/pebble-go/internal/domain"
)

// Exercise the same cache projections used by Search -> Rendered/Get, Starred,
// folder/combined lists and conversation metadata. Trash is not is_deleted.
func TestMessageTrashMembershipAcrossReadersAndLocalMutations(t *testing.T) {
	db := threadFixture(t)
	cached := cachedThread("a", "member", "shared", 1, "a-inbox")
	cached.Message.Subject = "Membership regression"
	cached.Message.IsStarred = true
	commitThreads(t, db, "a", cached)
	commitThreads(t, db, "b", cachedThread("b", "member", "shared", 1, "b-inbox"))
	schema := integer(t, db.write, "PRAGMA user_version")

	assertReaders := func(db *DB, want bool) {
		t.Helper()
		check := func(m domain.Message) {
			t.Helper()
			if m.ID != "a:member" || m.InTrash == nil || *m.InTrash != want || m.IsDeleted {
				t.Fatalf("unexpected membership: id=%s trash=%v deleted=%v", m.ID, m.InTrash, m.IsDeleted)
			}
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(raw, &wire); err != nil || string(wire["in_trash"]) != map[bool]string{true: "true", false: "false"}[want] {
				t.Fatal("membership missing from message DTO", err)
			}
		}
		m, err := db.Message(t.Context(), "a:member")
		if err != nil {
			t.Fatal(err)
		}
		check(m)
		hits, err := db.Search(t.Context(), "Membership", "a", 50)
		if err != nil || len(hits) != 1 || hits[0].MessageID != m.ID {
			t.Fatal("trashed mail must remain searchable", hits, err)
		}
		for _, starred := range []bool{false, true} {
			messages, err := db.Messages(t.Context(), "", "a", starred, 50, 0)
			if err != nil || len(messages) != 1 {
				t.Fatal("list projection", messages, err)
			}
			check(messages[0])
		}
		folders, err := db.Folders(t.Context(), "a")
		if err != nil {
			t.Fatal(err)
		}
		remote := "INBOX"
		if want {
			remote = "TRASH"
		}
		for _, folder := range folders {
			if folder.RemoteID != remote {
				continue
			}
			messages, err := db.Messages(t.Context(), folder.ID, "", false, 50, 0)
			if err != nil || len(messages) != 1 {
				t.Fatal("folder projection", messages, err)
			}
			check(messages[0])
		}
		handle, err := db.PublicThreadID(t.Context(), m.ID)
		if err != nil {
			t.Fatal(err)
		}
		thread, err := db.ThreadMessages(t.Context(), handle)
		if err != nil || len(thread) != 1 {
			t.Fatal("thread projection", thread, err)
		}
		check(thread[0])
		if thread[0].BodyText != "" || thread[0].BodyHTML != "" {
			t.Fatal("thread metadata loaded bodies")
		}
		other, err := db.Message(t.Context(), "b:member")
		if err != nil || other.InTrash == nil || *other.InTrash {
			t.Fatal("membership crossed accounts", err)
		}
	}

	assertReaders(db, false)
	// The production mutation owns both membership and durable pending intent.
	queueMutation(t, db, MailMutation{MessageIDs: []string{"a:member"}, OpType: "delete"})
	assertReaders(db, true)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), db.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertReaders(reopened, true)
	queueMutation(t, reopened, MailMutation{MessageIDs: []string{"a:member"}, OpType: "restore"})
	assertReaders(reopened, false)
	if integer(t, reopened.write, "PRAGMA user_version") != schema {
		t.Fatal("derived membership must not require a schema change")
	}
}

func TestMessageTrashMembershipUsesOwnedRoleOrRemoteID(t *testing.T) {
	raw, err := json.Marshal(domain.Message{})
	if err != nil {
		t.Fatal(err)
	}
	var synthetic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &synthetic); err != nil {
		t.Fatal(err)
	}
	if _, present := synthetic["in_trash"]; present {
		t.Fatal("synthetic messages may omit the derived field")
	}
	for _, tc := range []struct {
		name, account, remote string
		role                  *string
		want                  bool
	}{
		{"role", "a", "provider-trash", ptr("trash"), true},
		{"remote-without-role", "a", "TRASH", nil, true},
		{"unrelated-folder", "a", "work", nil, false},
		{"another-account", "b", "TRASH", ptr("trash"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := threadFixture(t)
			cached := cachedThread("a", "member", "shared", 1, "a-inbox")
			// A caller-supplied DTO flag is not persisted or trusted as membership.
			inTrash := true
			cached.Message.InTrash = &inTrash
			commitThreads(t, db, "a", cached)
			if err := db.CommitSnapshot(t.Context(), Snapshot{AccountID: tc.account, Folders: []domain.Folder{{ID: "candidate", AccountID: tc.account, RemoteID: tc.remote, Name: "Trash", FolderType: "label", Role: tc.role}}}); err != nil {
				t.Fatal(err)
			}
			// Seed a malformed cross-account membership too: the legacy schema's
			// independent FKs permit it, but it must not grant the message a role.
			execute(t, db.write, "INSERT INTO message_folders(message_id,folder_id) VALUES('a:member','candidate')")
			m, err := db.Message(t.Context(), "a:member")
			if err != nil || m.InTrash == nil || *m.InTrash != tc.want {
				t.Fatalf("trash=%v want %v: %v", m.InTrash, tc.want, err)
			}
			rows, err := db.Messages(t.Context(), "", "a", false, 50, 0)
			if err != nil || len(rows) != 1 {
				t.Fatal("membership join duplicated the message", rows, err)
			}
		})
	}
}
