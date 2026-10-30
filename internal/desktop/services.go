package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/bary/pebble-go/internal/app"
	"github.com/bary/pebble-go/internal/crypto"
	"github.com/egoist/mygo"
)

type Keyring struct{}

func (Keyring) Get(key string) ([]byte, error) {
	value, err := mygo.Secrets.Service(crypto.KeyringService).Get(key)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, crypto.ErrKeyNotFound
	}
	return value, err
}
func (Keyring) Set(key string, value []byte) error {
	return mygo.Secrets.Service(crypto.KeyringService).Set(key, value)
}

type WindowRequest struct {
	Action string `json:"action"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
}
type Geometry struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	X      int `json:"x"`
	Y      int `json:"y"`
}
type Namespace struct {
	Value string `json:"value"`
}
type AppService struct {
	Window    atomic.Pointer[mygo.Window]
	Namespace string
	Lifecycle *Lifecycle
	// OpenURL is injected by the desktop entry point; services never log URLs.
	OpenURL func(string) error
}

func (s *AppService) Profile(app.Empty) Namespace { return Namespace{Value: s.Namespace} }
func (s *AppService) Quit(app.Empty) app.Empty    { mygo.App.Quit(); return app.Empty{} }
func (s *AppService) Geometry(app.Empty) Geometry {
	w := s.Window.Load()
	if w == nil {
		return Geometry{}
	}
	size := w.ContentBounds()
	position := w.Bounds()
	return Geometry{Width: size.Width, Height: size.Height, X: position.X, Y: position.Y}
}
func (s *AppService) WindowAction(a WindowRequest) app.Empty {
	w := s.Window.Load()
	if w == nil {
		return app.Empty{}
	}
	switch a.Action {
	case "show":
		w.Show()
	case "hide":
		w.Hide()
	case "focus":
		w.Focus()
	case "close":
		w.Close()
	case "restore":
		if s.Lifecycle != nil {
			s.Lifecycle.Restore()
		}
	case "resize":
		w.SetContentSize(a.Width, a.Height)
	case "move":
		w.SetPosition(a.X, a.Y)
	}
	return app.Empty{}
}

// RotatingLog retains at most three 2 MiB logs in this profile. Callers log
// operation names/status only; credential and message payloads are not logged.
type RotatingLog struct {
	mu   sync.Mutex
	path string
	file *os.File
	size int64
}

func OpenLog(profile string) (*RotatingLog, error) {
	directory := filepath.Join(profile, "logs")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	w := &RotatingLog{path: filepath.Join(directory, "pebble-go.log")}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}
func (w *RotatingLog) open() error {
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.size = info.Size()
	return nil
}
func (w *RotatingLog) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(data)) > 2<<20 {
		if err := w.file.Close(); err != nil {
			return 0, err
		}
		_ = os.Remove(w.path + ".2")
		if err := os.Rename(w.path+".1", w.path+".2"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
		if err := os.Rename(w.path, w.path+".1"); err != nil {
			return 0, err
		}
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(data)
	w.size += int64(n)
	return n, err
}
func (w *RotatingLog) Close() error { w.mu.Lock(); defer w.mu.Unlock(); return w.file.Close() }
