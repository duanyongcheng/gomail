package main

import (
	"context"
	"time"

	"github.com/bary/pebble-go/internal/app"
	"github.com/egoist/mygo"
)

// Hold only the synthetic server lock: Gmail cannot complete a request while
// the real WebView opens the message and commits its local read-state change.
func selftestOpenUnread(ctx context.Context, runtime *app.Runtime, window *mygo.Window, id, click, visibleRead string) (opened, read bool, elapsed time.Duration) {
	before, err := runtime.DB.Message(ctx, id)
	if err != nil || before.IsRead || selftestMailbox == nil {
		return false, false, 0
	}
	selftestMailbox.Lock()
	defer selftestMailbox.Unlock()
	if !waitSelftestPage(ctx, window, click, 3*time.Second) {
		return false, false, 0
	}
	started := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := runtime.DB.Message(checkCtx, id)
		if err == nil && current.IsRead {
			shown, evalErr := window.Page().EvalContext(checkCtx, visibleRead)
			if evalErr == nil && shown == true {
				return true, true, time.Since(started)
			}
		}
		select {
		case <-checkCtx.Done():
			return true, false, time.Since(started)
		case <-ticker.C:
		}
	}
}
