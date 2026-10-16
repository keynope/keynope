package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

var benchmarkLabelCapabilities selectionCapabilityState

func denseLabelCapabilitiesFixture(count int) (Deck, Slide, []string) {
	data, _ := json.Marshal(shapeLabelData{Text: "A diagram label", Query: "render=truetype&modern-font=mono&modern-size=40"})
	query := setQueryValue("left=10&top=4&width=70&height=16&shape=square&label-text-italic=1&label-ttf-weight=bold", "shape-label", base64.StdEncoding.EncodeToString(data))
	d := Deck{Theme: builtinDeckTheme("clean-light"), Appearance: &DeckAppearance{Version: 3}, Slides: []Slide{{ID: "dense-labels"}}}
	ids := []string{}
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("label-%d", i)
		d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: id, Kind: "shape", Query: query})
		ids = append(ids, id)
	}
	return d, d.ResolveSlide(0, false), ids
}

// Keep the previous scalar preparation as a differential oracle. It decodes
// and resolves each label independently for every requested property.
func scalarLabelPropertyReference(deck Deck, slide Slide, authored, resolved Element, property string) (interface{}, bool, bool) {
	common := labelCommonProperty(property)
	label, ok := shapeLabel(resolved)
	if !ok {
		return nil, false, false
	}
	raw, _ := rawShapeLabel(authored)
	if raw.ID == "" {
		raw = Element{Kind: "text", ID: authored.ID, Text: label.Text, Query: shapeLabelStyleValues(authored, url.Values{}).Encode()}
	}
	label.Query = deck.resolveThemeTextValues(label, mustQuery(label.Query)).Encode()
	value, explicit, supported := selectionPropertyValue(deck, slide, slide, raw, label, nil, common)
	q := mustQuery(authored.Query)
	if common == "text.bold" && q.Has("label-ttf-weight") || (common == "text.bold" || common == "text.italic" || common == "text.underline") && q.Has("label-modern-runs") {
		explicit = true
	}
	return value, explicit, supported
}

func TestSelectionPreparedProjectionScalarParityAndOwnership(t *testing.T) {
	for _, scenario := range []string{"concrete", "legacy", "invalid-query", "master-bound"} {
		t.Run(scenario, func(t *testing.T) {
			d, _, ids := denseLabelCapabilitiesFixture(5)
			d.Slides[0].Elements[1].Query = setQueryValue(d.Slides[0].Elements[1].Query, "label-modern-size", "90")
			d.Slides[0].Elements[2].Query = setQueryValue(d.Slides[0].Elements[2].Query, "label-theme-fg", "accent-2")
			d.Slides[0].Elements[3].Query = setQueryValue(d.Slides[0].Elements[3].Query, "label-text-italic", "0")
			d.Slides[0].Elements[4].Query = setQueryValue(d.Slides[0].Elements[4].Query, "label-gradient-dir", "none")
			if scenario == "legacy" {
				d.Appearance, d.Theme = nil, nil
			} else if scenario == "invalid-query" {
				d.Slides[0].Elements[4].Query += "&invalid=%zz"
			} else if scenario == "master-bound" {
				d.EnsureDefaultMasters()
				master := d.Slides[0].Elements[0]
				master.ID, master.SlotID, master.PlaceholderRole = "master-label", "label-slot", "body"
				d.Masters.Layouts = append(d.Masters.Layouts, MasterLayout{ID: "label-layout", Name: "Labels", Slide: Slide{Elements: []Element{master}}})
				d.Slides[0].LayoutID = "label-layout"
				d.Slides[0].Elements[0].MasterSlotID = "label-slot"
				d.Slides[0].Elements[0].Query = "label-theme-fg=accent-3&label-text-underline=1"
			}
			// Include ordinary text and a shape without a label: all non-label
			// property values also use the request's pre-parsed query inputs.
			d.Slides[0].Elements = append(d.Slides[0].Elements, Element{ID: "plain-text", Kind: "text", Text: "Plain", Query: "render=truetype&modern-font=sans&modern-size=80&text-align=right"}, Element{ID: "empty-shape", Kind: "shape", Query: "shape=circle&width=30&height=20"})
			ids = append(ids, "plain-text", "empty-shape")
			resolved := d.ResolveSlide(0, false)
			before := cloneDeck(d)
			caps := buildSelectionCapabilities(d, d.Slides[0], resolved, ids, "", 1)
			projected := map[string]Element{}
			for _, element := range resolved.Elements {
				projected[element.ID] = element
			}
			for _, property := range selectionPropertyIDs {
				state := selectionPropertyState{State: "unavailable", EligibleIDs: []string{}, TotalCount: len(ids)}
				for _, element := range d.Slides[0].Elements {
					var value interface{}
					var explicit, supported bool
					if labelCommonProperty(property) != "" {
						value, explicit, supported = scalarLabelPropertyReference(d, resolved, element, projected[element.ID], property)
					} else {
						value, explicit, supported = selectionPropertyValue(d, d.Slides[0], resolved, element, projected[element.ID], nil, property)
					}
					if !supported {
						continue
					}
					state.EligibleIDs = append(state.EligibleIDs, element.ID)
					provenance := "inherited"
					if explicit {
						provenance = "explicit"
					}
					if state.State == "unavailable" {
						state.State, state.Value, state.Provenance = "value", value, provenance
					} else {
						if !reflect.DeepEqual(value, state.Value) {
							state.State = "mixed"
						}
						if state.Provenance != provenance {
							state.Provenance = "mixed"
						}
					}
					if _, mixed := value.(mixedSelectionValue); mixed {
						state.State = "mixed"
					}
				}
				if state.State == "mixed" {
					state.Value = nil
				}
				actual := caps.Properties[property]
				if actual.State != state.State || actual.Provenance != state.Provenance || !reflect.DeepEqual(actual.Value, state.Value) || !reflect.DeepEqual(actual.EligibleIDs, state.EligibleIDs) {
					t.Fatal("prepared/scalar property mismatch", property, state, actual)
				}
			}
			if !reflect.DeepEqual(before, d) {
				t.Fatal("capability preparation mutated authored state")
			}
		})
	}
}

