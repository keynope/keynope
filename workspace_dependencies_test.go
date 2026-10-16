package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func workspaceAsset(c color.RGBA) DeckAsset {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: buf.Bytes()}
}

func workspaceConsumerFixture() Deck {
	d := Deck{Appearance: &DeckAppearance{Version: 3}, Theme: builtinDeckTheme("clean-light"), Objects: &DeckObjects{Version: 1}, Assets: map[string]DeckAsset{
		"asset-a":  workspaceAsset(color.RGBA{R: 220, G: 90, B: 60, A: 255}),
		"asset-b":  workspaceAsset(color.RGBA{R: 60, G: 160, B: 210, A: 255}),
		"backdrop": workspaceAsset(color.RGBA{R: 40, G: 100, B: 70, A: 255}),
		"unused":   workspaceAsset(color.RGBA{R: 170, G: 110, B: 200, A: 255}),
	}}
	d.EnsureDefaultMasters()
	d.Masters.Base.Slide = Slide{PageNumber: pageNumberHide, Elements: []Element{{ID: "base-text", Kind: "text", Text: "Inherited", Query: "top=1&left=1&render=truetype&modern-size=30"}}}
	d.Masters.Layouts = []MasterLayout{
		{ID: "alpha", Name: "Alpha", Slide: Slide{BG: "#e0eaf4", BGSet: true, BackgroundMediaSet: true, BackgroundMedia: &slideBackgroundMedia{AssetID: "backdrop", Fit: "cover", Opacity: .15}}},
		{ID: "beta", Name: "Beta", Slide: Slide{BG: "#f4eade", BGSet: true}},
		{ID: "unused-layout", Name: "Unused", Slide: Slide{}},
	}
	for i, layout := range []string{"alpha", "beta", "", "alpha", ""} {
		s := Slide{ID: fmt.Sprint("slide-", i), LayoutID: layout, PageNumber: pageNumberHide, Elements: []Element{{ID: fmt.Sprint("text-", i), Kind: "text", Text: fmt.Sprint("Page ", i+1), Query: "render=truetype&top=7&left=4&modern-size=48"}}}
		if i < 3 {
			asset := "asset-a"
			if i == 2 {
				asset = "asset-b"
			}
			s.Elements = append(s.Elements, Element{ID: fmt.Sprint("image-", i), Kind: "image", AssetID: asset, Query: "element-style=modern&top=15&left=8&width=50&height=20"})
		}
		if i == 3 {
			s.BG, s.BGSet = "#223344", true
			s.BackgroundMediaSet = true
		}
		if i == 4 {
			s.PageNumber = pageNumberShow
		}
		d.Slides = append(d.Slides, s)
	}
	data, _ := newTableData(2, 2)
	data.Cells[0][0].Text = "Data"
	d.Objects.Tables = map[string]TableData{"shared-table": data}
	for _, i := range []int{0, 2} {
		d.Slides[i].Elements = append(d.Slides[i].Elements, Element{ID: fmt.Sprint("table-", i), Kind: "table", ObjectID: "shared-table", Query: "top=15&left=80&width=90&height=20"})
	}
	d.Objects.Builds = map[string]SlideBuild{}
	return d
}

func assertWorkspaceConsumerRender(t *testing.T, c *workspacePageCache, d Deck, want []int) {
	t.Helper()
	before := cloneDeck(d)
	got, err := c.render(d, false, 0, 245, 56)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := editorWorkspacePages(d, false, 0, 245, 56)
	if err != nil || !reflect.DeepEqual(got, fresh) {
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(fresh)
		at := 0
		for at < len(a) && at < len(b) && a[at] == b[at] {
			at++
		}
		start := max(0, at-100)
		t.Fatalf("consumer-scoped pages differ err=%v jsonEqual=%v first=%d cached=%s fresh=%s", err, bytes.Equal(a, b), at, a[start:min(len(a), at+200)], b[start:min(len(b), at+200)])
	}
	if !reflect.DeepEqual(c.changed, want) {
		t.Fatalf("rebuilt pages %v, want %v", c.changed, want)
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("dependency comparison mutated authored deck")
	}
}

