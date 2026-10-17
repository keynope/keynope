package app

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func compressedActivityComment(t *testing.T, payload []byte) string {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return plainMetadataComment("engagement-results", string(compressed.Bytes()))
}

func TestActivityAndBackgroundMetadataAdmission(t *testing.T) {
	definition, err := encodeEngagementMetadata(&EngagementDefinition{ID: "storm-1", Kind: "storm", Prompt: "Ideas?"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := encodeEngagementResult(activityResultFixture())
	if err != nil {
		t.Fatal(err)
	}
	background, err := encodeBackgroundMetadata(&slideBackgroundMedia{AssetID: "bg", Fit: "contain"})
	if err != nil {
		t.Fatal(err)
	}
	for namespace, valid := range map[string]string{"engagement": definition, "engagement-results": result, "background-media": background} {
		for name, metadata := range map[string]string{
			"duplicate":       valid + "\n" + valid,
			"future":          strings.Replace(valid, "version=1", "version=2", 1),
			"malformed":       "<!-- keynope-" + namespace + " version=1 base64:%%% -->",
			"invalid-payload": plainMetadataComment(namespace, `[]`),
			"trailing":        valid + " trailing data",
		} {
			t.Run(namespace+"/"+name, func(t *testing.T) {
				if _, err := parseEditorDeckData("invalid.md", []byte(metadata+"\n\nText\n")); err == nil {
					t.Fatal("invalid activity/background archive silently accepted")
				}
			})
		}
	}
	for _, namespace := range []string{"engagement", "background-media"} {
		large := "<!-- keynope-" + namespace + " version=1 base64:" + strings.Repeat("A", base64.StdEncoding.EncodedLen(maxJSONExtensionBytes)+4) + " -->"
		if _, err := parseSlideChecked(large, "."); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatal("encoded bound missing", namespace, err)
		}
	}
	for namespace, payload := range map[string]string{
		"engagement":       `{"id":"one","ID":"two","kind":"storm"}`,
		"background-media": `{"assetId":"one","AssetId":"two"}`,
	} {
		if _, err := parseSlideChecked(plainMetadataComment(namespace, payload), "."); err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatal("typed ambiguity accepted", namespace, err)
		}
	}
	for _, state := range []string{`"phase":3,"phase":4`, `"phase":3,"ph\u0061se":4`} {
		payload := []byte(`{"version":1,"activityId":"storm-1","kind":"storm","state":{` + state + `}}`)
		if _, err := decodeEngagementResult(compressedActivityComment(t, payload)); err == nil || !strings.Contains(err.Error(), "ambiguous") {
			t.Fatal("duplicate result state silently overwritten", err)
		}
	}
}

func TestActivityResultWholeStreamAndDefinitionBounds(t *testing.T) {
	data, _ := json.Marshal(activityResultFixture())
	for _, size := range []int{maxEngagementResultBytes - 1, maxEngagementResultBytes} {
		padded := append(append([]byte(nil), data...), bytes.Repeat([]byte(" "), size-len(data))...)
		if _, err := decodeEngagementResult(compressedActivityComment(t, padded)); err != nil {
			t.Fatal("complete boundary stream rejected", size, err)
		}
	}
	// A valid JSON prefix must not hide extra decompressed bytes.
	tooLarge := append(append([]byte(nil), data...), bytes.Repeat([]byte(" "), maxEngagementResultBytes-len(data)+1)...)
	if _, err := decodeEngagementResult(compressedActivityComment(t, tooLarge)); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatal("truncated prefix accepted", err)
	}
	comment := compressedActivityComment(t, data)
	compressed, err := base64.StdEncoding.DecodeString(strings.Split(strings.Split(comment, "base64:")[1], " ")[0])
	if err != nil {
		t.Fatal(err)
	}
	compressed[len(compressed)-8] ^= 1
	if _, err := decodeEngagementResult(plainMetadataComment("engagement-results", string(compressed))); err == nil {
		t.Fatal("corrupt gzip checksum accepted")
	}
	definition := EngagementDefinition{ID: "one", Kind: "storm", Extra: extensionFixture("future", `"`+strings.Repeat("x", maxEngagementMetadataBytes)+`"`)}
	if _, err := encodeEngagementMetadata(&definition); err == nil {
		t.Fatal("oversized definition could be saved")
	}
	s := newNativeEditorSession("owned.md", Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Keep"}}}}})
	assertEditorFailureUnchanged(t, s, nativeEditorAction{Action: "set-engagement", EngagementData: &definition})
}

func TestBackgroundMetadataOwnershipClearingAndPrivacy(t *testing.T) {
	media := &slideBackgroundMedia{AssetID: "bg", Fit: "contain", Opacity: .5, OpacitySet: true, Query: "brightness=1.2", Extra: extensionFixture("futurePrivate", `{"token":"not participant data"}`)}
	d := Deck{Assets: map[string]DeckAsset{"bg": clipboardTestAsset(t)}, Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Public"}}, BackgroundMedia: media, BackgroundMediaSet: true}}}
	encoded, err := serializeDeck("background.md", d)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := parseDeckData("background.md", encoded)
	if err != nil || !reflect.DeepEqual(loaded.Slides[0].BackgroundMedia, media) {
		t.Fatal("background fields lost", err)
	}
	clone := cloneSlideBackgroundMedia(media)
	(*clone.Extra)["futurePrivate"][0] = '['
	if (*media.Extra)["futurePrivate"][0] != '{' {
		t.Fatal("background clone aliases source metadata")
	}
	incoming := cloneSlide(loaded.Slides[0])
	incoming.BackgroundMedia.Extra = nil
	incoming.BackgroundMedia.Query = "brightness=1.4"
	updated := prepareSlideAppearanceUpdate(loaded.Slides[0], incoming)
	requireExtension(t, updated.BackgroundMedia.Extra, "futurePrivate", `{"token":"not participant data"}`)
	public, err := participantSlideMarkdown(loaded, 0)
	if err != nil || bytes.Contains(public, []byte("futurePrivate")) {
		t.Fatal("background authoring data leaked", err)
	}
	publicDeck, err := parseDeckData("public.md", public)
	if err != nil || publicDeck.Slides[0].BackgroundMedia.Extra != nil {
		t.Fatal("encoded background authoring data leaked", err)
	}
	cleared, err := parseSlideChecked(plainMetadataComment("background-media", "null")+"Text", ".")
	if err != nil || !cleared.BackgroundMediaSet || cleared.BackgroundMedia != nil {
		t.Fatal("explicit inherited-background clearing rejected", err)
	}
	code := "<!-- keynope-engagement version=2 base64:%%% -->\n<!-- keynope-engagement-results version=2 base64:%%% -->\n<!-- keynope-background-media version=2 base64:%%% -->"
	slide, err := parseSlideChecked("```\n"+code+"\n```", ".")
	if err != nil || len(slide.Elements) != 1 || slide.Elements[0].Text != code {
		t.Fatal("literal activity/background code interpreted", err)
	}
}