func TestSelectionPreparedLabelsRequestIsolation(t *testing.T) {
	d, resolved, ids := denseLabelCapabilitiesFixture(3)
	initial := buildSelectionCapabilities(d, d.Slides[0], resolved, ids, "", 1)
	var work sync.WaitGroup
	for i := 0; i < 8; i++ {
		work.Add(1)
		go func() {
			defer work.Done()
			if got := buildSelectionCapabilities(d, d.Slides[0], resolved, ids, "", 1); !reflect.DeepEqual(initial, got) {
				t.Error("concurrent request reused another capability snapshot")
			}
		}()
	}
	work.Wait()
	changed := cloneDeck(d)
	for i := range changed.Slides[0].Elements {
		changed.Slides[0].Elements[i].Query = setQueryValue(changed.Slides[0].Elements[i].Query, "label-modern-size", strconv.Itoa(80))
	}
	updated := buildSelectionCapabilities(changed, changed.Slides[0], changed.ResolveSlide(0, false), ids, "", 2)
	if updated.Properties["label.size"].Value != float64(80) || !reflect.DeepEqual(initial, buildSelectionCapabilities(d, d.Slides[0], resolved, ids, "", 1)) {
		t.Fatal("label preparation reused stale or shared mutable document state")
	}
}

func BenchmarkSelectionLabelCapabilities(b *testing.B) {
	for _, count := range []int{1, 50} {
		b.Run(fmt.Sprintf("labels-%d", count), func(b *testing.B) {
			d, resolved, ids := denseLabelCapabilitiesFixture(count)
			benchmarkLabelCapabilities = buildSelectionCapabilities(d, d.Slides[0], resolved, ids, "", 1)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				benchmarkLabelCapabilities = buildSelectionCapabilities(d, d.Slides[0], resolved, ids, "", 1)
			}
		})
	}
}

func TestSelectionLabelsDenseBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_SELECTION_EVIDENCE")
	if root == "" {
		t.Skip("browser fixture output not requested")
	}
	d, _, _ := denseLabelCapabilitiesFixture(50)
	for i := range d.Slides[0].Elements {
		e := &d.Slides[0].Elements[i]
		q := mustQuery(e.Query)
		q.Set("left", strconv.Itoa(4+i%10*24))
		q.Set("top", strconv.Itoa(2+i/10*10))
		q.Set("width", "21")
		q.Set("height", "8")
		q.Set("fg", "#0055aa")
		data, _ := decodeShapeLabelData(q.Get("shape-label"))
		data.Text, data.Query = fmt.Sprintf("Node %d", i+1), "render=truetype&modern-font=mono&modern-size=24"
		raw, _ := json.Marshal(data)
		q.Set("shape-label", base64.StdEncoding.EncodeToString(raw))
		e.Query = q.Encode()
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "dense-labels.md"), d); err != nil {
		t.Fatal(err)
	}
}
