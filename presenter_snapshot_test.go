package main

import "testing"

func TestPresenterSnapshotUsesUpdatedPagesInSlideOrder(t *testing.T) {
	p := &presenterCompanion{
		html: "original HTML remains unchanged after a slide edit",
		pages: map[int][]exportPage{
			1: {{Slide: 1, Page: 0, FG: "updated second slide"}},
			0: {{Slide: 0, Page: 0, FG: "updated first slide"}, {Slide: 0, Page: 1}},
		},
	}
	p.state.DeckVersion = 9
	pages, version := p.slideSnapshot()
	if version != 9 || len(pages) != 3 {
		t.Fatalf("snapshot version=%d pages=%d", version, len(pages))
	}
	if pages[0].FG != "updated first slide" || pages[1].Page != 1 || pages[2].FG != "updated second slide" {
		t.Fatalf("snapshot did not retain current slide/page order: %+v", pages)
	}
	pages[0].FG = "changed copy"
	if p.pages[0][0].FG != "updated first slide" {
		t.Fatal("snapshot aliases cached page slice")
	}
}