func TestWorkspaceIndexedAssetPayloadMasterConsumers(t *testing.T) {
	d := workspaceConsumerFixture()
	var c workspacePageCache
	assertWorkspaceConsumerRender(t, &c, d, []int{0, 1, 2, 3, 4})
	checks := []struct {
		name string
		edit func()
		want []int
	}{
		{"unused asset", func() { d.Assets["unused"] = workspaceAsset(color.RGBA{R: 240, A: 255}) }, nil},
		{"shared image", func() { d.Assets["asset-a"] = workspaceAsset(color.RGBA{G: 230, A: 255}) }, []int{0, 1}},
		{"image removal", func() { delete(d.Assets, "asset-b") }, []int{2}},
		{"image reappearance", func() { d.Assets["asset-b"] = workspaceAsset(color.RGBA{B: 230, A: 255}) }, []int{2}},
		{"inherited backdrop", func() { d.Assets["backdrop"] = workspaceAsset(color.RGBA{R: 180, G: 70, A: 255}) }, []int{0}},
		{"unused layout", func() { d.Masters.Layouts[2].Slide.BG = "#113355" }, nil},
		{"used layout overridden elsewhere", func() { d.Masters.Layouts[0].Slide.BG = "#ffeeaa" }, []int{0}},
		{"shared table data", func() {
			data := d.Objects.Tables["shared-table"]
			data.Cells[1][1].Text = "Updated"
			d.Objects.Tables["shared-table"] = data
		}, []int{0, 2}},
		{"unused payload", func() { data, _ := newTableData(1, 1); d.Objects.Tables["unused-table"] = data }, nil},
		{"base text", func() { d.Masters.Base.Slide.Elements[0].Text = "New master text" }, []int{0, 1, 3}},
		{"notes and private results", func() {
			d.Slides[0].Notes = "Private notes"
			d.Slides[0].EngagementResult = &EngagementResult{ActivityID: "private"}
			d.Masters.Base.Slide.Notes = "Private master notes"
		}, nil},
		{"page number template", func() {
			d.Masters.Base.Slide.Elements = append(d.Masters.Base.Slide.Elements, Element{ID: "page-template", Kind: "page-number", Query: "bottom=1&right=1&fg=%23ff0055"})
		}, []int{4}},
		{"local build", func() {
			d.Objects.Builds["slide-2"] = SlideBuild{Version: 1, Transition: SlideTransition{Effect: "push", DurationMS: 500}}
		}, []int{2}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) { check.edit(); assertWorkspaceConsumerRender(t, &c, d, check.want) })
	}
	if !reflect.DeepEqual(c.consumers.assets["asset-a"], workspaceConsumers{0: true, 1: true}) || !reflect.DeepEqual(c.consumers.assets["backdrop"], workspaceConsumers{0: true}) || !reflect.DeepEqual(c.consumers.payloads[workspacePayloadReference{"table", "shared-table"}], workspaceConsumers{0: true, 2: true}) {
		t.Fatal("reverse consumers incorrect")
	}
}

func TestWorkspaceConsumerReassignmentAndFutureFallbacks(t *testing.T) {
	d := workspaceConsumerFixture()
	var c workspacePageCache
	assertWorkspaceConsumerRender(t, &c, d, []int{0, 1, 2, 3, 4})
	d.Slides[0].LayoutID = "beta"
	d.Slides[0].Elements[1].AssetID = "asset-b"
	assertWorkspaceConsumerRender(t, &c, d, []int{0})
	if c.consumers.assets["asset-a"][0] || c.consumers.layouts["alpha"][0] || c.consumers.assets["backdrop"][0] {
		t.Fatal("reassigned references stayed indexed")
	}
	d.Assets["asset-a"] = workspaceAsset(color.RGBA{R: 240, A: 255})
	assertWorkspaceConsumerRender(t, &c, d, []int{1})
	d.Masters.Layouts[0].Slide.BG = "#bbccdd"
	assertWorkspaceConsumerRender(t, &c, d, nil)
	d.Masters.Layouts[1].Slide.BG = "#bbccdd"
	assertWorkspaceConsumerRender(t, &c, d, []int{0, 1})
	d.Slides[3].LayoutID = "future-layout"
	assertWorkspaceConsumerRender(t, &c, d, []int{3})
	d.Masters.Layouts = append(d.Masters.Layouts, MasterLayout{ID: "future-layout", Name: "New", Slide: Slide{Elements: []Element{{ID: "new-text", Kind: "text", Text: "Appeared", Query: "render=truetype"}}}})
	assertWorkspaceConsumerRender(t, &c, d, []int{3})
	d.Masters.Layouts = d.Masters.Layouts[:3]
	assertWorkspaceConsumerRender(t, &c, d, []int{3})
	d.Extra = &jsonExtensions{"future": json.RawMessage(`true`)}
	assertWorkspaceConsumerRender(t, &c, d, []int{0, 1, 2, 3, 4})
	d.Masters.Layouts[2].Extra = &jsonExtensions{"future": json.RawMessage(`true`)}
	assertWorkspaceConsumerRender(t, &c, d, []int{0, 1, 2, 3, 4})
	d.Slides[2].Elements = append(d.Slides[2].Elements, Element{ID: "unknown", Kind: "future"})
	assertWorkspaceConsumerRender(t, &c, d, []int{2})
	d.Assets["unused"] = workspaceAsset(color.RGBA{R: 150, A: 255})
	assertWorkspaceConsumerRender(t, &c, d, []int{2})
}

