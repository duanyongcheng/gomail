package main

import (
	"context"
	"testing"
	"time"
)

func TestPackagedM2SyntheticProviders(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	checks := map[string]bool{}
	if err := runM2Selftest(ctx, checks); err != nil {
		t.Fatal(err)
	}
	for _, name := range m2SelftestChecks {
		if !checks[name] {
			t.Errorf("synthetic package assertion failed: %s", name)
		}
	}
}
