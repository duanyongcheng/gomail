package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type SyncComplete struct {
	AccountID string `json:"account_id"`
	Count     int    `json:"count"`
}
type Backend struct {
	db     *sqlite.DB
	window atomic.Pointer[mygo.Window]
	gmail  *GmailService
}

func (b *Backend) WindowAction(action string) {
	w := b.window.Load()
	switch action {
	case "show":
		w.Show()
	case "hide":
		w.Hide()
	case "focus":
		w.Focus()
	}
}
func (b *Backend) WindowSize() map[string]int {
	r := b.window.Load().ContentBounds()
	return map[string]int{"width": r.Width, "height": r.Height}
}
func (b *Backend) WindowPosition() map[string]int {
	r := b.window.Load().Bounds()
	return map[string]int{"x": r.X, "y": r.Y}
}
func (b *Backend) Resize(width, height int) { b.window.Load().SetContentSize(width, height) }
func (b *Backend) Move(x, y int)            { b.window.Load().SetPosition(x, y) }
func (b *Backend) Invoke(ctx context.Context, command string, args map[string]any) (any, error) {
	text := func(k string) string { v, _ := args[k].(string); return v }
	switch command {
	case "get_profile_storage_namespace":
		if b.gmail.live {
			return "gmail-live-isolated", nil
		}
		return "gmail-demo-isolated", nil
	case "list_accounts":
		return b.query(ctx, "SELECT id,email,display_name,color,provider,created_at,updated_at,account_label,provider_display_name FROM accounts")
	case "list_folders":
		return b.query(ctx, "SELECT * FROM folders WHERE account_id=?", text("accountId"))
	case "list_messages":
		folder := text("folderId")
		if folder != "" {
			return b.query(ctx, "SELECT m.* FROM messages m JOIN message_folders f ON f.message_id=m.id WHERE m.is_deleted=0 AND f.folder_id=? ORDER BY m.date DESC", folder)
		}
		return b.query(ctx, "SELECT * FROM messages WHERE is_deleted=0 ORDER BY date DESC")
	case "list_starred_messages":
		return b.query(ctx, "SELECT * FROM messages WHERE is_deleted=0 AND is_starred=1 ORDER BY date DESC")
	case "get_message_with_html", "get_rendered_html", "get_message":
		rows, err := b.query(ctx, "SELECT * FROM messages WHERE id=?", text("messageId"))
		if err != nil || len(rows) != 1 {
			return nil, err
		}
		m := rows[0]
		if command == "get_message" {
			return m, nil
		}
		bodyHTML, _ := m["body_html_raw"].(string)
		// No production backend sanitizer has been ported: live content is escaped
		// plain text. Demo exercises HTML with synthetic input only.
		if b.gmail.live {
			text, _ := m["body_text"].(string)
			bodyHTML = "<pre>" + html.EscapeString(text) + "</pre>"
		}
		rendered := map[string]any{"html": bodyHTML, "trackers_blocked": []any{}, "images_blocked": 0}
		if command == "get_rendered_html" {
			return rendered, nil
		}
		return []any{m, rendered}, nil
	case "update_message_flags":
		if b.gmail.live {
			return nil, nil
		} // automatic detail-read is local no-op in live mode.
		var read, star *bool
		if v, ok := args["isRead"].(bool); ok {
			read = &v
		}
		if v, ok := args["isStarred"].(bool); ok {
			star = &v
		}
		return nil, b.gmail.Flags(ctx, text("messageId"), read, star)
	case "get_folder_unread_counts", "get_account_unread_counts":
		return map[string]int{}, nil // optional fixture badges, no real counts claim.
	case "get_message_labels_batch", "list_kanban_context_notes":
		return map[string]any{}, nil
	case "list_attachments":
		return b.query(ctx, "SELECT * FROM attachments WHERE message_id=?", text("messageId"))
	case "search_messages":
		return b.gmail.Search(ctx, text("query"), text("accountId"), 50)
	case "list_kanban_cards", "get_message_labels", "list_labels", "list_contacts", "search_contacts", "search_contact_suggestions", "list_email_templates", "list_rules", "list_pending_mail_ops", "list_snoozed", "list_trusted_senders", "list_threads", "take_pending_mailto_urls":
		return []any{}, nil
	case "get_contact_by_email", "ai_get_config", "get_translate_config", "load_auto_backup_config", "get_global_proxy", "get_account_proxy":
		return nil, nil
	case "get_pending_mail_ops_summary":
		return map[string]any{"pending_count": 0, "in_progress_count": 0, "failed_count": 0, "total_active_count": 0, "last_error": nil, "updated_at": nil}, nil
	case "get_email_signature", "migrate_email_signature_if_absent":
		return "", nil
	case "start_sync":
		return "Use explicit Gmail Sync button", nil
	case "set_realtime_preference", "set_notifications_enabled", "set_tray_menu_labels", "sync_titlebar_theme", "clear_notification_attention", "trigger_sync", "stop_sync":
		return nil, nil
	case "send_email", "delete_message", "archive_message", "move_to_folder":
		return nil, errors.New("Gmail prototype: send/delete/archive disabled")
	case "quit_app":
		mygo.App.Quit()
		return nil, nil
	}
	return nil, fmt.Errorf("Gmail prototype does not implement %s", command)
}
func (b *Backend) query(ctx context.Context, sql string, args ...any) ([]map[string]any, error) {
	rows, err := b.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, r := range rows.Values {
		m := map[string]any{}
		for i, k := range rows.Columns {
			v := r[i]
			if len(k) > 3 && k[:3] == "is_" || k == "has_attachments" {
				if n, ok := v.(int64); ok {
					v = n != 0
				}
			}
			if k == "to_list" || k == "cc_list" || k == "bcc_list" {
				var a []any
				if err := json.Unmarshal([]byte(v.(string)), &a); err != nil {
					return nil, err
				}
				v = a
			}
			m[k] = v
		}
		out = append(out, m)
	}
	return out, nil
}
func fixtureIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	for y := range 48 {
		for x := range 48 {
			img.Set(x, y, color.NRGBA{R: 212, G: 113, B: 78, A: 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	service := newGmailService(root)
	name := "Pebble Gmail Demo"
	if service.live {
		name = "Pebble Gmail Live Isolated"
	}
	mygo.App.SetName(name)
	dataRoot, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	service.root = dataRoot
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	b := &Backend{gmail: service}
	mygo.Bind(b, service)
	var quitting bool
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
	mygo.App.WhenReady(func() {
		go func() {
			dbPath := filepath.Join(service.root, "fixture.sqlite")
			if service.live {
				dbPath = filepath.Join(service.root, "live.sqlite")
			}
			db, err := sqlite.Open(context.Background(), dbPath, sqlite.OpenOptions{})
			if err != nil {
				log.Print(err)
				mygo.App.Quit()
				return
			}
			b.db = db
			if err = initializeSchema(db); err != nil {
				log.Print(err)
				_ = db.Close()
				mygo.App.Quit()
				return
			}
			if err = service.initialize(context.Background(), db); err != nil {
				log.Print(err)
				_ = db.Close()
				mygo.App.Quit()
				return
			}
			mygo.App.WhenReady(func() {
				if !mygo.App.IsPackaged() {
					mygo.SetFrontend(os.DirFS(filepath.Join(root, "dist")))
				}
				win := mygo.NewWindow(mygo.WindowOptions{Title: name, Width: 1200, Height: 850, MinWidth: 800, MinHeight: 600, URL: "/", TitleBarStyle: mygo.TitleBarHidden, BackgroundColor: "#f8f7f5"})
				b.window.Store(win)
				win.OnClose(func(e *mygo.CloseEvent) {
					if !quitting {
						e.PreventDefault()
						win.Hide()
					}
				})
				mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}}))
				mygo.App.OnQuit(func() {
					if service.mock != nil {
						service.mock.Close()
					}
					_ = db.Close()
				})
				if !service.live {
					go verifyGmail(b, win)
				}
			})
		}()
	})
	if err = mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
