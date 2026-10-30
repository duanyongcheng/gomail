package desktop

import (
	"reflect"
	"sync"
	"testing"

	"github.com/egoist/mygo"
)

type recordingWindow struct {
	calls     []string
	minimized bool
}

func (w *recordingWindow) Hide()             { w.calls = append(w.calls, "hide") }
func (w *recordingWindow) Show()             { w.calls = append(w.calls, "show") }
func (w *recordingWindow) Focus()            { w.calls = append(w.calls, "focus") }
func (w *recordingWindow) IsMinimized() bool { return w.minimized }
func (w *recordingWindow) Restore()          { w.calls = append(w.calls, "restore"); w.minimized = false }

func TestLifecycleClosePreferenceUsesNativeActions(t *testing.T) {
	quits := 0
	l := newLifecycle(func(fn func()) { fn() }, func() { quits++ })
	w := &recordingWindow{}
	if !l.attach(w) {
		t.Fatal("first window rejected")
	}
	closeWindow := func() *mygo.CloseEvent {
		e := &mygo.CloseEvent{}
		l.OnClose(e)
		return e
	}
	// No hydrated preference: safe default retains the page and compose state.
	if !closeWindow().DefaultPrevented() || quits != 0 || !reflect.DeepEqual(w.calls, []string{"hide"}) {
		t.Fatalf("startup close: quit=%d calls=%v", quits, w.calls)
	}
	l.SetClosePreference(false)
	l.SetClosePreference(true)
	if !closeWindow().DefaultPrevented() || quits != 0 {
		t.Fatal("background preference did not hide")
	}
	l.SetClosePreference(false)
	if !closeWindow().DefaultPrevented() || quits != 1 {
		t.Fatal("quit-on-close did not defer to App.Quit")
	}
	l.BeforeQuit()
	if closeWindow().DefaultPrevented() || quits != 1 {
		t.Fatal("actual shutdown was intercepted or quit duplicated")
	}
	l.SetClosePreference(true)
	l.Restore()
	if !reflect.DeepEqual(w.calls, []string{"hide", "hide"}) {
		t.Fatalf("shutdown touched native window: %v", w.calls)
	}
}

func TestLifecycleEarlyRestoreRetainsOnlyOriginalWindow(t *testing.T) {
	l := newLifecycle(func(fn func()) { fn() }, func() { t.Fatal("restore quit") })
	l.Restore()
	l.Restore() // activation and second instance before profile initialization
	w := &recordingWindow{minimized: true}
	if !l.attach(w) {
		t.Fatal("attachment failed")
	}
	if !reflect.DeepEqual(w.calls, []string{"restore", "show", "focus"}) {
		t.Fatalf("pending restore: %v", w.calls)
	}
	other := &recordingWindow{}
	if l.attach(other) {
		t.Fatal("replaced the existing window")
	}
	w.calls = nil
	l.Restore()
	if !reflect.DeepEqual(w.calls, []string{"show", "focus"}) || len(other.calls) != 0 {
		t.Fatalf("restore changed identity or unmaximized window: %v / %v", w.calls, other.calls)
	}
}

func TestLifecycleShutdownBeforeReady(t *testing.T) {
	l := newLifecycle(func(fn func()) { fn() }, func() { t.Fatal("unexpected quit") })
	l.Restore()
	l.BeforeQuit()
	l.Restore()
	w := &recordingWindow{}
	if l.attach(w) || len(w.calls) != 0 || l.restorePending {
		t.Fatal("shutdown recreated/restored a window")
	}
	e := &mygo.CloseEvent{}
	l.OnClose(e)
	if e.DefaultPrevented() {
		t.Fatal("shutdown close blocked")
	}
}

func TestClosePreferenceRequiresHydratedProfile(t *testing.T) {
	quits := 0
	l := newLifecycle(func(fn func()) { fn() }, func() { quits++ })
	w := &recordingWindow{}
	l.attach(w)
	s := &AppService{Lifecycle: l, Namespace: "synthetic-profile"}
	for _, namespace := range []string{"", "another-profile"} {
		if _, err := s.ClosePreference(ClosePreferenceRequest{ProfileNamespace: namespace}); err == nil {
			t.Fatal("unhydrated preference accepted")
		}
	}
	l.OnClose(&mygo.CloseEvent{})
	if quits != 0 || !reflect.DeepEqual(w.calls, []string{"hide"}) {
		t.Fatal("invalid preference changed safe default")
	}
	if _, err := s.ClosePreference(ClosePreferenceRequest{ProfileNamespace: s.Namespace}); err != nil {
		t.Fatal(err)
	}
	l.OnClose(&mygo.CloseEvent{})
	if quits != 1 {
		t.Fatal("typed service preference did not affect native controller")
	}
}

func TestLifecycleConcurrentServiceCallsDispatchStateAndNativeActions(t *testing.T) {
	// A serial dispatcher models MyGO's UI-thread ownership. The race test
	// catches any access moved accidentally outside the dispatch boundary.
	var ui sync.Mutex
	l := newLifecycle(func(fn func()) { ui.Lock(); defer ui.Unlock(); fn() }, func() {})
	w := &recordingWindow{}
	l.attach(w)
	var calls sync.WaitGroup
	for range 20 {
		calls.Go(func() { l.SetClosePreference(true) })
		calls.Go(func() { l.Restore() })
		calls.Go(func() { l.OnClose(&mygo.CloseEvent{}) })
	}
	calls.Wait()
	l.BeforeQuit()
	before := len(w.calls)
	l.Restore()
	l.OnClose(&mygo.CloseEvent{})
	if len(w.calls) != before {
		t.Fatal("native action after shutdown")
	}
}
