package store

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestUpgradeFromEachLegacyVersion(t *testing.T) {
	for version := 1; version <= LegacySchemaVersion; version++ {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			db := openRaw(t)
			if err := migrate(t.Context(), db, version); err != nil {
				t.Fatal(err)
			}
			if integer(t, db, "PRAGMA user_version") != version {
				t.Fatal("migration stamped the wrong intermediate version")
			}
			if err := migrate(t.Context(), db, SchemaVersion); err != nil {
				t.Fatal(err)
			}
			checkLegacySchema(t, db)
		})
	}
}

func TestV12RebuildPreservesChildrenAndCredentials(t *testing.T) {
	db := openRaw(t)
	execute(t, db, `CREATE TABLE accounts (
 id TEXT PRIMARY KEY, email TEXT NOT NULL,display_name TEXT NOT NULL DEFAULT '',color TEXT,
 provider TEXT NOT NULL CHECK(provider IN ('imap','gmail','outlook')),
 auth_data BLOB,sync_state TEXT,created_at INTEGER NOT NULL,updated_at INTEGER NOT NULL
);
CREATE TABLE folders(id TEXT PRIMARY KEY,account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE);
INSERT INTO accounts VALUES('a','a@example.test','Name','#0ea5e9','gmail',X'123456','{"gmail_history_id":"999999999999999999"}',1,1);
INSERT INTO folders VALUES('f','a');
PRAGMA user_version=11;`)
	if err := migrate(t.Context(), db, 12); err != nil {
		t.Fatal(err)
	}
	var auth []byte
	var state, definition string
	if err := db.QueryRow("SELECT auth_data,sync_state FROM accounts WHERE id='a'").Scan(&auth, &state); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(auth, []byte{0x12, 0x34, 0x56}) || state != `{"gmail_history_id":"999999999999999999"}` {
		t.Fatal("credentials or sync state changed")
	}
	if integer(t, db, "SELECT count(*) FROM folders WHERE account_id='a'") != 1 {
		t.Fatal("account rebuild lost children")
	}
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE name='folders'").Scan(&definition); err != nil || strings.Contains(definition, "legacy") {
		t.Fatalf("child foreign key changed: %q, %v", definition, err)
	}
	execute(t, db, "INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('pop','pop','pop3',1,1)")
	execute(t, db, "DELETE FROM accounts WHERE id='a'")
	if integer(t, db, "SELECT count(*) FROM folders") != 0 || integer(t, db, "PRAGMA foreign_keys") != 1 {
		t.Fatal("FK enforcement was not restored")
	}
}

func TestV12ViolationRollsBackAndRestoresFK(t *testing.T) {
	db := openRaw(t)
	execute(t, db, `PRAGMA foreign_keys=OFF;
CREATE TABLE accounts(id TEXT PRIMARY KEY,email TEXT,display_name TEXT,color TEXT,provider TEXT,auth_data BLOB,sync_state TEXT,created_at INTEGER,updated_at INTEGER);
CREATE TABLE folders(id TEXT PRIMARY KEY,account_id TEXT REFERENCES accounts(id));
INSERT INTO folders VALUES('orphan','missing');
PRAGMA user_version=11;`)
	if err := migrate(t.Context(), db, 12); err == nil {
		t.Fatal("expected foreign key violation")
	}
	if integer(t, db, "PRAGMA user_version") != 11 || integer(t, db, "PRAGMA foreign_keys") != 1 {
		t.Fatal("failed V12 changed version or left FKs off")
	}
	if integer(t, db, "SELECT count(*) FROM sqlite_master WHERE name='accounts_new'") != 0 {
		t.Fatal("failed V12 leaked rebuilt table")
	}
}

func TestV18FailureRollsBackEarlierAlter(t *testing.T) {
	db := openRaw(t)
	execute(t, db, "CREATE TABLE accounts(id TEXT PRIMARY KEY,provider_display_name TEXT); PRAGMA user_version=17;")
	if err := migrate(t.Context(), db, 18); err == nil {
		t.Fatal("expected duplicate-column migration failure")
	}
	if integer(t, db, "PRAGMA user_version") != 17 {
		t.Fatal("failed migration advanced version")
	}
	if integer(t, db, "SELECT count(*) FROM pragma_table_info('accounts') WHERE name='account_label'") != 0 {
		t.Fatal("failed migration leaked its first ALTER")
	}
}

