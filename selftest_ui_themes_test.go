package main

import (
	"context"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestUIThemeSelftestMatrixMatchesRegistry(t *testing.T) {
	source, err := os.ReadFile("src/lib/appThemes.ts")
	if err != nil {
		t.Fatal(err)
	}
	entries := regexp.MustCompile(`id: "([a-z]+)",\s+rowHeight: ([0-9]+)`).FindAllStringSubmatch(string(source), -1)
	if len(entries) != len(uiThemeSelftestMatrix) || len(entries) != 5 {
		t.Fatal("native theme coverage must include all five registry entries")
	}
	seen := map[string]bool{}
	for index, entry := range entries {
		height, err := strconv.Atoi(entry[2])
		if err != nil || entry[1] != uiThemeSelftestMatrix[index].id || height != uiThemeSelftestMatrix[index].rowHeight {
			t.Fatal("native theme matrix drifted from the frontend registry")
		}
		for _, mode := range []string{"light", "dark"} {
			name := uiThemeSelftestCheck(entry[1], mode)
			if name == "" || seen[name] {
				t.Fatalf("duplicate/empty theme runtime check %q", name)
			}
			seen[name] = true
		}
	}
	if len(seen) != 10 {
		t.Fatal("both color modes must be checked for all five styles")
	}
}

func TestUIThemeSelftestChecksRealUIAndScopedStorage(t *testing.T) {
	choose := uiThemeChooseScript("paper", "light", "app")
	for _, want := range []string{"data-app-theme-option", "data-message-theme-option", "theme.click()", "mode.click()", "message.click()"} {
		if !strings.Contains(choose, want) {
			t.Fatalf("theme selection does not exercise %q", want)
		}
	}
	for _, forbidden := range []string{"setAttribute", "setProperty", "setItem", "useUIStore", "dispatchEvent"} {
		if strings.Contains(choose, forbidden) {
			t.Fatalf("theme selection bypasses production controls with %q", forbidden)
		}
	}
	assertion := uiThemeRenderedScript("paper", "light", 88)
	for _, want := range []string{"pebble:profile-storage-migrated", "'pebble:profile:'+namespace+':pebble-app-theme'", "getBoundingClientRect()", "--ui-mail-row-height", "scroll.scrollWidth", "fontFamily"} {
		if !strings.Contains(assertion, want) {
			t.Fatalf("theme runtime oracle misses %q", want)
		}
	}
	if strings.Contains(assertion, ".click()") || strings.Contains(assertion, "setItem") {
		t.Fatal("polling rendered theme must not change state or storage")
	}
}

func TestUIThemeSelftestRequiresExplicitSyntheticMode(t *testing.T) {
	t.Setenv("PEBBLE_SELFTEST", "")
	checks := map[string]bool{}
	selftestUIThemes(context.Background(), nil, checks, func(string) { t.Fatal("captured a normal user window") })
	if len(checks) != 0 {
		t.Fatal("theme harness must stay inactive outside explicit selftest mode")
	}
}

func TestUIThemeSelftestRestoresProfileSettingsTab(t *testing.T) {
	script := uiThemeRestoreSettingsTabScript("accounts")
	for _, want := range []string{"settings-tab-accounts", "target.click()", "target.disabled"} {
		if !strings.Contains(script, want) {
			t.Fatalf("settings tab restore misses %q", want)
		}
	}
	for _, forbidden := range []string{"setItem", "setAttribute", "setProperty", "pebble-settings-tab"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("settings tab restore bypasses UI using %q", forbidden)
		}
	}
	assertion := uiThemeSettingsTabRestoredScript("accounts")
	for _, want := range []string{"sessionStorage.getItem", "pebble:profile-storage-migrated", "===\"accounts\""} {
		if !strings.Contains(assertion, want) {
			t.Fatalf("settings restore assertion misses %q", want)
		}
	}
	for _, forbidden := range []string{"setItem", "setAttribute", "setProperty", ".click()"} {
		if strings.Contains(assertion, forbidden) {
			t.Fatalf("settings restore assertion mutates state using %q", forbidden)
		}
	}
}

func TestUIThemeGlassDarkRequiresDarkSidebarLayers(t *testing.T) {
	script := uiThemeGlassDarkSidebarScript()
	for _, want := range []string{"sidebar-account-row[data-selected", "sidebar-row[data-selected", "accountIsDark", "folderIsDark", "Math.max"} {
		if !strings.Contains(script, want) {
			t.Fatalf("glass dark sidebar check misses %q", want)
		}
	}
}

func TestUIThemeSelftestCoversRealNarrowLayout(t *testing.T) {
	reader := uiThemeNarrowReaderScript(390)
	for _, want := range []string{"innerWidth!==390", "innerWidth-24", "getComputedStyle(list).display!=='none'", "getComputedStyle(sidebar).display!=='none'", "message-detail-back", "scroll.scrollWidth"} {
		if !strings.Contains(reader, want) {
			t.Fatalf("narrow reader oracle misses %q", want)
		}
	}
	picker := uiThemeNarrowPickerScript(760)
	for _, want := range []string{"innerWidth!==760", "picker.scrollWidth<=picker.clientWidth", "panel.scrollWidth<=panel.clientWidth", "[data-app-theme-option]"} {
		if !strings.Contains(picker, want) {
			t.Fatalf("narrow picker oracle misses %q", want)
		}
	}
	for _, script := range []string{reader, picker} {
		if strings.Contains(script, ".click()") || strings.Contains(script, "setAttribute") || strings.Contains(script, "setProperty") {
			t.Fatal("narrow layout predicates must not fake responsive presentation")
		}
	}
	main, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(main), "MinWidth: 800, MinHeight: 600") {
		t.Fatal("update the isolated theme selftest's restored minimum to match production")
	}
}

func TestUIThemeSelftestHookFollowsMessageCapture(t *testing.T) {
	source, err := os.ReadFile("selftest.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	capture := strings.Index(text, `capture("message-read.png")`)
	hook := strings.Index(text, "selftestUIThemes(ctx, window, checks, capture)")
	threads := strings.Index(text, "opened, threadReadFast, threadReadElapsed :=")
	if capture < 0 || hook <= capture || threads <= hook {
		t.Fatal("theme checks must run after opening a message and before conversation checks")
	}
}
