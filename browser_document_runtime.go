package main

import "sync"

// A browser owns one active document in one WASM runtime. Export/render calls
// borrow that lifetime; successful replacement waits for them before releasing
// old storage. Native helper constructors do not reset process-wide caches.
var browserDocumentGate sync.RWMutex
var browserDocumentEpoch uint64

func browserDocumentRead[T any](operation func() T) T {
	browserDocumentGate.RLock()
	defer browserDocumentGate.RUnlock()
	return operation()
}

func replaceBrowserDocument(path string, source []byte, untitled bool) (*nativeEditorSession, error) {
	browserDocumentGate.Lock()
	defer browserDocumentGate.Unlock()
	deck, err := parseEditorDeckData(path, source)
	if err != nil {
		return nil, err
	} // Failed Open preserves the working runtime.
	ensureDefaultAuthoredSize()
	session := newNativeEditorSession(path, deck, untitled, parsedDeckElementOrderChanged)
	releaseBrowserDocumentCaches(session.deck)
	browserDocumentEpoch++
	return session, nil
}

// Caller owns the exclusive browser lifetime gate. All registered WASM calls
// borrow its read side, so an old conversion cannot publish back after this
// boundary. Use each cache's lock and keep its configured capacity/mutex intact.
// Bundled font faces, emoji files, static silhouette definitions and template
// previews contain no authored document data and deliberately remain reusable.
func releaseBrowserDocumentCaches(deck Deck) {
	sharedSceneSources.mu.Lock()
	sharedSceneSources.entries = nil
	sharedSceneSources.order.Init()
	sharedSceneSources.bytes = 0
	sharedSceneSources.mu.Unlock()
	sharedSceneTextObjects.mu.Lock()
	sharedSceneTextObjects.entries = nil
	sharedSceneTextObjects.bytes = 0
	sharedSceneTextObjects.mu.Unlock()
	sharedSceneTables.mu.Lock()
	sharedSceneTables.entries = nil
	sharedSceneTables.order.Init()
	sharedSceneTables.bytes = 0
	sharedSceneTables.mu.Unlock()
	sharedChartGeometry.Lock()
	sharedChartGeometry.entries = nil
	sharedChartGeometry.bytes = 0
	sharedChartGeometry.Unlock()
	sharedSceneAnimations.mu.Lock()
	sharedSceneAnimations.entries = nil
	sharedSceneAnimations.bytes = 0
	sharedSceneAnimations.mu.Unlock()
	sharedSceneLayouts.mu.Lock()
	sharedSceneLayouts.entries = nil
	sharedSceneLayouts.bytes = 0
	sharedSceneLayouts.mu.Unlock()
	sharedSceneObjectLayouts.mu.Lock()
	sharedSceneObjectLayouts.entries = nil
	sharedSceneObjectLayouts.order.Init()
	sharedSceneObjectLayouts.bytes = 0
	sharedSceneObjectLayouts.mu.Unlock()
	sharedShapeLabelFits.mu.Lock()
	sharedShapeLabelFits.queries = nil
	sharedShapeLabelFits.bytes = 0
	sharedShapeLabelFits.mu.Unlock()
	connectorRouteCache.mu.Lock()
	connectorRouteCache.entries = nil
	connectorRouteCache.order.Init()
	connectorRouteCache.bytes = 0
	connectorRouteCache.mu.Unlock()
	sharedShapeRasters.mu.Lock()
	sharedShapeRasters.entries = nil
	sharedShapeRasters.bytes = 0
	sharedShapeRasters.mu.Unlock()
	sharedMediaValidation.mu.Lock()
	sharedMediaValidation.entries = nil
	sharedMediaValidation.order = nil
	sharedMediaValidation.mu.Unlock()
	asciiImageCache.clear()
	fastASCIIImageCache.clear()
	decodedStillImageCache.clear()
	emojiGlyphCache.clear()

	// Keep only the new owned document's sources, including hidden/master/tab
	// assets needed by editing/history. Do not retain a parse candidate's duplicate
	// buffers or an earlier deck merely because it used a different asset ID.
	stills, animations := map[string]DeckAsset{}, map[string]DeckAsset{}
	stillIDs, animationIDs := map[string]string{}, map[string]string{}
	for id, asset := range deck.Assets {
		if !validObjectID(id) {
			continue
		}
		if len(asset.Frames) > 0 {
			animations[id] = asset
			animationIDs[id] = embeddedImageSourceIdentity(asset)
		} else if len(asset.Data) > 0 {
			stills[id] = asset
			stillIDs[id] = embeddedImageSourceIdentity(asset)
		}
	}
	embeddedStillAssets.Lock()
	embeddedStillAssets.assets, embeddedStillAssets.identities = stills, stillIDs
	embeddedStillAssets.Unlock()
	embeddedAnimatedAssets.Lock()
	embeddedAnimatedAssets.assets, embeddedAnimatedAssets.identities = animations, animationIDs
	embeddedAnimatedAssets.Unlock()
	embeddedImageRegistrationRevision.Add(1)
}

