package app

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAuthoredMetadataRejectsAmbiguousPropertiesAndRegistryIDs(t *testing.T) {
	table, _ := json.Marshal(structuredFixture().Objects.Tables["table-1"])
	chart, _ := json.Marshal(structuredFixture().Objects.Charts["chart-1"])
	build, _ := json.Marshal(structuredFixture().Objects.Builds["slide-1"])
	for name, raw := range map[string]string{
		"registry-field":   `{"version":1,"tables":{},"tables":{}}`,
		"registry-alias":   `{"version":1,"Version":1}`,
		"registry-escaped": `{"version":1,"vers\u0069on":1}`,
		"table-id":         `{"version":1,"tables":{"same":` + string(table) + `,"same":` + string(table) + `}}`,
		"chart-id":         `{"version":1,"charts":{"same":` + string(chart) + `,"same":` + string(chart) + `}}`,
		"build-id":         `{"version":1,"builds":{"same":` + string(build) + `,"same":` + string(build) + `}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var objects DeckObjects
			if err := json.Unmarshal([]byte(raw), &objects); err == nil {
				t.Fatal("ambiguous authored metadata accepted")
			}
			var compressed bytes.Buffer
			writer := gzip.NewWriter(&compressed)
			if _, err := writer.Write([]byte(raw)); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			envelope := "<!-- keynope-objects version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->"
			if _, _, err := decodeDeckObjects(envelope); err == nil {
				t.Fatal("ambiguous compressed metadata accepted")
			}
		})
	}
	for _, raw := range []string{`{"fontSize":12,"fontSize":48}`, `{"fontSize":12,"FontSize":48}`, `{"future":1,"future":2}`} {
		var style TableCellStyle
		if err := json.Unmarshal([]byte(raw), &style); err == nil {
			t.Fatal("ambiguous nested style accepted", raw)
		}
	}
}

func TestMetadataAmbiguityGuardPreservesOpaqueNestedData(t *testing.T) {
	raw := []byte(`{"Version":1,"Tables":{},"future":{"first":1,"first":2,"deep":[{"x":true}]}}`)
	var objects DeckObjects
	if err := json.Unmarshal(raw, &objects); err != nil {
		t.Fatal(err)
	}
	if objects.Version != 1 || objects.Extra == nil || len(*objects.Extra) != 1 || string((*objects.Extra)["future"]) != `{"first":1,"first":2,"deep":[{"x":true}]}` {
		t.Fatal("opaque nested content was interpreted or known alias retained as future metadata", objects)
	}
	encoded, err := json.Marshal(objects)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"first":1,"first":2`) {
		t.Fatal("opaque nested pairs changed", string(encoded))
	}
	for _, raw := range []string{`{"version":1} {}`, `[]`, `{"version":1,}`} {
		var objects DeckObjects
		if json.Unmarshal([]byte(raw), &objects) == nil {
			t.Fatal("malformed metadata accepted", raw)
		}
	}
}

func TestClipboardMetadataAmbiguityRejectedBeforeAssignment(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"version":2}`,
		`{"version":1,"Version":2}`,
		`{"version":1,"themeFallbacks":{"same":"fg=x","same":"fg=y"}}`,
		`{"version":1,"assets":{"same":{},"same":{}}}`,
		`{"version":1,"chartThemeFallbacks":{"same":{},"same":{}}}`,
		`{"version":1,"objects":{"version":1,"charts":{"same":{},"same":{}}}}`,
	} {
		bundle := ObjectClipboard{Version: 1, Elements: []Element{{ID: "original", Kind: "text", Text: "Unchanged"}}}
		if err := json.Unmarshal([]byte(raw), &bundle); err == nil || len(bundle.Elements) != 1 || bundle.Elements[0].ID != "original" {
			t.Fatal("ambiguous clipboard accepted or partially assigned", raw, err)
		}
	}
	var objects DeckObjects
	table, _ := json.Marshal(structuredFixture().Objects.Tables["table-1"])
	raw := `{"version":1,"tables":{"Case":` + string(table) + `,"case":` + string(table) + `}}`
	if err := json.Unmarshal([]byte(raw), &objects); err != nil || len(objects.Tables) != 2 {
		t.Fatal("case-sensitive registry IDs conflated", err)
	}
}

func TestMetadataAmbiguityPasteEndpointKeepsDocumentAndHistory(t *testing.T) {
	for _, raw := range []string{
		`{"action":"paste-objects","clipboard":{"version":1,"Version":2}}`,
		`{"action":"paste-objects","clipboard":{"version":1,"assets":{"same":{},"same":{}}}}`,
		`{"action":"paste-objects","clipboard":{"version":1,"objects":{"version":1,"charts":{"same":{},"same":{}}}}}`,
	} {
		session := newNativeEditorSession("owned.md", structuredFixture(), false, false)
		before := cloneDeck(session.deck)
		version, cache := session.version, session.sceneResponse
		response := httptest.NewRecorder()
		session.handlePasteObjectClipboard(response, httptest.NewRequest(http.MethodPost, "/api/editor/paste-clipboard", strings.NewReader(raw)))
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid object clipboard") || !reflect.DeepEqual(session.deck, before) || session.version != version || session.sceneResponse != cache || len(session.undo) != 0 || len(session.redo) != 0 {
			t.Fatal("rejected ambiguous paste changed document/transaction", response.Code, response.Body.String())
		}
	}
}
