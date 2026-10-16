package main

import (
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestModernMasterSuppressionRoundTrip(t *testing.T) {
	d := strokeInheritanceFixture()
	master := &d.Masters.Layouts[0].Slide.Elements[0]
	master.Kind = "text"
	master.Query = "modern-font=mono&modern-size=160&modern-width=70&modern-line-height=1.6&modern-paragraph-before=12&modern-paragraph-after=8&modern-opacity=0.4&theme-fg=accent1&object-rotation=15&object-offset-x=0.25&object-offset-y=0.5&width=40&height=12"
	child := &d.Slides[0].Elements[0]
	child.Kind, child.Text = "text", "Explicit inheritance suppression"
	keys := []string{"modern-font", "modern-size", "modern-width", "modern-line-height", "modern-paragraph-before", "modern-paragraph-after", "modern-opacity", "theme-fg", "object-rotation", "object-offset-x", "object-offset-y"}
	sort.Strings(keys)
	child.Query = "left=10&top=5&master-clear=" + url.QueryEscape(strings.Join(keys, ","))
	before := cloneDeck(d)
	encoded, err := serializeDeck("Overrides.md", d)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := parseDeckData("Overrides.md", encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d, before) {
		t.Fatal("serialization changed source")
	}
	q, _ := url.ParseQuery(reopened.Slides[0].Elements[0].Query)
	if q.Get("master-clear") != strings.Join(keys, ",") {
		t.Fatalf("lost modern suppression markers: %s", q.Get("master-clear"))
	}
	for _, e := range reopened.ResolveSlide(0, false).Elements {
		if e.ID != "instance" {
			continue
		}
		q, _ := url.ParseQuery(e.Query)
		for _, key := range keys {
			if q.Has(key) {
				t.Fatalf("reopening restored suppressed %s", key)
			}
		}
		return
	}
	t.Fatal("missing reopened placeholder instance")
}

func TestMasterStyleSuppressionRegistry(t *testing.T) {
	for property, control := range connectorSelectionControls {
		if !isMasterOverrideQueryKey(control.key) {
			t.Fatalf("%s cannot persist suppression of %s", property, control.key)
		}
	}
	for property, key := range objectStyleKeys {
		if key != "" && !isMasterOverrideQueryKey(key) {
			t.Fatalf("%s cannot persist suppression of %s", property, key)
		}
	}
	for _, key := range []string{"", "element-id", "object-id", "master-clear", "participant-inherited", "connector-from"} {
		if isMasterOverrideQueryKey(key) {
			t.Fatalf("non-style field accepted for suppression: %q", key)
		}
	}
}
