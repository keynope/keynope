package app

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestBufferedLayoutFingerprintPreservesFraming(t *testing.T) {
	for _, values := range [][]string{
		{"", "A", "", "B"}, {"A\x00B", "🎉", "percent%+&"},
		{strings.Repeat("x", 4095), "cross-buffer", strings.Repeat("y", 10000)},
	} {
		want, got := sha256.New(), sha256.New()
		buffer := bufio.NewWriterSize(got, 4096)
		for _, value := range values {
			var size [8]byte
			binary.LittleEndian.PutUint64(size[:], uint64(len(value)))
			want.Write(size[:])
			want.Write([]byte(value))
			layoutHashString(buffer, value)
		}
		if err := buffer.Flush(); err != nil || string(want.Sum(nil)) != string(got.Sum(nil)) {
			t.Fatal("buffer changed layout fingerprint stream", err)
		}
	}
}

func TestTextDependencyKeyBorrowsQueriesWithoutRetainingThem(t *testing.T) {
	q := url.Values{
		"render": {"truetype"}, "modern-font": {"sans"}, "modern-size": {"48"},
		"fg": {"#ff0055"}, "top": {"3"}, "width": {"100"}, "link": {"https://example.com"},
		"future": {"first", "second"}, "text-align": {"justify"},
	}
	before := cloneQueryValues(q)
	e := Element{Kind: "text", Text: "Owned text", Query: q.Encode()}
	key := sceneTextDependencyKey(Deck{}, e, "modern", q)
	want := url.Values{"modern-font": {"sans"}, "modern-size": {"48"}, "future": {"first", "second"}, "text-align": {"justify"}}
	if key.element.Query != want.Encode() || !reflect.DeepEqual(q, before) {
		t.Fatal("key filter lost dependencies or mutated borrowed values", key.element.Query)
	}
	q["future"][0] = "caller mutation"
	q.Set("modern-font", "c64")
	if key.element.Query != want.Encode() {
		t.Fatal("cache key retained borrowed slices")
	}
}

func TestSceneLinkValuesMatchQueryReader(t *testing.T) {
	for _, query := range []string{
		"", "slide=2", "slide=9", "slide=invalid&link=https%3A%2F%2Fexample.com",
		"link=https%3A%2F%2Fexample.com%2Fa%252Fb%3Fq%3Da%2Bb", "link=javascript%3Aalert%281%29",
		"bad=%ZZ&link=https%3A%2F%2Fexample.com", "link=https%3A%2F%2Fexample.com&link=https%3A%2F%2Fother.com",
	} {
		want, wantOK := linkTargetFromQuery(query, 3)
		q, err := url.ParseQuery(query)
		got, gotOK := linkTarget{}, false
		if err == nil {
			got, gotOK = linkTargetFromValues(q, 3)
		}
		if got != want || gotOK != wantOK {
			t.Fatal("shared link values changed validation", query, got, want)
		}
	}
}

func TestResolvedWorkspacePreviewMatchesColdCorpus(t *testing.T) {
	for name, deck := range powerupProofCorpus(t) {
		t.Run(name, func(t *testing.T) {
			before := cloneDeck(deck)
			for i := range deck.Slides {
				resolved := deck.ResolveSlide(i, false)
				owned := cloneSlide(resolved)
				preview := deck.resolvedSlideRenderPreview(resolved, i, 245, 56)
				got := editorPreviewWorkspacePages(deck, preview, i, 245, 56)
				want := editorSlideWorkspacePages(deck, i, 245, 56)
				// Compare encoded payloads too: shared pointer identity is not proof
				// of equivalent public output.
				g, ge := json.Marshal(got)
				w, we := json.Marshal(want)
				if ge != nil || we != nil || string(g) != string(w) || !reflect.DeepEqual(resolved, owned) {
					t.Fatal("reused resolution changed output or owned input", i, ge, we)
				}
			}
			if !reflect.DeepEqual(deck, before) {
				t.Fatal("projection changed authored corpus")
			}
		})
	}
}
