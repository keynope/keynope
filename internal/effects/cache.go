package effects

import "sync"

type gradientKey struct {
	style, start, end string
	width, height     float64
}

var gradientCache = struct {
	sync.Mutex
	entries map[gradientKey]string
	bytes   int
}{}

func cachedGradient(key gradientKey, render func() string) string {
	gradientCache.Lock()
	source, ok := gradientCache.entries[key]
	gradientCache.Unlock()
	if ok {
		return source
	}
	source = render()
	if source == "" || len(source) > 2<<20 {
		return source
	}
	gradientCache.Lock()
	defer gradientCache.Unlock()
	if prior, ok := gradientCache.entries[key]; ok {
		return prior
	}
	if gradientCache.entries == nil || len(gradientCache.entries) >= 32 || gradientCache.bytes+len(source) > 8<<20 {
		gradientCache.entries = map[gradientKey]string{}
		gradientCache.bytes = 0
	}
	gradientCache.entries[key] = source
	gradientCache.bytes += len(source)
	return source
}
