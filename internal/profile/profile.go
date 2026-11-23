package profile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func Resolve(exeDir, fallback, override string) (string, error) {
	clean := func(value string) string { return strings.TrimPrefix(strings.TrimSpace(value), "\ufeff") }
	resolve := func(value string) string {
		value = clean(value)
		if filepath.IsAbs(value) {
			return filepath.Clean(value)
		}
		return filepath.Join(exeDir, value)
	}
	if clean(override) != "" {
		return resolve(override), nil
	}
	pointer := filepath.Join(exeDir, "gomail-profile.txt")
	data, err := os.ReadFile(pointer)
	if err == nil {
		first := strings.SplitN(string(data), "\n", 2)[0]
		if clean(first) != "" {
			return resolve(first), nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	portable := filepath.Join(exeDir, "gomail-profile")
	if info, err := os.Stat(portable); err == nil && info.IsDir() {
		return portable, nil
	}
	return fallback, nil
}

func Namespace(directory string) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, "profile-id")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	id := strings.TrimPrefix(strings.TrimSpace(string(data)), "\ufeff")
	if id == "" {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return "", err
		}
		id = hex.EncodeToString(random)
		if err := os.WriteFile(path, []byte(id), 0600); err != nil {
			return "", err
		}
	}
	var hash uint64 = 0xcbf29ce484222325
	for _, b := range []byte(strings.ToLower(strings.ReplaceAll(id, "\\", "/"))) {
		hash ^= uint64(b)
		hash *= 0x100000001b3
	}
	result := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		result[i] = byte(hash)
		hash >>= 8
	}
	return hex.EncodeToString(result), nil
}
