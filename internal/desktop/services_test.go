package desktop

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// A running app must publish diagnostics as they happen. Buffering until Close
// leaves an empty log for the whole session, which is exactly when a failure
// needs to be inspected.
func TestOpenLogPublishesRecordsBeforeClose(t *testing.T) {
	root := t.TempDir()
	log, err := OpenLog(root)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	slog.New(slog.NewJSONHandler(log, nil)).Warn("visible while running", "stage", "http_status")

	path := filepath.Join(root, "logs", "pebble-go.log")
	if got := readLog(t, path); !strings.Contains(got, "visible while running") {
		t.Fatal("diagnostic was not written before Close", got)
	}
}

// Rotation must not leave the session writing to the replaced file.
func TestRotatingLogKeepsPublishingAcrossRotation(t *testing.T) {
	root := t.TempDir()
	log, err := OpenLog(root)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	path := filepath.Join(root, "logs", "pebble-go.log")

	// Exceed one rotation budget through the same writer the app uses.
	line := []byte(strings.Repeat("x", 4096) + "\n")
	for written := 0; written <= 2<<20; written += len(line) {
		if _, err := log.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := log.Write([]byte("after rotation\n")); err != nil {
		t.Fatal(err)
	}
	if current := readLog(t, path); !strings.Contains(current, "after rotation") {
		t.Fatal("records were not published to the rotated-to file", len(current))
	}
	if previous := readLog(t, path+".1"); previous == "" {
		t.Fatal("previous log was not retained")
	}
	if info, err := os.Stat(path); err != nil || info.Size() > 2<<20 {
		t.Fatal("rotated log exceeded its budget", info, err)
	}
}

// Concurrent writers must not interleave or lose whole records.
func TestRotatingLogSerializesConcurrentWriters(t *testing.T) {
	root := t.TempDir()
	log, err := OpenLog(root)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for line := 0; line < 50; line++ {
				if _, err := log.Write([]byte("record\n")); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	group.Wait()
	if got := strings.Count(readLog(t, filepath.Join(root, "logs", "pebble-go.log")), "record\n"); got != 400 {
		t.Fatal("concurrent writes lost records", got)
	}
}
