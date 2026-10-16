package main

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPlacementNumbersRejectMalformedAndNonfinite(t *testing.T) {
	floatKeys := strings.Fields("width height shape-offset-x shape-offset-y object-offset-x object-offset-y left_pct right_pct modern-line-height modern-paragraph-before modern-paragraph-after modern-size modern-width ttf-width object-rotation modern-opacity scale brightness contrast saturation sharpness shape-stroke-width connector-width connector-arrow-width label-ttf-width")
	integerKeys := strings.Fields("top bottom left right row_delta ttf-size text-size z-index shadow-x shadow-y alpha slide label-ttf-size")
	for _, keys := range [][]string{floatKeys, integerKeys} {
		for _, key := range keys {
			for _, value := range []string{"", "garbage", "NaN", "+Inf", "-Inf", "1e999"} {
				t.Run(key+"/"+value, func(t *testing.T) {
					q := url.Values{key: {value}, "render": {"truetype"}}
					if err := validateElementPlacementQuery(Element{Kind: "text", Query: q.Encode()}); err == nil {
						t.Fatal("bad typed number accepted")
					}
					for _, body := range []string{q.Encode(), placementCommentText(q.Encode())} {
						// The serializer omits empty values; raw authored input must still reject.
						if value == "" {
							body = "render=truetype " + key + "="
						}
						if _, _, err := textPlacementCommentChecked("<!-- " + body + " -->"); err == nil {
							t.Fatal("bad authored number accepted", body)
						}
					}
				})
			}
		}
	}
}

func TestPlacementNumbersPreserveLegacyFiniteNormalization(t *testing.T) {
	query, ok, err := textPlacementCommentChecked("<!-- top=12000 left_pct=1.5 ttf-width=250 alpha=300 shadow-x=9 scale=1.25 -->")
	if err != nil || !ok {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(query)
	want := url.Values{"top": {"10000"}, "left_pct": {"1.000000"}, "ttf-width": {"200"}, "alpha": {"255"}, "shadow-x": {"4"}, "scale": {"1.25"}}
	if !reflect.DeepEqual(q, want) {
		t.Fatal("legacy normalization changed", q)
	}
	for _, query := range []string{"future=NaN&future=Inf", "top=-10&width=0.5&object-rotation=-360&brightness=1e2"} {
		if err := validateElementPlacementQuery(Element{Kind: "text", Query: query}); err != nil {
			t.Fatal("valid finite/opaque query rejected", err)
		}
	}
	if err := validateElementPlacementQuery(Element{Kind: "future-widget", Query: "width=NaN&width=Inf"}); err != nil {
		t.Fatal("future element query reinterpreted", err)
	}
}

func TestPlacementGeometryRejectsUnboundedFiniteCoordinates(t *testing.T) {
	for _, key := range strings.Fields("top bottom left right row_delta width height") {
		for _, value := range []string{"1000001", "-1000001", "9223372036854775807"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				query := url.Values{key: {value}}.Encode()
				if err := validateElementPlacementQuery(Element{Kind: "text", Query: query}); err == nil {
					t.Fatal("unbounded finite geometry admitted", query)
				}
			})
		}
	}
	for _, key := range []string{"width", "height"} {
		if err := validateElementPlacementQuery(Element{Kind: "text", Query: key + "=1e308"}); err == nil {
			t.Fatal("finite dimension can overflow downstream pixel conversion", key)
		}
	}
	for _, query := range []string{"top=-1000000&left=1000000&width=0.5&height=1000000", "width=1.125&height=20.375", "future-coordinate=1e308"} {
		if err := validateElementPlacementQuery(Element{Kind: "text", Query: query}); err != nil {
			t.Fatal("bounded fractional or opaque geometry rejected", query, err)
		}
	}
}

func TestPlacementNumbersRejectAllAuthoredOwnersAndSave(t *testing.T) {
	for _, query := range []string{"brightness=NaN", "top=invalid", "width=1e999", "width=1e308", "row_delta=9223372036854775807", "top=1&Top=2", "link=https%3A%2F%2Fexample.com&slide=2"} {
		element := Element{Kind: "text", Text: "Keep", Query: query}
		for _, owner := range []string{"slide", "base", "layout"} {
			t.Run(owner+"/"+query, func(t *testing.T) {
				d := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Original"}}}}}
				switch owner {
				case "slide":
					d.Slides[0].Elements = []Element{element}
				case "base":
					d.Masters.Base.Slide.Elements = []Element{element}
				case "layout":
					d.Masters = defaultMasterDeck()
					d.Masters.Layouts[0].Slide.Elements = []Element{element}
				}
				before := cloneDeck(d)
				data, err := serializeDeck("reject.md", d)
				if err == nil || data != nil || !reflect.DeepEqual(before, d) {
					t.Fatal("bad save returned data or changed source", err)
				}
			})
		}
	}
	// Image URL queries and typed master/envelope queries must not bypass the
	// readable-comment boundary. Known page-number envelopes are checked too.
	for _, source := range []string{
		"![image](https://example.com/image.png?brightness=NaN)",
		"![image](https://example.com/image.png?row_delta=9223372036854775807)",
		"<!-- top=9223372036854775807 -->\nRejected",
		"<!-- width=1e308 -->\nRejected",
		"![image](https://example.com/image.png?element-id=first&element-id=second)",
		"![image](https://example.com/image.png?object-id=first&object-id=second)",
		func() string {
			data, _ := json.Marshal(Element{Kind: "page-number", Text: "1", Query: "width=Inf"})
			return "<!-- keynope-element version=1 base64:" + base64.StdEncoding.EncodeToString(data) + " -->"
		}(),
	} {
		if _, err := parseDeckData("reject.md", []byte(source)); err == nil {
			t.Fatal("non-comment query bypassed admission", source)
		}
	}
}

func TestPlacementNumbersNestedLabelsAndMasterAdmission(t *testing.T) {
	raw, _ := json.Marshal(shapeLabelData{Text: "Label", Query: "ttf-width=NaN"})
	e := Element{Kind: "shape", Query: url.Values{"shape": {"square"}, "shape-label": {base64.StdEncoding.EncodeToString(raw)}}.Encode()}
	if err := validateElementPlacementQuery(e); err == nil || !strings.Contains(err.Error(), "shape label") {
		t.Fatal("nested label bypassed admission", err)
	}
	for _, base := range []bool{true, false} {
		masters := defaultMasterDeck()
		if base {
			masters.Base.Slide.Elements = []Element{e}
		} else {
			masters.Layouts[0].Slide.Elements = []Element{e}
		}
		envelope, err := encodeMasterDeckMetadata(masters)
		if err != nil {
			t.Fatal(err)
		}
		w, h, order := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
		if _, err := parseDeckData("master.md", []byte("<!-- keynope width=100 height=80 -->\n"+envelope+"\nText")); err == nil {
			t.Fatal("master label accepted invalid number")
		}
		if authoredTerminalWidth != w || authoredTerminalHeight != h || parsedDeckElementOrderChanged != order {
			t.Fatal("rejected master changed runtime state")
		}
	}
}

func TestPlacementNumbersFailedSaveKeepsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned.md")
	original := []byte("Existing deck stays here\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"ttf-width=NaN", "width=1e308", "row_delta=9223372036854775807"} {
		deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "text", Text: "Invalid candidate", Query: query}}}}}
		if err := saveDeck(path, deck); err == nil {
			t.Fatal("invalid numeric save succeeded", query)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(original) {
			t.Fatal("invalid numeric save replaced existing file", query, err)
		}
	}
}
