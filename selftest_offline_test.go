package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/duanyongcheng/pebble-go/internal/store"
)

func TestOfflineSelftestDistinctExplicitChecks(t *testing.T) {
	want := []string{
		"offline browser state applied in real WebView",
		"offline list star persists locally while synthetic Gmail stalls",
		"offline uncached Starred view renders local message",
		"offline uncached basic Search submits and renders local hit",
		"offline Outbox renders existing unknown send",
		"offline Outbox local refresh completes while synthetic Gmail stalls",
		"offline unknown send remains unchanged without remote submission",
		"offline browser descriptor restored before native quit",
	}
	if !reflect.DeepEqual(offlineSelftestChecks, want) {
		t.Fatalf("runtime offline checks changed: %v", offlineSelftestChecks)
	}
	seen := map[string]bool{}
	for _, name := range reviewSelftestChecks {
		seen[name] = true
	}
	for _, name := range offlineSelftestChecks {
		if name == "" || seen[name] {
			t.Fatalf("duplicate/empty runtime check: %q", name)
		}
		seen[name] = true
	}
	if len(reviewSelftestChecks) != 7 {
		t.Fatal("offline checks must not change the seven review checks")
	}
}

// Check source wiring without running (or pretending to run) a native WebView.
func TestOfflineSelftestRegisteredAfterReviewBeforeQuit(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "selftest.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var run *ast.FuncDecl
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Name.Name == "runSelftest" {
			run = fn
		}
	}
	if run == nil {
		t.Fatal("missing runSelftest")
	}
	registered := false
	var calls []string
	ast.Inspect(run.Body, func(node ast.Node) bool {
		if loop, ok := node.(*ast.RangeStmt); ok {
			if id, ok := loop.X.(*ast.Ident); ok && id.Name == "offlineSelftestChecks" {
				registered = true
			}
		}
		if call, ok := node.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok {
				calls = append(calls, id.Name)
			}
		}
		return true
	})
	if !registered {
		t.Fatal("offline checks are not initialized in runtime.json map")
	}
	index := func(name string) int {
		for i, call := range calls {
			if call == name {
				return i
			}
		}
		return -1
	}
	cloud, review, offline, quit := index("selftestCloudLifecycle"), index("selftestReviewFixes"), index("selftestOffline"), index("selftestQuitOnClose")
	if cloud < 0 || review <= cloud || offline <= review || quit <= offline {
		t.Fatal("offline smoke must follow original lifecycle/review checks and precede native quit")
	}
}

func TestOfflineSelftestUnknownRecordOracle(t *testing.T) {
	before := store.OutboxRecord{ID: "synthetic-unknown", AccountID: offlineAccount, Status: "outcome_unknown",
		Data: []byte("synthetic encrypted payload"), Summary: []byte("synthetic encrypted summary"), Attempts: 1,
		LastError: "fixture unknown", CreatedAt: 1, UpdatedAt: 2}
	if !offlineUnknownUnchanged(before, before) {
		t.Fatal("unchanged protected synthetic row rejected")
	}
	for name, mutate := range map[string]func(*store.OutboxRecord){
		"status":          func(r *store.OutboxRecord) { r.Status = "sent" },
		"attempts":        func(r *store.OutboxRecord) { r.Attempts++ },
		"payload cleared": func(r *store.OutboxRecord) { r.Data = nil },
		"payload changed": func(r *store.OutboxRecord) { r.Data = []byte("replaced") },
		"summary cleared": func(r *store.OutboxRecord) { r.Summary = nil },
		"wrong account":   func(r *store.OutboxRecord) { r.AccountID = "other" },
		"remote receipt":  func(r *store.OutboxRecord) { r.RemoteID = "unexpected" },
		"retry enabled":   func(r *store.OutboxRecord) { r.Retryable = true },
	} {
		t.Run(name, func(t *testing.T) {
			after := before
			mutate(&after)
			if offlineUnknownUnchanged(before, after) {
				t.Fatal("oracle accepted changed unknown record")
			}
		})
	}
	for _, invalid := range []store.OutboxRecord{{}, {ID: "x", AccountID: offlineAccount, Status: "outcome_unknown"},
		{ID: "x", AccountID: "other", Status: "outcome_unknown", Data: before.Data, Summary: before.Summary}} {
		if offlineUnknownUnchanged(invalid, invalid) {
			t.Fatal("oracle accepted missing/unprotected fixture")
		}
	}
}

func TestOfflineSelftestRowSelectorIsExactAndReadOnly(t *testing.T) {
	script := offlineRowScript(".starred-list-scroll", "return navigator.onLine===false;")
	for _, want := range []string{strconv.Quote(".starred-list-scroll .message-list-row"), strconv.Quote(offlineSubject),
		".message-row-subject", "textContent.trim()===", "getClientRects().length", "navigator.onLine===false"} {
		if !strings.Contains(script, want) {
			t.Fatalf("row predicate missing %q", want)
		}
	}
	if strings.Contains(script, ".click()") || strings.Contains(script, ".focus()") {
		t.Fatal("polling a visible-row predicate must not perform actions")
	}
	if strings.Contains(offlineOutboxVisible, ".click()") || strings.Contains(offlineOutboxVisible, "refetch(") {
		t.Fatal("Outbox visibility polling must not retrigger refresh")
	}
}
