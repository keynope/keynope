//go:build !js && !keynope_sitegen

package app

import (
	"os"
	"strings"
	"testing"
)

func TestMCPScreenshotHelperPackaging(t *testing.T) {
	// The nested renderer must inherit the sandbox, not re-enter it as the
	// independently sandboxed main application executable.
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(testRepositoryPath(path))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	makefile := read("Makefile")
	for _, want := range []string{"KEYNOPE_APP_RENDERER := $(KEYNOPE_APP)/Contents/Helpers/keynope-renderer", "--identifier sh.keynope.app.renderer --entitlements $(KEYNOPE_APP_ENGINE_ENTITLEMENTS)"} {
		if !strings.Contains(makefile, want) {
			t.Fatal("renderer not separately packaged/signed", want)
		}
	}
	for _, path := range []string{".github/workflows/upload-mac-app-store.yml", ".github/workflows/test-and-build-artifacts.yml"} {
		data := read(path)
		if !strings.Contains(data, "--identifier sh.keynope.app.renderer") || !strings.Contains(data, "--entitlements app/KeynopeEngine.entitlements") {
			t.Fatal("release misses inherited-sandbox renderer", path)
		}
	}
	store := read(".github/workflows/upload-mac-app-store.yml")
	if !strings.Contains(store, "-output dist/Keynope.app/Contents/Helpers/keynope-renderer") {
		t.Fatal("universal renderer not assembled")
	}
	entitlements := read("app/KeynopeEngine.entitlements")
	if !strings.Contains(entitlements, "com.apple.security.inherit") {
		t.Fatal("renderer sandbox inheritance missing")
	}
	native := read("presenter/Automation.swift")
	if strings.Contains(native, "WKContentRuleListStore.default()") || !strings.Contains(native, "WKContentRuleListStore(url: ruleDirectory)") {
		t.Fatal("renderer sharing persistent WebKit rule storage")
	}
}