func TestWorkspaceIndexEvictionBoundAndReorder(t *testing.T) {
	d := workspaceConsumerFixture()
	for len(d.Slides) < 130 {
		i := len(d.Slides)
		s := cloneSlide(d.Slides[2])
		s.ID = fmt.Sprint("slide-", i)
		s.Elements[0].ID = fmt.Sprint("text-", i)
		d.Slides = append(d.Slides, s)
	}
	var c workspacePageCache
	if _, err := c.render(d, false, 0, 245, 56); err != nil {
		t.Fatal(err)
	}
	if len(c.pages) != 128 || len(c.consumers.base) != 128 || c.consumers.assets["asset-b"][129] {
		t.Fatal("reverse indexes retained evicted page")
	}
	d.Slides[0], d.Slides[1] = d.Slides[1], d.Slides[0]
	assertWorkspaceConsumerRender(t, &c, d, []int{0, 1, 128, 129})
	if !c.consumers.layouts["alpha"][1] || c.consumers.layouts["alpha"][0] {
		t.Fatal("reorder left stale master consumers")
	}
	var total int
	for _, e := range c.pages {
		total += e.bytes
	}
	if total > 64<<20 {
		t.Fatal("input/page budget exceeded")
	}
}

func TestWorkspaceSnapshotRetainsOnlyKnownConsumers(t *testing.T) {
	d := workspaceConsumerFixture()
	d.Objects.Tables["unused-table"] = cloneTable(d.Objects.Tables["shared-table"])
	d.Objects.Charts = map[string]ChartData{"unused-chart": newChartData("column")}
	d.Objects.Builds["slide-0"], d.Objects.Builds["unused-build"] = SlideBuild{Version: 1}, SlideBuild{Version: 1}
	d.Slides[0].Notes = "Private notes"
	d.Slides[0].EngagementResult = archivedLayoutFixture().EngagementResult
	d.Masters.Base.Slide.Notes = "Private master notes"
	d.Masters.Base.Slide.EngagementResult = d.Slides[0].EngagementResult
	var c workspacePageCache
	assertWorkspaceConsumerRender(t, &c, d, []int{0, 1, 2, 3, 4})
	if len(c.previous.Assets) != 3 || len(c.previous.Objects.Tables) != 1 || len(c.previous.Objects.Charts) != 0 || len(c.previous.Objects.Builds) != 1 {
		t.Fatal("unused archive dependencies retained")
	}
	if _, ok := c.previous.Assets["unused"]; ok {
		t.Fatal("unused media retained")
	}
	if c.previous.Slides[0].Notes != "" || c.previous.Slides[0].EngagementResult != nil || c.previous.Masters.Base.Slide.Notes != "" || c.previous.Masters.Base.Slide.EngagementResult != nil {
		t.Fatal("private data retained")
	}
	// Mutating the caller's metadata must not corrupt accepted cache inputs.
	data := d.Objects.Tables["shared-table"]
	data.Cells[0][0].Text = "Edited"
	d.Objects.Tables["shared-table"] = data
	if c.previous.Objects.Tables["shared-table"].Cells[0][0].Text != "Data" {
		t.Fatal("retained payload aliases caller")
	}
	assertWorkspaceConsumerRender(t, &c, d, []int{0, 2})
	// An unknown adapter can depend on the full registry; retain it as before.
	d.Slides[2].Elements = append(d.Slides[2].Elements, Element{ID: "unknown-cache", Kind: "future"})
	assertWorkspaceConsumerRender(t, &c, d, []int{2})
	if len(c.previous.Assets) != len(d.Assets) || len(c.previous.Objects.Tables) != len(d.Objects.Tables) || len(c.previous.Objects.Charts) != len(d.Objects.Charts) {
		t.Fatal("unknown adapter lost conservative inputs")
	}
	d.Assets["unused"] = workspaceAsset(color.RGBA{R: 240, A: 255})
	assertWorkspaceConsumerRender(t, &c, d, []int{2})
}

