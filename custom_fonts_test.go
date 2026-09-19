package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func legacyFontFixture() (string, error) {
	var data bytes.Buffer
	writer := gzip.NewWriter(&data)
	if _, err := writer.Write([]byte("{\"arcade\":{\"id\":\"arcade\",\"name\":\"Legacy\",\"normal\":{\"A\":[\"###\",\"#.#\"]},\"bold\":{\"A\":[\"###\",\"###\"]}}}")); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return "<!-- keynope-fonts version=1 base64:" + base64.StdEncoding.EncodeToString(data.Bytes()) + " -->", nil
}

func TestLegacyFontDeckLoadDoesNotTouchUserLibrary(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "existing"}[existing], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("APP_SANDBOX_CONTAINER_ID", "")
			library := filepath.Join(home, ".keynope", "fonts")
			path := filepath.Join(library, "arcade.json.gz")
			if existing {
				if err := os.MkdirAll(library, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("user-owned archive"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			metadata, err := legacyFontFixture()
			if err != nil {
				t.Fatal(err)
			}
			deck, err := parseDeckData("legacy.md", []byte(metadata+"\n\n<!-- font=arcade render=text-image -->\nA\n"))
			if err != nil {
				t.Fatal(err)
			}
			if !isTrueType(deck.Slides[0].Elements[0]) {
				t.Fatal("legacy text did not migrate")
			}
			if existing {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "user-owned archive" {
					t.Fatal("existing user library changed")
				}
				entries, err := os.ReadDir(library)
				if err != nil || len(entries) != 1 {
					t.Fatal("library contents changed")
				}
			} else if _, err := os.Stat(library); !os.IsNotExist(err) {
				t.Fatal("deck load created font library")
			}
		})
	}
}
