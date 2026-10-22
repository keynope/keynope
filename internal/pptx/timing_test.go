package pptx

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func timingFixture() Presentation {
	objects := []Object{}
	for _, id := range []string{"appear", "fade", "wipe", "zoom", "together"} {
		objects = append(objects, Object{ID: id, Kind: "shape", X: 100, Y: 100, Width: 300, Height: 200, Fill: "#0044AA", Shape: "rect"})
	}
	return Presentation{Slides: []Slide{{Hidden: true, Objects: objects, Timeline: &Timeline{Transition: Transition{Effect: "push", Direction: "up", DurationMS: 1750}, Steps: []BuildStep{
		{TargetIDs: []string{"appear"}, Effect: "appear", DurationMS: 100, Advance: "after", DelayMS: 200},
		{TargetIDs: []string{"fade", "together"}, Effect: "fade", DurationMS: 500, Advance: "click"},
		{TargetIDs: []string{"wipe"}, Effect: "wipe", Direction: "right", DurationMS: 350, Advance: "after", DelayMS: 150},
		{TargetIDs: []string{"zoom"}, Effect: "zoom", DurationMS: 600, Advance: "after", DelayMS: 80},
	}}}}}
}
func timingPackage(t *testing.T, p Presentation) []byte {
	t.Helper()
	var b bytes.Buffer
	report, err := Write(context.Background(), &b, p)
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	return b.Bytes()
}