func TestV19DoesNotSkipV20OnResume(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 18); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES
 ('new','new','gmail',300,300),('old','old','gmail',100,100),('middle','middle','gmail',200,200);`)
	if err := migrate(t.Context(), db, 19); err != nil {
		t.Fatal(err)
	}
	if integer(t, db, "PRAGMA user_version") != 19 {
		t.Fatal("V19 claimed later migrations were applied")
	}
	if err := migrate(t.Context(), db, SchemaVersion); err != nil {
		t.Fatal(err)
	}
	var order string
	if err := db.QueryRow("SELECT group_concat(id,',') FROM (SELECT id FROM accounts ORDER BY sort_order)").Scan(&order); err != nil || order != "old,middle,new" {
		t.Fatalf("order = %q, %v", order, err)
	}
}

func TestUnknownOrUnversionedDatabaseIsPreserved(t *testing.T) {
	for _, version := range []int{0, SchemaVersion + 1} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			db := openRaw(t)
			execute(t, db, "CREATE TABLE sentinel(value TEXT); INSERT INTO sentinel VALUES('preserve');")
			execute(t, db, fmt.Sprintf("PRAGMA user_version=%d", version))
			if err := migrate(t.Context(), db, SchemaVersion); err == nil {
				t.Fatal("expected unsupported database error")
			}
			if integer(t, db, "PRAGMA user_version") != version || integer(t, db, "SELECT count(*) FROM sentinel WHERE value='preserve'") != 1 {
				t.Fatal("unsupported database changed")
			}
		})
	}
}

func TestV14DedupRetainsRelatedDataAndIMAPMailboxScope(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 13); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('g','g','gmail',1,1),('i','i','imap',1,1);
INSERT INTO folders(id,account_id,remote_id,name,folder_type) VALUES('inbox','g','INBOX','Inbox','label'),('archive','g','ARCHIVE','Archive','label'),('imap-inbox','i','INBOX','Inbox','folder'),('imap-archive','i','ARCHIVE','Archive','folder');
INSERT INTO messages(id,account_id,remote_id,date,created_at,updated_at) VALUES
 ('loser','g','remote',1,1,1),('winner','g','remote',1,2,2),('imap-a','i','42',1,1,1),('imap-b','i','42',1,2,2);
INSERT INTO message_folders VALUES('loser','archive'),('winner','inbox'),('imap-a','imap-inbox'),('imap-b','imap-archive');
INSERT INTO attachments(id,message_id,local_path) VALUES('att','loser','attachments/safe');
INSERT INTO labels(id,name) VALUES('l','Label');
INSERT INTO message_labels VALUES('loser','l');
INSERT INTO kanban_cards VALUES('loser','done',9,1,20),('winner','todo',1,1,10);
INSERT INTO snoozed_messages VALUES('loser',30,60,'archive'),('winner',10,40,'inbox');
INSERT INTO search_pending VALUES('loser','index',20),('winner','remove',10);
INSERT INTO pending_mail_ops(id,account_id,message_id,op_type,payload_json,status,created_at,updated_at) VALUES('op','g','loser','flags','{}','pending',1,1);`)
	if err := migrate(t.Context(), db, 14); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT count(*) FROM messages WHERE id='winner'",
		"SELECT count(*) FROM attachments WHERE message_id='winner' AND local_path='attachments/safe'",
		"SELECT count(*) FROM message_labels WHERE message_id='winner'",
		"SELECT count(*) FROM kanban_cards WHERE message_id='winner' AND column_name='done' AND position=9",
		"SELECT count(*) FROM snoozed_messages WHERE message_id='winner' AND snoozed_at=30",
		"SELECT count(*) FROM search_pending WHERE message_id='winner' AND operation='index' AND created_at=20",
		"SELECT count(*) FROM pending_mail_ops WHERE message_id='winner'",
	} {
		if integer(t, db, query) != 1 {
			t.Errorf("lost related data: %s", query)
		}
	}
	if integer(t, db, "SELECT count(*) FROM messages WHERE id='loser'") != 0 || integer(t, db, "SELECT count(*) FROM message_folders WHERE message_id='winner'") != 2 {
		t.Fatal("Gmail dedup membership wrong")
	}
	if integer(t, db, "SELECT count(*) FROM messages WHERE account_id='i'") != 2 {
		t.Fatal("V14 merged IMAP UIDs from different mailboxes")
	}
}