func TestWorkspaceSnapshotDropsEvictedSlideContents(t *testing.T) {
	d := workspaceConsumerFixture()
	owned := cloneDeckForScene(d)
	resolved := owned.ResolveSlide(0, false)
	entry := workspacePageEntry{refs: workspacePageReferences(owned.Slides[0], resolved)}
	snapshot := workspaceRetainedSnapshot(owned, map[int]workspacePageEntry{0: entry})
	if len(snapshot.Slides) != len(d.Slides) || len(snapshot.Slides[0].Elements) == 0 {
		t.Fatal("retained authored index missing")
	}
	for i := 1; i < len(snapshot.Slides); i++ {
		if !reflect.DeepEqual(snapshot.Slides[i], Slide{}) {
			t.Fatal("evicted slide content retained", i)
		}
	}
	if _, ok := snapshot.Assets["asset-b"]; ok {
		t.Fatal("evicted slide media retained")
	}
	if !reflect.DeepEqual(owned.Slides, cloneDeckForScene(d).Slides) {
		t.Fatal("pruning mutated owned render input")
	}
}

func TestWorkspaceConsumerBrowserFixture(t *testing.T) {
	root := os.Getenv("KEYNOPE_WORKSPACE_CONSUMER_EVIDENCE")
	if root == "" {
		t.Skip("fixture output not requested")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveDeck(filepath.Join(root, "consumers.md"), workspaceConsumerFixture()); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceFallbackPageNumberStableIdentity(t *testing.T) {
	d := workspaceConsumerFixture()
	before := cloneDeck(d)
	a, b := d.ResolveSlide(4, false), d.ResolveSlide(4, false)
	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(d, before) {
		t.Fatal("fallback page number unstable or mutates authored source")
	}
	page, ok := pageNumberElement(a)
	if !ok || page.ID != "keynope-default-page-number-5" {
		t.Fatal("unexpected derived identity", a.Elements)
	}
	d.Masters.Base.Slide.Elements = append(d.Masters.Base.Slide.Elements, defaultPageNumberElement("authored-template"))
	p := d.ResolveSlide(4, false)
	page, ok = pageNumberElement(p)
	if !ok || page.ID != "authored-template" {
		t.Fatal("authored template identity replaced")
	}
}

func BenchmarkWorkspaceIndexedUnusedAsset(b *testing.B) {
	for _, cached := range []bool{false, true} {
		b.Run(fmt.Sprint("cached=", cached), func(b *testing.B) {
			d := workspaceConsumerFixture()
			for len(d.Slides) < 100 {
				s := cloneSlide(d.Slides[2])
				s.ID = fmt.Sprint("slide-", len(d.Slides))
				d.Slides = append(d.Slides, s)
			}
			var c workspacePageCache
			if cached {
				if _, err := c.render(d, false, 0, 245, 56); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				asset := d.Assets["unused"]
				asset.Width = 8 + i%2
				d.Assets["unused"] = asset
				var err error
				if cached {
					_, err = c.render(d, false, 0, 245, 56)
					if len(c.changed) != 0 {
						b.Fatal("unused asset rebuilt pages")
					}
				} else {
					_, err = editorWorkspacePages(d, false, 0, 245, 56)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