func TestTimelineCorpus(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "pptx")
	data, err := os.ReadFile(filepath.Join(root, "keynope", "timing-native.pptx"))
	if err != nil {
		t.Fatal(err)
	}
	expectation, err := os.ReadFile(filepath.Join(root, "expected", "timing-native.json"))
	if err != nil {
		t.Fatal(err)
	}
	var want struct {
		Hidden       bool
		ObjectIDs    []string
		Timeline     Timeline
		WarningCodes []string
	}
	if err := json.Unmarshal(expectation, &want); err != nil {
		t.Fatal(err)
	}
	p, report, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	codes := []string{}
	for _, warning := range report.Warnings {
		codes = append(codes, warning.Code)
	}
	if !reflect.DeepEqual(codes, want.WarningCodes) || len(p.Slides) != 1 {
		t.Fatal(report)
	}
	ids := []string{}
	for _, object := range p.Slides[0].Objects {
		ids = append(ids, object.ID)
	}
	if p.Slides[0].Hidden != want.Hidden || !reflect.DeepEqual(ids, want.ObjectIDs) || !reflect.DeepEqual(p.Slides[0].Timeline, &want.Timeline) {
		t.Fatal("static timing corpus changed semantics", p.Slides[0])
	}
}
func TestTimelineNativeXMLRoundTripAndShapeIDs(t *testing.T) {
	input := timingFixture()
	data := timingPackage(t, input)
	parts := packageParts(t, data)
	if !bytes.Contains(parts["ppt/slides/slide1.xml"], []byte(`show="0"`)) || bytes.Contains(parts["ppt/presentation.xml"], []byte(`show="0"`)) {
		t.Fatal("hidden flag is not on the slide root")
	}
	got, report, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil || len(report.Warnings) != 0 {
		t.Fatal(err, report)
	}
	if !got.Slides[0].Hidden || got.Slides[0].Timeline == nil {
		t.Fatal("hidden/timeline missing")
	}
	expected := *input.Slides[0].Timeline
	expected.Steps = append([]BuildStep(nil), expected.Steps...)
	ids := map[string]string{}
	for i, o := range input.Slides[0].Objects {
		ids[o.ID] = got.Slides[0].Objects[i].ID
	}
	for i, step := range expected.Steps {
		expected.Steps[i].Direction = timingDirection(step.Direction)
		expected.Steps[i].TargetIDs = nil
		for _, id := range step.TargetIDs {
			expected.Steps[i].TargetIDs = append(expected.Steps[i].TargetIDs, ids[id])
		}
	}
	if !reflect.DeepEqual(expected, *got.Slides[0].Timeline) {
		t.Fatalf("timeline differs\n%#v\n%#v", expected, *got.Slides[0].Timeline)
	}
	if output := os.Getenv("KEYNOPE_PPTX_TIMING_EVIDENCE"); output != "" {
		if err := os.MkdirAll(output, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "timing-native.pptx"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}
}
func TestTimelineTargetOmissionsAndFallbacks(t *testing.T) {
	input := timingFixture().Slides[0]
	ids := map[string]int{"appear": 2, "fade": 3, "together": 0, "wipe": 4, "zoom": 5}
	input.Timeline.Steps[0].TargetIDs = append(input.Timeline.Steps[0].TargetIDs, "missing")
	data, warnings := timelineXML(input.Timeline, ids, input.Objects)
	if len(warnings) != 2 || strings.Contains(data, `spid="0"`) {
		t.Fatal("unsafe/missing target not reported", warnings)
	}
	input.Timeline.Steps[0].Effect = "custom"
	input.Timeline.Steps[0].DurationMS = 0
	_, warnings = timelineXML(input.Timeline, ids, input.Objects)
	codes := map[string]bool{}
	for _, w := range warnings {
		codes[w.Code] = true
	}
	if !codes["build-effect-fallback"] || !codes["build-duration-rounded"] {
		t.Fatal(warnings)
	}
}
func TestTimelineRejectsComplexOrMalformedTreesWithoutHidingObjects(t *testing.T) {
	parts := packageParts(t, timingPackage(t, timingFixture()))
	original := string(parts["ppt/slides/slide1.xml"])
	for name, alter := range map[string]func(string) string{
		"missing target": func(s string) string { return strings.ReplaceAll(s, `spid="2"`, `spid="9999"`) },
		"repeat": func(s string) string {
			return strings.Replace(s, `nodeType="clickEffect"`, `repeatCount="2000" nodeType="clickEffect"`, 1)
		},
		"exit":           func(s string) string { return strings.Replace(s, `presetClass="entr"`, `presetClass="exit"`, 1) },
		"custom effect":  func(s string) string { return strings.Replace(s, `filter="fade"`, `filter="blinds(horizontal)"`, 1) },
		"partial scale":  func(s string) string { return strings.Replace(s, `x="86000"`, `x="50000"`, 1) },
		"duplicate node": func(s string) string { return strings.Replace(s, `id="6" dur="1"`, `id="1" dur="1"`, 1) },
		"paragraph build": func(s string) string {
			return strings.Replace(s, `<p:spTgt spid="2"/>`, `<p:spTgt spid="2"><p:txEl/></p:spTgt>`, 1)
		},
		"navigation trigger":     func(s string) string { return strings.Replace(s, `evt="onNext"`, `evt="onClick"`, 1) },
		"negative delay":         func(s string) string { return strings.Replace(s, `delay="200"`, `delay="-1"`, 1) },
		"numeric duplicate node": func(s string) string { return strings.Replace(s, `id="6" dur="1"`, `id="01" dur="1"`, 1) },
		"time acceleration": func(s string) string {
			return strings.Replace(s, `nodeType="clickEffect"`, `accel="50000" nodeType="clickEffect"`, 1)
		},
		"speed multiplier": func(s string) string {
			return strings.Replace(s, `nodeType="mainSeq"`, `spd="200000" nodeType="mainSeq"`, 1)
		},
		"extra time root": func(s string) string {
			return strings.Replace(s, `</p:tnLst>`, `<p:par><p:cTn id="99999" dur="indefinite"/></p:par></p:tnLst>`, 1)
		},
		"click target event": func(s string) string {
			return strings.Replace(s, `<p:cond delay="indefinite"/>`, `<p:cond delay="indefinite"><p:tgtEl><p:sldTgt/></p:tgtEl></p:cond>`, 1)
		},
		"inconsistent build trigger": func(s string) string { return strings.Replace(s, `nodeType="clickEffect"`, `nodeType="withEffect"`, 1) },
		"scale frame not content":    func(s string) string { return strings.Replace(s, `zoomContents="1"`, `zoomContents="0"`, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte(alter(original))
			root, err := parseXML(data, 128)
			if err != nil {
				t.Fatal(err)
			}
			objects := []Object{}
			for i := 2; i <= 6; i++ {
				objects = append(objects, Object{ID: fmtInt(i)})
			}
			timeline, warnings := readTimeline(context.Background(), data, root, objects)
			if timeline == nil || len(timeline.Steps) != 0 || len(warnings) == 0 || warnings[0].Code != "timing-unsupported" {
				t.Fatal("unsupported tree accepted", timeline, warnings)
			}
			if timeline.Transition.Effect != "push" {
				t.Fatal("lost supported transition")
			}
		})
	}
}

func TestTimelineTransitionMatrix(t *testing.T) {
	for _, effect := range []string{"cut", "fade", "push", "wipe", "dip-black"} {
		for _, direction := range []string{"left", "right", "up", "down"} {
			t.Run(effect+"-"+direction, func(t *testing.T) {
				p := timingFixture()
				p.Slides[0].Timeline = &Timeline{Transition: Transition{Effect: effect, Direction: direction, DurationMS: 1371}}
				data := timingPackage(t, p)
				got, report, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
				if err != nil || len(report.Warnings) != 0 {
					t.Fatal(err, report)
				}
				if effect == "cut" {
					if got.Slides[0].Timeline != nil {
						t.Fatal("cut unexpectedly created timing")
					}
					return
				}
				tr := got.Slides[0].Timeline.Transition
				if tr.Effect != effect || tr.DurationMS != 1371 || effect != "fade" && effect != "dip-black" && tr.Direction != direction {
					t.Fatal(tr)
				}
			})
		}
	}
}

func TestTVSwitchTransitionFallback(t *testing.T) {
	timeline := &Timeline{Transition: Transition{Effect: "tv-switch", DurationMS: 180}}
	xml, warnings := timelineXML(timeline, nil, nil)
	if !strings.Contains(xml, "<p:fade") || len(warnings) != 1 || warnings[0].Code != "transition-fallback" {
		t.Fatalf("unexpected TV switching export: %s %#v", xml, warnings)
	}
	p := timingFixture()
	p.Slides[0].Timeline = timeline
	var output bytes.Buffer
	exportReport, err := Write(context.Background(), &output, p)
	if err != nil || len(exportReport.Warnings) != 1 || exportReport.Warnings[0].Code != "transition-fallback" {
		t.Fatal("missing TV switching export warning", err, exportReport)
	}
	data := output.Bytes()
	got, report, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil || len(report.Warnings) != 0 || got.Slides[0].Timeline == nil || got.Slides[0].Timeline.Transition.Effect != "fade" {
		t.Fatal("fallback is not valid, editable PowerPoint timing", err, report)
	}
}

func TestTimelineLongAutomaticChainAndBounds(t *testing.T) {
	p := timingFixture()
	p.Slides[0].Timeline.Steps = []BuildStep{}
	for _, object := range p.Slides[0].Objects {
		p.Slides[0].Timeline.Steps = append(p.Slides[0].Timeline.Steps, BuildStep{TargetIDs: []string{object.ID}, Effect: "appear", Advance: "after", DelayMS: 3600000, DurationMS: 60000})
	}
	data := timingPackage(t, p)
	got, report, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil || len(report.Warnings) != 0 || len(got.Slides[0].Timeline.Steps) != 5 {
		t.Fatal(err, report)
	}
	for _, step := range got.Slides[0].Timeline.Steps {
		if step.DelayMS != 3600000 || step.DurationMS != 60000 {
			t.Fatal(step)
		}
	}
	p.Slides[0].Timeline.Steps[0].TargetIDs = make([]string, 1001)
	xml, warnings := timelineXML(p.Slides[0].Timeline, map[string]int{}, p.Slides[0].Objects)
	if strings.Contains(xml, "<p:timing>") || len(warnings) != 1 || warnings[0].Code != "timing-too-large" {
		t.Fatal(warnings)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Read(ctx, bytes.NewReader(data), int64(len(data)), Options{}); err != context.Canceled {
		t.Fatal("cancelled import accepted", err)
	}
}

func TestTimelineDecoderBounds(t *testing.T) {
	for _, depth := range []int{33, 100} {
		data := []byte(`<p:sld xmlns:p="` + presentationNamespace + `"><p:timing>` + strings.Repeat(`<p:par>`, depth) + strings.Repeat(`</p:par>`, depth) + `</p:timing></p:sld>`)
		root, err := parseXML(data, 128)
		if err != nil {
			t.Fatal(err)
		}
		timeline, warnings := readTimeline(context.Background(), data, root, nil)
		if timeline != nil || len(warnings) != 1 || warnings[0].Code != "timing-unsupported" {
			t.Fatal("unbounded timing tree accepted", timeline, warnings)
		}
	}
	d := xml.NewDecoder(strings.NewReader(`<p:timing xmlns:p="` + presentationNamespace + `"><p:par/></p:timing>`))
	start, err := d.Token()
	if err != nil {
		t.Fatal(err)
	}
	count := 99999
	if _, err := timingTree(context.Background(), d, start.(xml.StartElement), 0, &count); err == nil {
		t.Fatal("timing node bound was ignored")
	}
}

func TestTimelineXMLSizeBound(t *testing.T) {
	timeline := &Timeline{}
	ids := map[string]int{}
	for i := 0; i < 8; i++ {
		step := BuildStep{Effect: "zoom", Advance: "click", DurationMS: 500}
		for j := 0; j < 1000; j++ {
			id := strconv.Itoa(i*1000 + j + 2)
			ids[id] = i*1000 + j + 2
			step.TargetIDs = append(step.TargetIDs, id)
		}
		timeline.Steps = append(timeline.Steps, step)
	}
	data, warnings := timelineXML(timeline, ids, nil)
	if strings.Contains(data, "<p:timing>") || len(warnings) != 1 || warnings[0].Code != "timing-too-large" {
		t.Fatal("oversized timing serialized", len(data), warnings)
	}
}

func TestTimelineDelayBeyond32BitClock(t *testing.T) {
	// Go/WASM has 32-bit ints. Absolute sequence offsets still have to survive
	// 600 bounded steps whose cumulative delay exceeds MaxInt32.
	p := Presentation{Slides: []Slide{{Timeline: &Timeline{}}}}
	for i := 0; i < 600; i++ {
		id := strconv.Itoa(i + 2)
		p.Slides[0].Objects = append(p.Slides[0].Objects, Object{ID: id, Kind: "shape", Shape: "rect", Width: 10, Height: 10, Fill: "#0044aa"})
		p.Slides[0].Timeline.Steps = append(p.Slides[0].Timeline.Steps, BuildStep{TargetIDs: []string{id}, Effect: "appear", DurationMS: 60000, Advance: "after", DelayMS: 3600000})
	}
	data := timingPackage(t, p)
	got, report, err := Read(context.Background(), bytes.NewReader(data), int64(len(data)), Options{})
	if err != nil || len(report.Warnings) != 0 || len(got.Slides[0].Timeline.Steps) != 600 {
		t.Fatal(err, report)
	}
	for _, step := range got.Slides[0].Timeline.Steps {
		if step.DelayMS != 3600000 {
			t.Fatal(step)
		}
	}
}
func fmtInt(value int) string { return strconv.Itoa(value) }
func TestTimelineDurationInferenceAndInheritedIDIsolation(t *testing.T) {
	parts := packageParts(t, timingPackage(t, timingFixture()))
	data := bytes.Replace(parts["ppt/slides/slide1.xml"], []byte(`presetSubtype="0" dur="500"`), []byte(`presetSubtype="0"`), -1)
	root, err := parseXML(data, 128)
	if err != nil {
		t.Fatal(err)
	}
	objects := []Object{{ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"}, {ID: "6"}, {ID: "3", Inherited: true}}
	timeline, warnings := readTimeline(context.Background(), data, root, objects)
	if len(warnings) != 0 || len(timeline.Steps) != 4 || timeline.Steps[1].DurationMS != 500 {
		t.Fatal("inherited ID/inferred duration mishandled", timeline, warnings)
	}
}