func TestV16NormalizesDuplicatesAndEnforcesAllUIDPaths(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 15); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('g','g','gmail',1,1),('i','i','imap',1,1);
INSERT INTO folders(id,account_id,remote_id,name,folder_type) VALUES('f1','i','INBOX','Inbox','folder'),('f2','i','Archive','Archive','folder');
INSERT INTO messages(id,account_id,remote_id,date,created_at,updated_at) VALUES
 ('g-old','g','42',1,1,1),('g-new','g','42',1,2,2),
 ('i-old','i','7',1,1,1),('i-new','i','7',1,2,2),('i-other','i','7',1,3,3),('i-update','i','8',1,1,1);
INSERT INTO message_folders VALUES('i-old','f1'),('i-new','f1'),('i-other','f2'),('i-update','f1');
INSERT INTO attachments(id,message_id) VALUES('att','i-old');`)
	if err := migrate(t.Context(), db, 16); err != nil {
		t.Fatal(err)
	}
	if integer(t, db, "SELECT count(*) FROM messages WHERE id IN ('g-old','i-old')") != 0 || integer(t, db, "SELECT count(*) FROM attachments WHERE message_id='i-new'") != 1 {
		t.Fatal("V16 did not merge scoped duplicates and attachments")
	}
	for _, query := range []string{
		"INSERT INTO messages(id,account_id,remote_id,date,created_at,updated_at) VALUES('dup','g','42',1,1,1)",
		"UPDATE messages SET remote_id='7' WHERE id='i-update'",
		"INSERT INTO message_folders VALUES('i-other','f1')",
		"UPDATE message_folders SET folder_id='f1' WHERE message_id='i-other'",
	} {
		if _, err := db.Exec(query); err == nil {
			t.Errorf("UID constraint missed: %s", query)
		}
	}
	// A UID in another folder and local drafts remain distinct valid records.
	if integer(t, db, "SELECT count(*) FROM messages WHERE account_id='i' AND remote_id='7'") != 2 {
		t.Fatal("V16 merged separate IMAP mailbox UIDs")
	}
	execute(t, db, `INSERT INTO messages(id,account_id,remote_id,date,created_at,updated_at) VALUES('d1','g','',1,1,1),('d2','g','',1,1,1);`)
}

func TestV21BackfillsActiveMessagesOnly(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 20); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','a','gmail',1,1);
INSERT INTO messages(id,account_id,remote_id,body_text,is_deleted,date,created_at,updated_at) VALUES
 ('active','a','r1','中文搜索',0,1,1,1),('deleted','a','r2','中文搜索',1,1,1,1);`)
	if err := migrate(t.Context(), db, 21); err != nil {
		t.Fatal(err)
	}
	if integer(t, db, `SELECT count(*) FROM message_search WHERE message_search MATCH '"中文搜索"'`) != 1 {
		t.Fatal("search backfill ignored active/deleted state")
	}
}

func TestCancelledMigrationDoesNotAdvanceVersion(t *testing.T) {
	db := openRaw(t)
	if err := migrate(t.Context(), db, 17); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := migrate(ctx, db, SchemaVersion); err == nil {
		t.Fatal("expected cancellation")
	}
	if integer(t, db, "PRAGMA user_version") != 17 {
		t.Fatal("cancelled migration advanced version")
	}
}

func TestSnippetMigrationMatchesLegacyCases(t *testing.T) {
	for _, test := range []struct{ text, html, want string }{
		{"Plain text content", "<p>HTML content</p>", "Plain text content"},
		{"", "<STYLE>body { margin: 0; }</STYLE><p>中文 HTML &amp;lt; C</p><script>danger()</script>", "中文 HTML < C"},
		{"<style>css</style><p>Actual content</p>", "", "Actual content"},
		{"<script>never closed", "<p>fallback</p>", "fallback"},
		{strings.Repeat("中", 201), "", strings.Repeat("中", 200) + "..."},
	} {
		if got := buildSnippet(test.text, test.html); got != test.want {
			t.Fatalf("snippet = %q, want %q", got, test.want)
		}
	}
	// Exercise V13 with data, not just the helper or an empty fresh schema.
	db := openRaw(t)
	if err := migrate(t.Context(), db, 12); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `INSERT INTO accounts(id,email,provider,created_at,updated_at) VALUES('a','a','gmail',1,1);
INSERT INTO messages(id,account_id,remote_id,body_text,date,created_at,updated_at) VALUES('m','a','r','<style>leaked css</style><p>clean preview</p>',1,1,1);`)
	if err := migrate(t.Context(), db, 13); err != nil {
		t.Fatal(err)
	}
	var snippet string
	if err := db.QueryRow("SELECT snippet FROM messages WHERE id='m'").Scan(&snippet); err != nil || snippet != "clean preview" {
		t.Fatalf("migrated snippet = %q, %v", snippet, err)
	}
}
