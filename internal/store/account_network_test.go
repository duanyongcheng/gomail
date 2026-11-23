package store

import (
	"github.com/duanyongcheng/gomail/internal/mailnet"
	"testing"
)

func TestAccountNetworkPreservesAuthCheckpointAndIsolation(t *testing.T) {
	db := openStore(t)
	execute(t, db.write, `INSERT INTO accounts(id,email,provider,created_at,updated_at,auth_data,sync_state) VALUES('a','a','gmail',1,1,x'0102','{"gmail_checkpoint":{"phase":"history"},"other":7}'),('b','b','gmail',1,1,x'0304',NULL),('c','c','imap',1,1,NULL,NULL)`)
	setting := mailnet.ProxySetting{Mode: "custom", Proxy: &mailnet.Proxy{Scheme: "socks5", Host: "localhost", Port: 1080}}
	if err := db.SetAccountNetwork(t.Context(), "a", setting); err != nil {
		t.Fatal(err)
	}
	got, err := db.AccountNetwork(t.Context(), "a")
	if err != nil || got.Proxy == nil || got.Proxy.Port != 1080 {
		t.Fatal(got, err)
	}
	other, err := db.AccountNetwork(t.Context(), "b")
	if err != nil || other.Mode != "inherit" || other.Proxy != nil {
		t.Fatal(other, err)
	}
	var phase, auth string
	var untouched int
	if err := db.read.QueryRow(`SELECT json_extract(sync_state,'$.gmail_checkpoint.phase'),json_extract(sync_state,'$.other'),hex(auth_data) FROM accounts WHERE id='a'`).Scan(&phase, &untouched, &auth); err != nil || phase != "history" || untouched != 7 || auth != "0102" {
		t.Fatal(phase, untouched, auth, err)
	}
	if err := db.SetAccountNetwork(t.Context(), "c", setting); err == nil {
		t.Fatal("protocol plaintext proxy bypass accepted")
	}
	if err := db.SetAccountNetwork(t.Context(), "missing", setting); err == nil {
		t.Fatal("deleted account recreated")
	}
	if err := db.SetAccountNetwork(t.Context(), "a", mailnet.ProxySetting{Mode: "disabled", Proxy: setting.Proxy}); err == nil {
		t.Fatal("ambiguous direct policy accepted")
	}
}
