package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfilePrecedenceAndPersistentNamespace(t *testing.T) {
	exe := t.TempDir()
	fallback := filepath.Join(t.TempDir(), "default")
	if got, err := Resolve(exe, fallback, ""); err != nil || got != fallback {
		t.Fatal(got, err)
	}
	portable := filepath.Join(exe, "gomail-profile")
	if err := os.Mkdir(portable, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := Resolve(exe, fallback, ""); err != nil || got != portable {
		t.Fatal(got, err)
	}
	if err := os.WriteFile(filepath.Join(exe, "gomail-profile.txt"), []byte("\ufeffprofiles/work\ncomment"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := Resolve(exe, fallback, ""); err != nil || got != filepath.Join(exe, "profiles/work") {
		t.Fatal(got, err)
	}
	if got, err := Resolve(exe, fallback, "override"); err != nil || got != filepath.Join(exe, "override") {
		t.Fatal(got, err)
	}
	first, err := Namespace(portable)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Namespace(portable)
	if err != nil || first != second {
		t.Fatal("namespace did not persist", err)
	}
	moved := filepath.Join(exe, "moved")
	if err := os.Rename(portable, moved); err != nil {
		t.Fatal(err)
	}
	after, err := Namespace(moved)
	if err != nil || after != first {
		t.Fatal("namespace changed after moving profile", err)
	}
}