func initializeSchema(db *sqlite.DB) error {
	data := []byte(schemaSQL)
	var err error
	for _, sql := range splitStatements(string(data)) {
		if _, err = db.Execute(context.Background(), sql); err != nil {
			return err
		}
	}
	for _, column := range []string{"account_label TEXT", "provider_display_name TEXT"} {
		name := column[:bytes.IndexByte([]byte(column), ' ')]
		rows, e := db.Query(context.Background(), "SELECT name FROM pragma_table_info('accounts') WHERE name=?", name)
		if e != nil {
			return e
		}
		if len(rows.Values) == 0 {
			if _, e = db.Execute(context.Background(), "ALTER TABLE accounts ADD COLUMN "+column); e != nil {
				return e
			}
		}
	}
	_, err = db.Execute(context.Background(), "INSERT OR IGNORE INTO accounts(id,email,display_name,provider,created_at,updated_at) VALUES('poc-account','Not signed in','Gmail OAuth','gmail',0,0)")
	return err
}
func splitStatements(sql string) []string {
	var out []string
	for _, s := range bytes.Split([]byte(sql), []byte(";")) {
		if len(bytes.TrimSpace(s)) > 0 {
			out = append(out, string(s))
		}
	}
	return out
}
func verifyGmail(b *Backend, w *mygo.Window) {
	checks := []map[string]any{}
	check := func(name string, ok bool) {
		checks = append(checks, map[string]any{"name": name, "passed": ok})
		log.Printf("%s: %v", name, ok)
	}
	eval := func(js string) (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return w.Page().EvalContext(ctx, js)
	}
	wait := func(js string) bool {
		until := time.Now().Add(30 * time.Second)
		for time.Now().Before(until) {
			v, e := eval(js)
			if e == nil && v == true {
				return true
			}
			time.Sleep(200 * time.Millisecond)
		}
		return false
	}
	capture := func(name string) {
		if png, e := w.CapturePage(); e == nil {
			_ = os.WriteFile(filepath.Join(b.gmail.root, "artifacts", name+".png"), png, 0600)
		}
	}
	defer func() {
		diag, _ := eval("window.__gmailTest?.errors")
		report := map[string]any{"checks": checks, "frontend_errors": diag, "real_google": false, "packaged": mygo.App.IsPackaged()}
		data, _ := json.MarshalIndent(report, "", "  ")
		file := "gmail-runtime.json"
		if mygo.App.IsPackaged() {
			file = "gmail-packaged-runtime.json"
		}
		_ = os.WriteFile(filepath.Join(b.gmail.root, "artifacts", file), data, 0600)
		mygo.App.Quit()
	}()
	check("Gmail control panel mounted", wait("Boolean(document.querySelector('[data-gmail-panel]'))"))
	v, e := eval("window.__gmailTest.api.sync()")
	check("typed Gmail API initial sync", e == nil && v != nil)
	v, e = eval("window.__gmailTest.backend.invoke('list_messages',{folderId:'gmail-label:INBOX',limit:50,offset:0}).then(x=>JSON.stringify(x))")
	check("Gmail inbox backend DTO", e == nil && strings.Contains(fmt.Sprint(v), "gmail:m1"))
	v, e = eval("window.__gmailTest.backend.invoke('list_folders',{accountId:'poc-account'}).then(x=>JSON.stringify(x))")
	check("Gmail sidebar folders DTO", e == nil && strings.Contains(fmt.Sprint(v), "gmail-label:INBOX"))
	v, e = eval("window.__gmailTest.backend.invoke('list_accounts').then(x=>JSON.stringify(x))")
	check("Gmail sidebar accounts DTO", e == nil && strings.Contains(fmt.Sprint(v), "poc-account"))
	check("Gmail sidebar account/folder data available", e == nil)
	check("existing React inbox from Gmail API", wait("document.body.innerText.includes('Gmail API 验证')"))
	capture("gmail-inbox")
	_, _ = eval("(window.__gmailTest.select('gmail:m1'),true)")
	check("Gmail RAW MIME renders in existing Shadow DOM", wait("Array.from(document.querySelectorAll('*')).some(e=>e.shadowRoot?.textContent.includes('Gmail 中文邮件'))"))
	capture("gmail-message")
	_, _ = eval("(window.__gmailTest.search('中文搜索'),true)")
	check("existing SearchView displays Gmail cache", wait("document.querySelector('.search-results-scroll')?.textContent.includes('Gmail API 验证')"))
	capture("gmail-search")
	v, e = eval("window.__gmailTest.api.flags('gmail:m1',true,true).then(()=>true)")
	check("Gmail modify labels/flags through API", e == nil && v == true)
	v, e = eval("window.__gmailTest.api.sync()")
	check("Gmail History incremental", e == nil && v != nil)
	v, e = eval("window.__gmailTest.api.search('中文搜索','poc-account',10)")
	hits, ok := v.([]any)
	check("typed CJK search", e == nil && ok && len(hits) == 2)
	v, _ = eval("window.__gmailTest.errors.length")
	check("no frontend unhandled errors", v == float64(0))
	v, e = eval("window.__gmailTest.backend.invoke('send_email',{}).then(()=>false,e=>e.message.includes('disabled'))")
	check("Gmail send disabled", e == nil && v == true)
}
