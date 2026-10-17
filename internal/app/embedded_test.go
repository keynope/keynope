package app

import (
	"bytes"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestBundledResourcesMatchRepository(t *testing.T) {
	for _, name := range []string{"assets/emoji/keynope-emoji-fonts.zip", "assets/emoji/keynope-emoji-glyphs.bin.gz"} {
		data := embeddedBytes(name)
		original, err := os.ReadFile(testRepositoryPath(name))
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 || !bytes.Equal(data, original) {
			t.Fatalf("bundled archive differs from repository: %s", name)
		}
		if &data[0] != &embeddedBytes(name)[0] {
			t.Errorf("large archive must not be copied on access: %s", name)
		}
	}
	for _, name := range []string{"LICENSE.txt", "app/Welcome.md", "assets/templates/previews.json", "assets/keynope-c64.ttf.base64", "web/workspace.js", "web/scene.js"} {
		if _, err := embeddedFiles.ReadFile(name); err != nil {
			t.Fatalf("required bundled resource %s: %v", name, err)
		}
	}
	err := fs.WalkDir(embeddedFiles, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		if name != "LICENSE.txt" && name != "app/Welcome.md" && !strings.HasPrefix(name, "assets/") && !strings.HasPrefix(name, "web/") {
			t.Errorf("unexpected bundled resource: %s", name)
		}
		actual, err := embeddedFiles.ReadFile(name)
		if err != nil {
			return err
		}
		original, err := os.ReadFile(testRepositoryPath(name))
		if err != nil {
			return err
		}
		if !bytes.Equal(actual, original) {
			t.Errorf("bundled resource differs from repository: %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
