package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func assetArchiveEnvelope(t *testing.T, raw []byte) string {
	t.Helper()
	var output bytes.Buffer
	writer := gzip.NewWriter(&output)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return "<!-- keynope-assets version=1 base64:" + base64.StdEncoding.EncodeToString(output.Bytes()) + " -->"
}

func TestAssetArchiveRejectsAmbiguousAndUnsupportedEnvelopes(t *testing.T) {
	valid := assetArchiveEnvelope(t, []byte(`{}`))
	asset, _ := json.Marshal(clipboardTestAsset(t))
	for name, text := range map[string]string{
		"duplicate":     valid + "\n" + valid,
		"mixed-version": valid + "\n" + strings.Replace(valid, "version=1", "version=2", 1),
		"unsupported":   strings.Replace(valid, "version=1", "version=2", 1),
		"malformed":     "<!-- keynope-assets version=1 base64:??? -->",
		"null-registry": assetArchiveEnvelope(t, []byte(`null`)),
		"duplicate-id":  assetArchiveEnvelope(t, []byte(`{"same":`+string(asset)+`,"same":`+string(asset)+`}`)),
		"escaped-id":    assetArchiveEnvelope(t, []byte(`{"same":`+string(asset)+`,"sa\u006de":`+string(asset)+`}`)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, remaining, err := decodeDeckAssets(text); err == nil || remaining != text {
				t.Fatal("ambiguous archive accepted or input consumed on failure", err)
			}
		})
	}
}

func TestAssetArchiveCompleteByteBudgetAndChecksum(t *testing.T) {
	const limit = 256
	for _, length := range []int{limit - 1, limit, limit + 1} {
		raw := append([]byte(`{}`), bytes.Repeat([]byte(" "), length-2)...)
		text := assetArchiveEnvelope(t, raw)
		_, remaining, err := decodeDeckAssetArchive(text, limit)
		if length <= limit && (err != nil || remaining != "") {
			t.Fatal("bounded archive rejected", length, err)
		}
		if length > limit && (err == nil || remaining != text) {
			t.Fatal("oversized valid JSON prefix was accepted", err)
		}
	}
	valid := assetArchiveEnvelope(t, []byte(`{}`))
	encoded := keynopeAssetsRE.FindStringSubmatch(valid)[1]
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	compressed[len(compressed)-8] ^= 0xff
	corrupt := "<!-- keynope-assets version=1 base64:" + base64.StdEncoding.EncodeToString(compressed) + " -->"
	if _, _, err := decodeDeckAssetArchive(corrupt, limit); err == nil {
		t.Fatal("bad gzip checksum accepted")
	}
	oversized := "<!-- keynope-assets version=1 base64:" + strings.Repeat("A", limit*2+4) + " -->"
	if _, _, err := decodeDeckAssetArchive(oversized, limit); err == nil || !strings.Contains(err.Error(), "encoded size") {
		t.Fatal("encoded budget was not checked first", err)
	}
}

func TestAssetArchiveEncoderDecoderBudgetSymmetry(t *testing.T) {
	assets := map[string]DeckAsset{"owned": clipboardTestAsset(t)}
	raw, err := json.Marshal(assets)
	if err != nil {
		t.Fatal(err)
	}
	text, err := encodeDeckAssetArchive(assets, len(raw))
	if err != nil {
		t.Fatal(err)
	}
	decoded, remaining, err := decodeDeckAssetArchive(text, len(raw))
	if err != nil || remaining != "" || !reflect.DeepEqual(decoded, assets) {
		t.Fatal("at-limit asset round trip", err)
	}
	if text, err := encodeDeckAssetArchive(assets, len(raw)-1); err == nil || text != "" {
		t.Fatal("encoder produced archive its decoder rejects")
	}
	unknown := "<!-- keynope-assets-extra version=2 future-data -->"
	if _, remaining, err := decodeDeckAssets(unknown); err != nil || remaining != unknown {
		t.Fatal("unrelated future envelope consumed", err)
	}
}
