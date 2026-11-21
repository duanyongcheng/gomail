package desktop

import (
	"errors"

	"github.com/duanyongcheng/pebble-go/internal/app"
	"github.com/egoist/mygo"
)

// Lifecycle retains one window for the application's lifetime. All state and
// native actions are serialized on the UI thread, including bound service calls
// (which MyGO runs on goroutines). It never creates windows or starts workers.
type Lifecycle struct {
	dispatch        func(func())
	quit            func()
	window          lifecycleWindow
	preferenceReady bool
	keepRunning     bool
	restorePending  bool
	shuttingDown    bool
}

type lifecycleWindow interface {
	Hide()
	Show()
	Restore()
	Focus()
	IsMinimized() bool
}

func NewLifecycle() *Lifecycle {
	return newLifecycle(mygo.RunOnMain, mygo.App.Quit)
}

func newLifecycle(dispatch func(func()), quit func()) *Lifecycle {
	return &Lifecycle{dispatch: dispatch, quit: quit, keepRunning: true}
}

// Attach accepts only the original window. A restore requested during profile
// startup is applied once it exists; attachment after shutdown is refused.
func (l *Lifecycle) Attach(window *mygo.Window) bool {
	if window == nil {
		return false
	}
	return l.attach(window)
}

func (l *Lifecycle) attach(window lifecycleWindow) bool {
	attached := false
	l.dispatch(func() {
		if l.shuttingDown || l.window != nil || window == nil {
			return
		}
		l.window = window
		attached = true
		if l.restorePending {
			l.restorePending = false
			l.restore()
		}
	})
	return attached
}

func (l *Lifecycle) SetClosePreference(keepRunning bool) {
	l.dispatch(func() {
		if !l.shuttingDown {
			l.keepRunning = keepRunning
			l.preferenceReady = true
		}
	})
}

// ClosePreferenceState reports the applied native preference, including whether
// profile hydration has reached the service. Used by packaged regression checks.
func (l *Lifecycle) ClosePreferenceState() (ready, keepRunning bool) {
	l.dispatch(func() { ready, keepRunning = l.preferenceReady, l.keepRunning })
	return
}

// OnClose is shared by the actual native event and packaged regression checks.
// MyGO App.Quit posts a later quit sequence, so the first close is prevented;
// the ensuing OnBeforeQuit/OnClose sequence is allowed to destroy the window.
func (l *Lifecycle) OnClose(event *mygo.CloseEvent) {
	l.dispatch(func() {
		if l.shuttingDown {
			return
		}
		event.PreventDefault()
		if !l.preferenceReady || l.keepRunning {
			if l.window != nil {
				l.window.Hide()
			}
			return
		}
		l.shuttingDown = true
		l.restorePending = false
		l.quit()
	})
}

// BeforeQuit also handles explicit Quit/menu/OS requests, regardless of the
// close preference. Main owns cancellation and joining of application workers.
func (l *Lifecycle) BeforeQuit() {
	l.dispatch(func() {
		l.shuttingDown = true
		l.restorePending = false
	})
}

// Restore is used by both activation and second-instance callbacks. The latter
// runs inline on MyGO's main thread to retain its platform activation token.
func (l *Lifecycle) Restore() {
	l.dispatch(func() {
		if l.shuttingDown {
			return
		}
		if l.window == nil {
			l.restorePending = true
			return
		}
		l.restore()
	})
}

func (l *Lifecycle) restore() {
	if l.window.IsMinimized() {
		l.window.Restore()
	}
	l.window.Show()
	l.window.Focus()
}

type ClosePreferenceRequest struct {
	ProfileNamespace        string `json:"profileNamespace"`
	KeepRunningInBackground bool   `json:"keepRunningInBackground"`
}

func (s *AppService) ClosePreference(request ClosePreferenceRequest) (app.Empty, error) {
	if s.Lifecycle == nil || s.Namespace == "" || request.ProfileNamespace != s.Namespace {
		return app.Empty{}, errors.New("window preference is unavailable until the profile is ready")
	}
	s.Lifecycle.SetClosePreference(request.KeepRunningInBackground)
	return app.Empty{}, nil
}
