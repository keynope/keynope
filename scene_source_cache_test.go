package main

import (
	"encoding/base64"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestSceneSourceCache(t *testing.T) {
	c := sceneSourceCache{limit: 100}
	data := []byte("first")
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	if c.uri("image/png", data) != want || c.uri("image/png", append([]byte(nil), data...)) != want || c.order.Len() != 1 {
		t.Fatal("identical bytes not reused")
	}
	data[0] = 'F'
	if c.uri("image/png", data) == want {
		t.Fatal("mutated source was stale")
	}
	if !strings.HasPrefix(c.uri("image/jpeg", data), "data:image/jpeg;") {
		t.Fatal("MIME omitted from key")
	}
	if c.bytes > c.limit {
		t.Fatal("cache exceeded byte budget")
	}
	before := c.order.Len()
	if len(c.uri("image/png", make([]byte, 1000))) < 1000 || c.order.Len() != before {
		t.Fatal("oversized source should bypass storage")
	}
	disabled := sceneSourceCache{}
	if disabled.uri("image/png", data) == "" || disabled.order.Len() != 0 {
		t.Fatal("disabled cache")
	}
	tiny := sceneSourceCache{limit: 1 << 20}
	for i := 0; i < 1100; i++ {
		tiny.uri("x", []byte(strconv.Itoa(i)))
	}
	if tiny.order.Len() != 1024 || len(tiny.entries) != 1024 {
		t.Fatal("small source entry count is not bounded")
	}
}

func TestSceneSourceCacheLRUAndConcurrentAccess(t *testing.T) {
	c := sceneSourceCache{limit: 50}
	c.uri("x", []byte("a"))
	c.uri("x", []byte("b"))
	c.uri("x", []byte("a"))
	c.uri("x", []byte("c"))
	if c.order.Len() != 2 || c.order.Back().Value.(sceneSourceEntry).uri != "data:x;base64,YQ==" {
		t.Fatal("recently used source was evicted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if c.uri("x", []byte("a")) != "data:x;base64,YQ==" {
					t.Error("concurrent encoding mismatch")
				}
			}
		}()
	}
	wg.Wait()
	if c.bytes > c.limit {
		t.Fatal("concurrent cache exceeded budget")
	}
}

func BenchmarkSceneSourceEncoding(b *testing.B) {
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i)
	}
	for _, cached := range []bool{false, true} {
		name := "uncached"
		if cached {
			name = "cached"
		}
		b.Run(name, func(b *testing.B) {
			c := sceneSourceCache{limit: 4 << 20}
			c.uri("image/png", data)
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var uri string
				if cached {
					uri = c.uri("image/png", data)
				} else {
					uri = "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
				}
				if len(uri) == 0 {
					b.Fatal("empty source")
				}
			}
		})
	}
}