// Explicit local profiling only: counts and accounting estimates, never source
// names, text, IDs, bytes or authored/runtime activity information. Not heap/GC
// measurements. Caller borrows the browser lifetime gate.
type browserCacheFootprint struct {
	Entries int `json:"entries"`
	Bytes   int `json:"bytes"`
}
type browserRuntimeCacheStats struct {
	Epoch          uint64                           `json:"epoch"`
	StillAssets    int                              `json:"stillAssets"`
	AnimatedAssets int                              `json:"animatedAssets"`
	Caches         map[string]browserCacheFootprint `json:"caches"`
}

func browserDocumentCacheStats() browserRuntimeCacheStats {
	stats := browserRuntimeCacheStats{Epoch: browserDocumentEpoch, Caches: map[string]browserCacheFootprint{}}
	sharedSceneSources.mu.Lock()
	stats.Caches["sources"] = browserCacheFootprint{len(sharedSceneSources.entries), sharedSceneSources.bytes}
	sharedSceneSources.mu.Unlock()
	sharedSceneTextObjects.mu.Lock()
	stats.Caches["text"] = browserCacheFootprint{len(sharedSceneTextObjects.entries), sharedSceneTextObjects.bytes}
	sharedSceneTextObjects.mu.Unlock()
	sharedSceneTables.mu.Lock()
	stats.Caches["tables"] = browserCacheFootprint{len(sharedSceneTables.entries), sharedSceneTables.bytes}
	sharedSceneTables.mu.Unlock()
	sharedChartGeometry.Lock()
	stats.Caches["charts"] = browserCacheFootprint{len(sharedChartGeometry.entries), sharedChartGeometry.bytes}
	sharedChartGeometry.Unlock()
	sharedSceneAnimations.mu.Lock()
	stats.Caches["animations"] = browserCacheFootprint{len(sharedSceneAnimations.entries), sharedSceneAnimations.bytes}
	sharedSceneAnimations.mu.Unlock()
	sharedSceneLayouts.mu.Lock()
	stats.Caches["layout"] = browserCacheFootprint{len(sharedSceneLayouts.entries), sharedSceneLayouts.bytes}
	sharedSceneLayouts.mu.Unlock()
	sharedSceneObjectLayouts.mu.Lock()
	stats.Caches["objectLayout"] = browserCacheFootprint{len(sharedSceneObjectLayouts.entries), sharedSceneObjectLayouts.bytes}
	sharedSceneObjectLayouts.mu.Unlock()
	sharedShapeLabelFits.mu.Lock()
	stats.Caches["labelFit"] = browserCacheFootprint{len(sharedShapeLabelFits.queries), sharedShapeLabelFits.bytes}
	sharedShapeLabelFits.mu.Unlock()
	connectorRouteCache.mu.Lock()
	stats.Caches["routes"] = browserCacheFootprint{len(connectorRouteCache.entries), connectorRouteCache.bytes}
	connectorRouteCache.mu.Unlock()
	sharedShapeRasters.mu.Lock()
	stats.Caches["shapeRasters"] = browserCacheFootprint{len(sharedShapeRasters.entries), sharedShapeRasters.bytes}
	sharedShapeRasters.mu.Unlock()
	sharedMediaValidation.mu.Lock()
	stats.Caches["validation"] = browserCacheFootprint{Entries: len(sharedMediaValidation.entries)}
	sharedMediaValidation.mu.Unlock()
	stats.Caches["imageRows"], stats.Caches["imageFastRows"], stats.Caches["decodedImages"], stats.Caches["emojiRows"] = asciiImageCache.footprint(), fastASCIIImageCache.footprint(), decodedStillImageCache.footprint(), emojiGlyphCache.footprint()
	embeddedStillAssets.RLock()
	stats.StillAssets = len(embeddedStillAssets.assets)
	embeddedStillAssets.RUnlock()
	embeddedAnimatedAssets.RLock()
	stats.AnimatedAssets = len(embeddedAnimatedAssets.assets)
	embeddedAnimatedAssets.RUnlock()
	return stats
}
