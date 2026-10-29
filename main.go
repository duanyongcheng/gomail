package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/bary/pebble-go/internal/app"
	"github.com/bary/pebble-go/internal/desktop"
	"github.com/bary/pebble-go/internal/domain"
	"github.com/bary/pebble-go/internal/profile"
	"github.com/bary/pebble-go/internal/store"
	"github.com/egoist/mygo"
)

var syncComplete = mygo.NewEvent[app.SyncComplete]("mail:sync-complete")
var syncProgress = mygo.NewEvent[domain.SyncProgress]("mail:sync-progress")
var newMail = mygo.NewEvent[domain.NewMail]("mail:new")
var realtimeStatus = mygo.NewEvent[app.RealtimeStatus]("mail:realtime-status")
var pendingOpsChanged = mygo.NewEvent[struct{}]("mail:pending-ops-changed")
var attachmentProgress = mygo.NewEvent[app.AttachmentProgress]("attachment:download-progress")
var outboxChanged = mygo.NewEvent[struct{}]("mail:outbox-changed")

func main() {
	mygo.App.SetName("Pebble Go")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := app.New(ctx, nil, desktop.Keyring{}, mygo.Shell.OpenExternal)
	runtime.OnSync = func(event app.SyncComplete) { _ = syncComplete.Broadcast(event) }
	runtime.OnProgress = func(event domain.SyncProgress) { _ = syncProgress.Broadcast(event) }
	runtime.OnNewMail = func(event domain.NewMail) { _ = newMail.Broadcast(event) }
	runtime.OnRealtime = func(event app.RealtimeStatus) { _ = realtimeStatus.Broadcast(event) }
	runtime.OnPendingOpsChanged = func() { _ = pendingOpsChanged.Broadcast(struct{}{}) }
	runtime.OnAttachmentProgress = func(event app.AttachmentProgress) { _ = attachmentProgress.Broadcast(event) }
	runtime.OnOutboxChanged = func() { _ = outboxChanged.Broadcast(struct{}{}) }
	windowService := &desktop.AppService{}
	fileService := &desktop.FileService{Runtime: runtime, App: windowService}
	mygo.Bind(&app.AccountService{Runtime: runtime}, &app.FolderService{Runtime: runtime}, &app.MessageService{Runtime: runtime}, &app.ThreadService{Runtime: runtime}, &app.SearchService{Runtime: runtime}, &app.SyncService{Runtime: runtime}, &app.PendingOpsService{Runtime: runtime}, &app.AttachmentService{Runtime: runtime}, &app.ComposeService{Runtime: runtime}, &app.OutboxService{Runtime: runtime}, fileService, windowService)
	var workers sync.WaitGroup
	var db *store.DB
	var logFile *desktop.RotatingLog
	var quitting bool
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true; cancel() })
	mygo.App.OnQuit(func() {
		cancel()
		workers.Wait()
		runtime.Scheduler.Close()
		if db != nil {
			_ = db.Close()
		}
		if logFile != nil {
			_ = logFile.Close()
		}
	})
	mygo.App.WhenReady(func() {
		if !mygo.App.RequestSingleInstanceLock() {
			mygo.App.Quit()
			return
		}
		fallback, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			slog.Error("cannot resolve application data directory")
			mygo.App.Quit()
			return
		}
		exe, err := os.Executable()
		if err != nil {
			slog.Error("cannot resolve executable path")
			mygo.App.Quit()
			return
		}
		root, err := profile.Resolve(filepath.Dir(exe), fallback, os.Getenv("PEBBLE_PROFILE_DIR"))
		if err != nil {
			slog.Error("cannot resolve profile directory")
			mygo.App.Quit()
			return
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			var err error
			windowService.Namespace, err = profile.Namespace(root)
			if err != nil {
				slog.Error("cannot initialize profile identity")
				mygo.App.Quit()
				return
			}
			logFile, err = desktop.OpenLog(root)
			if err != nil {
				slog.Error("cannot open profile log")
				mygo.App.Quit()
				return
			}
			slog.SetDefault(slog.New(slog.NewJSONHandler(logFile, nil)))
			db, err = store.Open(ctx, filepath.Join(root, "pebble.sqlite"))
			if err != nil {
				slog.Error("cannot open profile database")
				mygo.App.Quit()
				return
			}
			runtime.DB = db
			if err := desktop.RegisterAttachmentProtocol(db); err != nil {
				slog.Error("cannot register attachment protocol")
				mygo.App.Quit()
				return
			}
			if err := runtime.Restore(); err != nil {
				slog.Warn("saved credentials require attention; cached mail remains available")
			}
			workers.Add(1)
			go func() { defer workers.Done(); runtime.RunPendingOpsWorker(ctx) }()
			workers.Add(1)
			go func() { defer workers.Done(); runtime.RunOutboxWorker(ctx) }()
			if os.Getenv("PEBBLE_SELFTEST") == "1" {
				if err := prepareSelftest(runtime); err != nil {
					slog.Error("selftest fixture failed")
					mygo.App.Quit()
					return
				}
			}
			mygo.App.WhenReady(func() {
				if ctx.Err() != nil {
					return
				}
				if !mygo.App.IsPackaged() {
					mygo.SetFrontend(os.DirFS("dist"))
				}
				window := mygo.NewWindow(mygo.WindowOptions{Title: "Pebble Go", Width: 1200, Height: 850, MinWidth: 800, MinHeight: 600, URL: "/", TitleBarStyle: mygo.TitleBarHidden, BackgroundColor: "#f8f7f5"})
				windowService.Window.Store(window)
				window.OnFocus(func() { runtime.Scheduler.Foreground(true) })
				window.OnBlur(func() { runtime.Scheduler.Foreground(false) })
				window.OnHide(func() { runtime.Scheduler.Foreground(false) })
				window.OnClose(func(e *mygo.CloseEvent) {
					if !quitting {
						e.PreventDefault()
						window.Hide()
					}
				})
				mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}}))
				if os.Getenv("PEBBLE_SELFTEST") == "1" {
					workers.Add(1)
					go func() { defer workers.Done(); runSelftest(ctx, runtime, window) }()
				}
			})
		}()
	})
	if err := mygo.App.Run(); err != nil {
		slog.Error("desktop event loop failed")
		os.Exit(1)
	}
}
