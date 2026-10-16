package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestMarkdownIsNeverAutomaticallyHandledByKeynope(t *testing.T) {
	data, err := os.ReadFile("app/Info.plist")
	if err != nil {
		t.Fatal(err)
	}
	// None permits explicitly opening/dropping decks without offering Keynope
	// as an automatic handler for all Markdown documents.
	ranks := regexp.MustCompile(`<key>LSHandlerRank</key>\s*<string>([^<]+)</string>`).FindAllStringSubmatch(string(data), -1)
	if len(ranks) != 1 || ranks[0][1] != "None" {
		t.Fatalf("unexpected document handler ranks: %v", ranks)
	}
	if !strings.Contains(string(data), "net.daringfireball.markdown") {
		t.Fatal("explicit Markdown document opening must remain supported")
	}
	workflow, err := os.ReadFile(".github/workflows/upload-mac-app-store.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(workflow), `CFBundleDocumentTypes.0.LSHandlerRank raw -o - bin/Keynope.app/Contents/Info.plist)" = "None"`) {
		t.Fatal("App Store bundle verification must enforce the non-default handler rank")
	}
}
