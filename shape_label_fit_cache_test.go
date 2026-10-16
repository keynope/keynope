package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestShapeLabelFitCacheColdParityAndInputs(t *testing.T) {
	corpus := powerupProofCorpus(t)
	var cache shapeLabelFitCache
	for _, deck := range corpus {
		for _, slide := range deck.Slides {
			for _, original := range slide.Elements {
				if original.Kind != "shape" {
					continue
				}
				for _, size := range [][2]int{{245, 56}, {160, 90}} {
					changedLabel, _ := json.Marshal(shapeLabelData{Text: "A much longer\nupdated label", Query: "modern-font=mono&modern-size=70&render=truetype"})
					queries := []string{original.Query, setQueryValue(original.Query, "width", "3"), setQueryValue(original.Query, "height", "2"), setQueryValue(original.Query, "left_pct", "0.51"), setQueryValue(original.Query, "label-modern-size", "128"), setQueryValue(original.Query, "label-modern-font", "c64"), setQueryValue(original.Query, "label-ttf-weight", "bold"), setQueryValue(original.Query, "shape", "diamond"), setQueryValue(original.Query, "shape-label", base64.StdEncoding.EncodeToString(changedLabel)), setQueryValue(original.Query, "shape-label", "malformed")}
					for _, query := range queries {
						cold, warm := original, original
						cold.Query, warm.Query = query, query
						fitShapeLabelUncached(&cold, size[0], size[1])
						cache.fit(&warm, size[0], size[1])
						if !reflect.DeepEqual(cold, warm) {
							t.Fatal("cached fit differs from cold shape")
						}
						again := original
						again.Query = query
						if !cache.fit(&again, size[0], size[1]) || !reflect.DeepEqual(cold, again) {
							t.Fatal("fit reuse changed result")
						}
					}
				}
			}
		}
	}
	plain := Element{Kind: "text", Query: "untouched"}
	if cache.fit(&plain, 245, 56) || plain.Query != "untouched" {
		t.Fatal("non-shape modified")
	}
}

func TestShapeLabelFitCacheConcurrentOwnershipAndBounds(t *testing.T) {
	var cache shapeLabelFitCache
	original := powerupProofCorpus(t)["diagrams.md"].Slides[0].Elements[0]
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e := original; cache.fit(&e, 245, 56); e.Query = "caller edit" }()
	}
	wg.Wait()
	cold, warm := original, original
	fitShapeLabelUncached(&cold, 245, 56)
	if !cache.fit(&warm, 245, 56) || !reflect.DeepEqual(cold, warm) {
		t.Fatal("caller changed shared fit")
	}
	for i := 0; i < 2100; i++ {
		e := Element{Kind: "shape", Query: fmt.Sprintf("shape=square&future-extension=%d", i)}
		cache.fit(&e, 245, 56)
	}
	if len(cache.queries) > 2048 || cache.bytes > 4<<20 {
		t.Fatal("unbounded label-fit cache")
	}
}
