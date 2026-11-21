package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/duanyongcheng/pebble-go/internal/desktop"
	"github.com/egoist/mygo"
)

const selftestLinkURL = "https://example.invalid/pebble-link-selftest"
const selftestFailedLinkURL = "https://example.invalid/pebble-link-failure?private=selftest"

// Installed before the WebView is created. The fixture never launches a browser
// or makes a request; even an unexpected URL fails locally without being logged.
var selftestLinks struct {
	sync.Mutex
	calls int
	valid bool
}

func configureSelftestDesktop(service *desktop.AppService) {
	selftestLinks.Lock()
	selftestLinks.calls, selftestLinks.valid = 0, true
	selftestLinks.Unlock()
	service.OpenURL = func(raw string) error {
		selftestLinks.Lock()
		defer selftestLinks.Unlock()
		selftestLinks.calls++
		switch raw {
		case selftestLinkURL:
			return nil
		case selftestFailedLinkURL:
			return errors.New("synthetic browser failure")
		default:
			selftestLinks.valid = false
			return errors.New("unexpected synthetic link")
		}
	}
}

func selftestLinkCalls(want int) bool {
	selftestLinks.Lock()
	defer selftestLinks.Unlock()
	return selftestLinks.valid && selftestLinks.calls == want
}

func selftestExternalLinks(ctx context.Context, window *mygo.Window) bool {
	click := func(text string) bool {
		return waitSelftestPage(ctx, window, `(() => {
			const hosts = Array.from(document.querySelectorAll('[data-pebble-message-theme]'));
			const anchor = hosts.flatMap(h => Array.from(h.shadowRoot?.querySelectorAll('a') || []))
				.find(a => a.textContent === '`+text+`');
			if (!anchor) return false;
			anchor.click(); return true;
		})()`, 5*time.Second)
	}
	if !click("Safe link selftest") {
		return false
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for !selftestLinkCalls(1) {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
	if !click("Failed link selftest") {
		return false
	}
	// Check the visible error, not a resolving invoke mock or console message.
	visibleError := waitSelftestPage(ctx, window, `(() => {
		const text = document.body.innerText;
		return /could not open|unable to open|failed to open|无法打开|打开.*失败/i.test(text)
			&& !text.includes('private=selftest') && !text.includes('synthetic browser failure');
	})()`, 5*time.Second)
	return visibleError && selftestLinkCalls(2)
}

// Drive the statusbar preference, then wait for the typed service to acknowledge
// it. A frontend-only localStorage assertion would miss a disconnected adapter.
func selftestClosePreference(ctx context.Context, window *mygo.Window, keep bool) bool {
	pressed := "false"
	if keep {
		pressed = "true"
	}
	if !waitSelftestPage(ctx, window, `(() => {
		const b = document.querySelector('footer button[aria-pressed]');
		if (!b || b.disabled) return false;
		if (b.getAttribute('aria-pressed') !== '`+pressed+`') b.click();
		return true;
	})()`, 5*time.Second) {
		return false
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, actual := desktopAppService.Lifecycle.ClosePreferenceState()
		if ready && actual == keep {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
		}
	}
}

func selftestWindowRestore(ctx context.Context, window *mygo.Window) (bool, bool) {
	if !selftestClosePreference(ctx, window, true) {
		return false, false
	}
	const state = `document.getElementById('compose-subject')?.value === 'Edited local draft selftest'
		&& document.body.innerText.includes('draft-selftest.txt')
		&& window.__pebbleSelftestWindowIdentity === 'original-page'`
	if !waitSelftestPage(ctx, window, `(() => { window.__pebbleSelftestWindowIdentity = 'original-page'; return true; })()`, time.Second) {
		return false, false
	}
	window.Close()
	if window.IsDestroyed() || window.IsVisible() || ctx.Err() != nil {
		return false, false
	}
	// Exercise the same restore path used by native activation.
	desktopAppService.Lifecycle.Restore()
	restored := window.IsVisible() && !window.IsDestroyed() &&
		waitSelftestPage(ctx, window, state, 5*time.Second)
	if !restored {
		return false, false
	}
	window.Close()
	if window.IsVisible() || window.IsDestroyed() {
		return restored, false
	}
	executable, err := os.Executable()
	if err != nil {
		desktopAppService.Lifecycle.Restore()
		return restored, false
	}
	// The test-only application name isolates this lock from a normal user app.
	// The second process must exit before opening the disposable profile again.
	childCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	command := exec.CommandContext(childCtx, executable)
	command.Env = os.Environ()
	err = command.Run()
	second := err == nil && waitSelftestPage(ctx, window, state, 5*time.Second) &&
		window.IsVisible() && !window.IsDestroyed() &&
		desktopAppService.Window.Load() == window && len(mygo.Windows()) == 1
	if !second {
		desktopAppService.Lifecycle.Restore()
	}
	return restored, second
}

// Must be the last runtime check: exercise the real close → App.Quit sequence.
// The caller records results without dispatching UI work after cancellation.
func selftestQuitOnClose(ctx context.Context, window *mygo.Window) bool {
	if !selftestClosePreference(ctx, window, false) {
		return false
	}
	window.Close()
	select {
	case <-ctx.Done():
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

func selftestUnavailableSettings(ctx context.Context, window *mygo.Window) bool {
	return waitSelftestPage(ctx, window, `(() => {
		if (!document.getElementById('settings-tab-accounts')) return false;
		const proxy = document.getElementById('settings-tab-proxy');
		if (!proxy || proxy.disabled || proxy.getAttribute('aria-disabled') === 'true') return false;
		const unsupported = ['rules','translation','ai','cloudSync'];
		return unsupported.every(id => {
			const b = document.getElementById('settings-tab-' + id);
			return !b || b.disabled || b.getAttribute('aria-disabled') === 'true';
		});
	})()`, 5*time.Second)
}
