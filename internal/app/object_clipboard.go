package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"net/http"
	"net/url"
	"sort"
)

const maxObjectClipboardBytes = 64 << 20
const maxObjectClipboardElements = 2048

// ObjectClipboard owns only the selected objects and their dependencies. It
// deliberately has no slide, master, notes, engagement or result archive fields.
type ObjectClipboard struct {
	Geometry            *objectClipboardGeometry               `json:"geometry,omitempty"`
	ChartThemeFallbacks map[string]clipboardChartThemeFallback `json:"chartThemeFallbacks,omitempty"`
	TableThemeFallbacks map[string]clipboardTableThemeFallback `json:"tableThemeFallbacks,omitempty"`
	LabelThemeFallbacks map[string]string                      `json:"labelThemeFallbacks,omitempty"` // Label-only styling, never shape geometry/content.
	SlideLinks          map[string]string                      `json:"slideLinks,omitempty"`          // Selected object ID -> original destination slide ID; empty means unresolved.
	ThemeFallbacks      map[string]string                      `json:"themeFallbacks,omitempty"`      // Source-resolved query deltas, used only without a compatible theme.
	ThemeSignature      string                                 `json:"themeSignature,omitempty"`      // Palette association only, not a copied theme.
	Version             int                                    `json:"version"`
	Elements            []Element                              `json:"elements"`
	Objects             *DeckObjects                           `json:"objects,omitempty"`
	Assets              map[string]DeckAsset                   `json:"assets,omitempty"`
	Typefaces           map[string]DeckTypeface                `json:"typefaces,omitempty"`
	Warnings            []string                               `json:"warnings,omitempty"`
}

func (bundle *ObjectClipboard) UnmarshalJSON(data []byte) error {
	fields, err := decodeMetadataJSONObject(data, "version", "elements", "objects", "assets", "typefaces", "warnings", "themeSignature", "themeFallbacks", "labelThemeFallbacks", "tableThemeFallbacks", "chartThemeFallbacks", "slideLinks", "geometry")
	if err != nil {
		return err
	}
	for _, namespace := range []string{"assets", "typefaces", "themeFallbacks", "labelThemeFallbacks", "tableThemeFallbacks", "chartThemeFallbacks", "slideLinks"} {
		if raw, ok := fields[namespace]; ok {
			if _, err := decodeMetadataJSONObject(raw); err != nil {
				return fmt.Errorf("clipboard %s: %w", namespace, err)
			}
		}
	}
	type stored ObjectClipboard
	var decoded stored
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*bundle = ObjectClipboard(decoded)
	return nil
}

func (bundle ObjectClipboard) Validate() error {
	return bundle.validateContext(context.Background())
}

func (bundle ObjectClipboard) validateContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if bundle.Version != 1 || len(bundle.Elements) == 0 || len(bundle.Elements) > maxObjectClipboardElements {
		return fmt.Errorf("unsupported or oversized object clipboard")
	}
	if bundle.ThemeSignature != "" {
		value, err := hex.DecodeString(bundle.ThemeSignature)
		if err != nil || len(value) != 32 {
			return fmt.Errorf("invalid clipboard theme signature")
		}
	}
	data, err := json.Marshal(bundle)
	if err != nil || len(data) > maxObjectClipboardBytes {
		return fmt.Errorf("object clipboard exceeds 64 MiB")
	}
	if err := bundle.Objects.Validate(); err != nil {
		return err
	}
	if bundle.Objects != nil {
		data, err := json.Marshal(bundle.Objects)
		if err != nil || len(data) > maxStructuredObjectBytes {
			return fmt.Errorf("clipboard structured payloads exceed 16 MiB")
		}
	}
	if bundle.Objects != nil && len(bundle.Objects.Builds) != 0 {
		return fmt.Errorf("object clipboard cannot contain a slide timeline")
	}
	if len(bundle.Typefaces) > 128 {
		return fmt.Errorf("too many clipboard typefaces")
	}
	for id, face := range bundle.Typefaces {
		prepared, err := prepareTypeface(face)
		if err != nil {
			return err
		}
		if !typefaceIDRE.MatchString(id) || id != prepared.ID() {
			return fmt.Errorf("invalid clipboard typeface identity")
		}
	}
	seen := map[string]bool{}
	elementsByID := make(map[string]Element, len(bundle.Elements))
	usedTables, usedCharts, usedAssets := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, element := range bundle.Elements {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validObjectID(element.ID) || seen[element.ID] {
			return fmt.Errorf("invalid or duplicate clipboard element ID")
		}
		seen[element.ID] = true
		elementsByID[element.ID] = element
		if element.ObjectID != "" && element.Kind != "table" && element.Kind != "chart" {
			return fmt.Errorf("unexpected structured clipboard reference")
		}
		if element.Kind == "table" {
			usedTables[element.ObjectID] = true
		}
		if element.Kind == "chart" {
			usedCharts[element.ObjectID] = true
		}
		switch element.Kind {
		case "text", "heading", "bullet", "code", "text-image", "page-number", "shape", "connector", "image", "video", "table", "chart":
		default:
			return fmt.Errorf("unsupported clipboard object kind %q", element.Kind)
		}
		if _, err := url.ParseQuery(element.Query); err != nil {
			return fmt.Errorf("invalid clipboard object properties")
		}
		if element.AssetID != "" {
			usedAssets[element.AssetID] = true
			if element.Kind != "image" && element.Kind != "video" {
				return fmt.Errorf("only images may reference clipboard media")
			}
			if _, ok := bundle.Assets[element.AssetID]; !ok {
				return fmt.Errorf("missing clipboard image %q", element.AssetID)
			}
		}
	}

	if err := bundle.Geometry.validate(bundle.Elements); err != nil {
		return err
	}
	for _, element := range bundle.Elements {
		if element.Kind == "connector" {
			q, _ := url.ParseQuery(element.Query)
			if !seen[q.Get("connector-from")] || !seen[q.Get("connector-to")] {
				return fmt.Errorf("copy both connected shapes with their connector")
			}
		}
	}
	for id, query := range bundle.ThemeFallbacks {
		if !seen[id] || bundle.ThemeSignature == "" {
			return fmt.Errorf("clipboard fallback has no source object or theme")
		}
		if err := validateClipboardThemeFallback(query); err != nil {
			return err
		}
	}
	if len(bundle.TableThemeFallbacks) > len(bundle.Elements) {
		return fmt.Errorf("unrelated clipboard table fallbacks")
	}
	if len(bundle.ChartThemeFallbacks) > len(bundle.Elements) {
		return fmt.Errorf("unrelated clipboard chart fallbacks")
	}
	for id, fallback := range bundle.ChartThemeFallbacks {
		owner := elementsByID[id]
		if !seen[id] || owner.Kind != "chart" || bundle.ThemeSignature == "" || bundle.Objects == nil {
			return fmt.Errorf("clipboard chart fallback has no source chart or theme")
		}
		if err := fallback.validate(bundle.Objects.Charts[owner.ObjectID]); err != nil {
			return err
		}
	}
	for id, fallback := range bundle.TableThemeFallbacks {
		owner := elementsByID[id]
		if !seen[id] || owner.Kind != "table" || bundle.ThemeSignature == "" || bundle.Objects == nil {
			return fmt.Errorf("clipboard table fallback has no source table or theme")
		}
		if err := fallback.validate(bundle.Objects.Tables[owner.ObjectID]); err != nil {
			return err
		}
	}
	if len(bundle.LabelThemeFallbacks) > len(bundle.Elements) {
		return fmt.Errorf("clipboard contains unrelated label fallbacks")
	}
	for id, query := range bundle.LabelThemeFallbacks {
		if !seen[id] || bundle.ThemeSignature == "" {
			return fmt.Errorf("clipboard label fallback has no source object or theme")
		}
		if _, ok := shapeLabel(elementsByID[id]); !ok {
			return fmt.Errorf("clipboard label fallback has no source label")
		}
		if err := validateClipboardLabelThemeFallback(query); err != nil {
			return err
		}
	}
	if len(bundle.SlideLinks) > len(bundle.Elements) {
		return fmt.Errorf("clipboard contains unrelated slide-link identities")
	}
	for id, target := range bundle.SlideLinks {
		if !seen[id] || target != "" && !validObjectID(target) {
			return fmt.Errorf("invalid clipboard slide-link identity")
		}
		link, ok := linkTargetFromQuery(elementsByID[id].Query, 0)
		if !ok || link.Slide < 0 {
			return fmt.Errorf("clipboard slide link has no linked source object")
		}
	}
	if err := validateDeckObjectReferences(Deck{Slides: []Slide{{Elements: bundle.Elements}}, Objects: bundle.Objects}); err != nil {
		return err
	}
	if len(usedAssets) != len(bundle.Assets) || (bundle.Objects != nil && (len(usedTables) != len(bundle.Objects.Tables) || len(usedCharts) != len(bundle.Objects.Charts))) {
		return fmt.Errorf("clipboard contains unrelated payloads")
	}
	var decodedPixels int64
	for id, asset := range bundle.Assets {
		if !validObjectID(id) {
			return fmt.Errorf("invalid clipboard asset ID")
		}
		if err := validateClipboardAssetBudgetContext(ctx, asset, &decodedPixels); err != nil {
			return fmt.Errorf("clipboard image %s: %w", id, err)
		}
	}
	return nil
}

func validateClipboardAsset(asset DeckAsset) error {
	var pixels int64
	return validateClipboardAssetBudget(asset, &pixels)
}

// One decoded-pixel budget spans the whole bundle, including original sources
// and animation frames, rather than letting each small compressed asset claim
// a separate large allocation. Inspect complete pixels, not just image headers.
func validateClipboardAssetBudget(asset DeckAsset, pixels *int64) error {
	return validateClipboardAssetBudgetContext(context.Background(), asset, pixels)
}

func validateClipboardAssetBudgetContext(ctx context.Context, asset DeckAsset, pixels *int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(asset.Frames) > 2048 || asset.Source != nil && len(asset.Source.Frames) > 2048 {
		return fmt.Errorf("too many image frames")
	}
	key, err := mediaValidationDigest(ctx, asset)
	if err != nil {
		return err
	}
	if usage, ok := sharedMediaValidation.lookup(key); ok {
		*pixels += usage
		if *pixels > 64000000 {
			return fmt.Errorf("image exceeds the decoded frame budget")
		}
		return ctx.Err()
	}
	before := *pixels
	if err := validateMediaPixelsUncached(ctx, asset, pixels); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	sharedMediaValidation.remember(key, *pixels-before)
	return nil
}

func validateMediaPixelsUncached(ctx context.Context, asset DeckAsset, pixels *int64) error {
	if asset.Video != nil {
		if asset.Source != nil || asset.Vector != nil || len(asset.Frames) != 0 {
			return fmt.Errorf("video poster cannot also contain image derivatives or animation")
		}
		info, err := inspectMovie(asset.Video.Data)
		if err != nil {
			return err
		}
		if info.MIME != asset.Video.MIME || info.Width != asset.Video.Width || info.Height != asset.Video.Height || info.DurationMS != asset.Video.DurationMS {
			return fmt.Errorf("video bytes do not match declared metadata")
		}
	}
	if err := validateVectorAsset(ctx, asset); err != nil {
		return err
	}
	check := func(data []byte, mime string, width, height int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if width < 1 || height < 1 || width > 32768 || height > 32768 || int64(width)*int64(height) > 100000000 {
			return fmt.Errorf("invalid image dimensions")
		}
		if mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" && mime != "image/webp" {
			return fmt.Errorf("unsupported image type")
		}
		*pixels += int64(width) * int64(height)
		if *pixels > 64000000 {
			return fmt.Errorf("image exceeds the decoded frame budget")
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != width || config.Height != height {
			return fmt.Errorf("image bytes do not match their dimensions")
		}
		if mime != "image/"+format {
			return fmt.Errorf("image bytes do not match their declared type")
		}
		if format == "gif" {
			if err := preflightGIFFrames(data); err != nil {
				return err
			}
			animation, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("invalid GIF image pixels")
			}
			*pixels += int64(width) * int64(height) * int64(max(0, len(animation.Image)-1))
			if *pixels > 64000000 {
				return fmt.Errorf("image exceeds the decoded frame budget")
			}
		} else if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
			return fmt.Errorf("invalid image pixels")
		}
		return nil
	}
	if asset.MIME != "image/png" && asset.MIME != "image/jpeg" && asset.MIME != "image/gif" && asset.MIME != "image/webp" {
		return fmt.Errorf("unsupported image type")
	}
	if len(asset.Frames) > 2048 || asset.LoopCount < -1 {
		return fmt.Errorf("invalid image animation")
	}
	if len(asset.Frames) == 0 || len(asset.Data) > 0 {
		if err := check(asset.Data, asset.MIME, asset.Width, asset.Height); err != nil {
			return err
		}
	}
	if len(asset.Frames) > 0 {
		for _, frame := range asset.Frames {
			if frame.DelayMS < 1 || frame.DelayMS > 3600000 {
				return fmt.Errorf("invalid animation frame delay")
			}
			if err := check(frame.Data, "image/png", asset.Width, asset.Height); err != nil {
				return err
			}
		}
	}
	if source := asset.Source; source != nil {
		if source.MIME != "image/png" && source.MIME != "image/jpeg" && source.MIME != "image/gif" && source.MIME != "image/webp" {
			return fmt.Errorf("unsupported original image type")
		}
		if len(source.Frames) == 0 || len(source.Data) > 0 {
			if err := check(source.Data, source.MIME, source.Width, source.Height); err != nil {
				return err
			}
		}
		if len(source.Frames) > 2048 {
			return fmt.Errorf("too many source frames")
		}
		for _, frame := range source.Frames {
			if frame.DelayMS < 1 || frame.DelayMS > 3600000 {
				return fmt.Errorf("invalid source animation delay")
			}
			if err := check(frame.Data, "image/png", source.Width, source.Height); err != nil {
				return err
			}
		}
	}
	return nil
}

func makeObjectClipboard(deck Deck, elements []Element) (ObjectClipboard, error) {
	bundle, err := snapshotObjectClipboard(deck, elements)
	if err == nil {
		err = bundle.Validate()
	}
	return bundle, err
}

// Snapshot under the document read lock, then perform compression-independent
// size/reference/pixel validation outside it in the transport handler.
func snapshotObjectClipboard(deck Deck, elements []Element) (ObjectClipboard, error) {
	bundle := ObjectClipboard{Version: 1, Assets: map[string]DeckAsset{}, ThemeSignature: objectStyleThemeSignature(deck)}
	ids := map[string]bool{}
	for _, element := range elements {
		ids[element.ID] = true
	}
	for _, element := range cloneSlide(Slide{Elements: elements}).Elements {
		element = deck.captureWordColorFallbacks(element)
		if element.Kind == "connector" {
			q, _ := url.ParseQuery(element.Query)
			if !ids[q.Get("connector-from")] || !ids[q.Get("connector-to")] {
				bundle.Warnings = append(bundle.Warnings, "A connector was omitted because both connected shapes were not selected.")
				continue
			}
		}
		// A pasted placeholder becomes an ordinary authored object. Local image
		// cache paths are never included in the system clipboard.
		element.Placeholder, element.SlotID, element.PlaceholderRole, element.MasterSlotID, element.Inherited = false, "", "", "", false
		if element.Kind == "image" || element.Kind == "video" {
			element.Path = ""
			if element.AssetID != "" {
				asset, ok := deck.Assets[element.AssetID]
				if !ok {
					return bundle, fmt.Errorf("missing embedded clipboard image")
				}
				bundle.Assets[element.AssetID] = asset
			}
		}
		if element.ObjectID != "" {
			if deck.Objects == nil {
				return bundle, fmt.Errorf("missing structured clipboard payload")
			}
			if bundle.Objects == nil {
				bundle.Objects = &DeckObjects{Version: 1, Tables: map[string]TableData{}, Charts: map[string]ChartData{}}
			}
			switch element.Kind {
			case "table":
				value, ok := deck.Objects.Tables[element.ObjectID]
				if !ok {
					return bundle, fmt.Errorf("missing table clipboard payload")
				}
				bundle.Objects.Tables[element.ObjectID] = mapTableWordColors(value, deck, false)
				if deck.Theme != nil {
					if bundle.TableThemeFallbacks == nil {
						bundle.TableThemeFallbacks = map[string]clipboardTableThemeFallback{}
					}
					bundle.TableThemeFallbacks[element.ID] = captureClipboardTableTheme(deck, element, value)
				}
			case "chart":
				value, ok := deck.Objects.Charts[element.ObjectID]
				if !ok {
					return bundle, fmt.Errorf("missing chart clipboard payload")
				}
				bundle.Objects.Charts[element.ObjectID] = mapChartColorTokens(value, deck, false)
				if deck.Theme != nil {
					if bundle.ChartThemeFallbacks == nil {
						bundle.ChartThemeFallbacks = map[string]clipboardChartThemeFallback{}
					}
					bundle.ChartThemeFallbacks[element.ID] = captureClipboardChartTheme(deck, element, value)
				}
			}
		}
		bundle.Elements = append(bundle.Elements, element)
		if fallback := clipboardThemeFallback(deck, element); fallback != "" {
			if bundle.ThemeFallbacks == nil {
				bundle.ThemeFallbacks = map[string]string{}
			}
			bundle.ThemeFallbacks[element.ID] = fallback
		}
		if label, ok := shapeLabel(element); ok {
			if fallback := clipboardThemeFallback(deck, label); fallback != "" {
				if bundle.LabelThemeFallbacks == nil {
					bundle.LabelThemeFallbacks = map[string]string{}
				}
				bundle.LabelThemeFallbacks[element.ID] = fallback
			}
		}
	}
	// Asset bytes must outlive the source document and never alias its history.
	bundle.SlideLinks = captureClipboardSlideLinks(deck, bundle.Elements)
	bundle.Assets = cloneDeck(Deck{Assets: bundle.Assets}).Assets
	if len(deck.Typefaces) > 0 {
		// Copy only dependencies of the selected objects, including linked word,
		// label, table and chart styles; never the rest of the source deck.
		selection := deck
		selection.Slides = []Slide{{Elements: bundle.Elements}}
		selection.Masters = MasterDeck{}
		scene, err := buildDocumentSlideScene(selection, 0, 245, 56)
		if err != nil {
			return bundle, err
		}
		bundle.Typefaces = cloneTypefaces(scene.Typefaces, true)
	}
	return bundle, nil
}

func clipboardAssetHash(asset DeckAsset) string {
	data, _ := json.Marshal(asset)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// Work against owned payloads and a new asset map. Nothing in the destination
// document changes until every source reference has been resolved successfully.
func prepareObjectClipboardPaste(deck Deck, bundle ObjectClipboard) ([]Element, map[string]DeckAsset, *DeckObjects, error) {
	return prepareObjectClipboardPasteContext(context.Background(), deck, bundle)
}

func prepareObjectClipboardPasteContext(ctx context.Context, deck Deck, bundle ObjectClipboard, grid ...[2]int) ([]Element, map[string]DeckAsset, *DeckObjects, error) {
	if err := bundle.validateContext(ctx); err != nil {
		return nil, nil, nil, err
	}
	// Direct callers may still own and mutate their destination. The session
	// path below already captured this owned registry under its read lock.
	deck.Objects = deck.Objects.Clone()
	return prepareValidatedOwnedClipboardPaste(ctx, deck, bundle, grid...)
}

// Consumes only the captured destination registry; source bundle data is still
// cloned before insertion. Callers must validate the bundle first and must not
// pass a live document registry. Assets/theme are read-only here.
func prepareValidatedOwnedClipboardPaste(ctx context.Context, deck Deck, bundle ObjectClipboard, grid ...[2]int) ([]Element, map[string]DeckAsset, *DeckObjects, error) {
	elements := cloneSlide(Slide{Elements: bundle.Elements}).Elements
	resolveClipboardSlideLinks(deck, elements, bundle.SlideLinks)
	if deck.Theme == nil {
		for index, element := range elements {
			element = flattenWordColorTokens(element)
			q, _ := url.ParseQuery(element.Query)
			fallback, _ := url.ParseQuery(bundle.ThemeFallbacks[element.ID])
			for key, values := range fallback {
				if values[0] == "" {
					q.Del(key)
				} else {
					q.Set(key, values[0])
				}
			}
			labelFallback, _ := url.ParseQuery(bundle.LabelThemeFallbacks[element.ID])
			for key, values := range labelFallback {
				if values[0] == "" {
					q.Del("label-" + key)
				} else {
					q.Set("label-"+key, values[0])
				}
			}
			element.Query = q.Encode()
			elements[index] = element
		}
	}

	if err := applyClipboardGeometry(deck, elements, bundle.Geometry, grid...); err != nil {
		return nil, nil, nil, err
	}
	objects := deck.Objects
	if objects == nil {
		objects = &DeckObjects{Version: 1}
	}
	if objects.Tables == nil {
		objects.Tables = map[string]TableData{}
	}
	if objects.Charts == nil {
		objects.Charts = map[string]ChartData{}
	}
	assets := make(map[string]DeckAsset, len(deck.Assets)+len(bundle.Assets))
	byHash := map[string]string{}
	for id, asset := range deck.Assets {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		assets[id] = asset
		byHash[clipboardAssetHash(asset)] = id
	}
	assetIDs := map[string]string{}
	for old, asset := range cloneDeck(Deck{Assets: bundle.Assets}).Assets {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		hash := clipboardAssetHash(asset)
		id := byHash[hash]
		if id == "" {
			id = hash
			if _, exists := assets[id]; exists {
				id = newStableID("asset")
			}
			assets[id] = asset
			byHash[hash] = id
		}
		assetIDs[old] = id
	}
	for i := range elements {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		e := &elements[i]
		e.Placeholder, e.SlotID, e.PlaceholderRole, e.MasterSlotID, e.Inherited = false, "", "", "", false
		if e.Kind == "table" {
			id := newStableID("table")
			objects.Tables[id] = cloneTable(bundle.Objects.Tables[e.ObjectID])
			if deck.Theme == nil {
				objects.Tables[id] = mapTableWordColors(objects.Tables[id], deck, true)
				if fallback, ok := bundle.TableThemeFallbacks[e.ID]; ok {
					objects.Tables[id] = applyClipboardTableTheme(objects.Tables[id], fallback)
				}
			}
			e.ObjectID = id
		} else if e.Kind == "chart" {
			id := newStableID("chart")
			objects.Charts[id] = cloneChart(bundle.Objects.Charts[e.ObjectID])
			if deck.Theme == nil {
				objects.Charts[id] = mapChartColorTokens(objects.Charts[id], deck, true)
				if fallback, ok := bundle.ChartThemeFallbacks[e.ID]; ok {
					objects.Charts[id] = applyClipboardChartTheme(objects.Charts[id], e, fallback)
				}
			}
			e.ObjectID = id
		}
		if e.AssetID != "" {
			e.AssetID = assetIDs[e.AssetID]
			e.Path = embeddedAssetPath(e.AssetID, assets[e.AssetID])
		}
	}
	freshPastedGroups(elements)
	freshPastedConnectorIDs(elements)
	if err := objects.Validate(); err != nil {
		return nil, nil, nil, err
	}
	if deck.Objects == nil && bundle.Objects == nil {
		objects = nil
	}
	return elements, assets, objects, nil
}

var errClipboardPasteStale = errors.New("the document or selection changed; paste the objects again")

// A private, single-revision draft cannot be supplied through HTTP/JSON. It owns
// preparation results; only commit installs them into the captured destination.
type objectClipboardPasteDraft struct {
	cols, rows int
	owner      *nativeEditorSession
	ctx        context.Context
	version    int64
	master     bool
	index      int
	slideID    string
	elements   []Element
	assets     map[string]DeckAsset
	objects    *DeckObjects
	typefaces  map[string]DeckTypeface
}

// Capture only the registry/media dependencies, not unrelated slides or notes.
// Byte ownership is established under the read lock; image decoding, JSON size
// validation, hashing and ID remapping happen after it is released.
func (s *nativeEditorSession) captureObjectClipboardPaste(ctx context.Context, action nativeEditorAction) (*objectClipboardPasteDraft, Deck, error) {
	if action.Action != "paste-objects" || action.Clipboard == nil {
		return nil, Deck{}, errInvalidEditorAction
	}
	if err := ctx.Err(); err != nil {
		return nil, Deck{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if action.SceneRevision != nil && *action.SceneRevision != s.version {
		return nil, Deck{}, errClipboardPasteStale
	}
	draft := &objectClipboardPasteDraft{owner: s, ctx: ctx, version: s.version, master: s.masterMode, index: s.current}
	draft.cols, draft.rows = authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	var target *Slide
	if draft.master {
		draft.index = s.currentMaster
		if draft.index >= 0 && draft.index <= len(s.deck.Masters.Layouts) {
			target = masterSlideAt(&s.deck, draft.index)
		}
	} else if draft.index >= 0 && draft.index < len(s.deck.Slides) {
		target = &s.deck.Slides[draft.index]
	}
	if target == nil {
		return nil, Deck{}, errInvalidEditorAction
	}
	draft.slideID = target.ID
	// Palette/font roles are destination dependencies too. Omitting them makes
	// asynchronous paste flatten linked styles as if a themed deck were legacy.
	snapshot := cloneDeck(Deck{PageSize: s.deck.PageSize, Assets: s.deck.Assets, Objects: s.deck.Objects, Theme: s.deck.Theme, Typefaces: s.deck.Typefaces, Appearance: s.deck.Appearance})
	if action.Clipboard != nil && clipboardHasSlideLinks(*action.Clipboard) {
		snapshot.Slides = make([]Slide, len(s.deck.Slides))
		for index, slide := range s.deck.Slides {
			snapshot.Slides[index].ID = slide.ID // IDs only; no unrelated content.
		}
	}
	return draft, snapshot, nil
}

func (s *nativeEditorSession) prepareAndApplyObjectClipboard(ctx context.Context, action nativeEditorAction) error {
	_, err := s.prepareAndApplyObjectClipboardResult(ctx, action)
	return err
}

func (s *nativeEditorSession) prepareAndApplyObjectClipboardResult(ctx context.Context, action nativeEditorAction) ([]string, error) {
	if action.Clipboard == nil {
		return nil, errInvalidEditorAction
	}
	draft, snapshot, err := s.captureObjectClipboardPaste(ctx, action)
	if err != nil {
		return nil, err
	}
	if err = action.Clipboard.validateContext(ctx); err != nil {
		return nil, err
	}
	draft.typefaces = cloneTypefaces(snapshot.Typefaces, false)
	if draft.typefaces == nil {
		draft.typefaces = map[string]DeckTypeface{}
	}
	for id, face := range action.Clipboard.Typefaces {
		prepared, err := prepareTypeface(face)
		if err != nil {
			return nil, err
		}
		draft.typefaces[id] = prepared
	}
	if len(draft.typefaces) > 128 {
		return nil, fmt.Errorf("too many typefaces in destination deck")
	}
	draft.elements, draft.assets, draft.objects, err = prepareValidatedOwnedClipboardPaste(ctx, snapshot, *action.Clipboard, [2]int{draft.cols, draft.rows})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	warnings := clipboardSlideLinkWarnings(*action.Clipboard, draft.elements)
	warnings = append(warnings, clipboardGeometryWarnings(snapshot, *action.Clipboard)...)
	action.preparedClipboard = draft
	action.Clipboard = nil
	return warnings, s.apply(action)
}

// Check before taking a history snapshot or changing selection. A request that
// waited for the write lock can have become cancelled in the meantime.
func (s *nativeEditorSession) checkPreparedClipboardLocked(draft *objectClipboardPasteDraft) error {
	if draft.owner != s || draft.ctx == nil {
		return errInvalidEditorAction
	}
	if err := draft.ctx.Err(); err != nil {
		return err
	}
	if draft.version != s.version || draft.master != s.masterMode {
		return errClipboardPasteStale
	}
	cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	if cols != draft.cols || rows != draft.rows {
		return errClipboardPasteStale
	}
	var target *Slide
	if draft.master {
		if s.currentMaster == draft.index && draft.index >= 0 && draft.index <= len(s.deck.Masters.Layouts) {
			target = masterSlideAt(&s.deck, draft.index)
		}
	} else if s.current == draft.index && draft.index >= 0 && draft.index < len(s.deck.Slides) {
		target = &s.deck.Slides[draft.index]
	}
	if target == nil || target.ID != draft.slideID {
		return errClipboardPasteStale
	}
	return nil
}

func (s *nativeEditorSession) commitObjectClipboard(target *Slide, draft *objectClipboardPasteDraft) bool {
	start := len(target.Elements)
	target.Elements = append(target.Elements, draft.elements...)
	s.deck.Assets, s.deck.Objects = draft.assets, draft.objects
	s.deck.Typefaces = mergeTypefaces(draft.typefaces, s.fontLibrary)
	s.selected, s.selection, s.editingGroup = start, map[int]bool{}, ""
	for i := range draft.elements {
		s.selection[start+i] = true
	}
	return true
}

// Register only after document/reference validation has succeeded. Registration
// is a short in-memory map update, not decoding or a local image-cache write.
func registerPreparedClipboardAssets(draft *objectClipboardPasteDraft) {
	if draft == nil {
		return
	}
	for _, element := range draft.elements {
		if element.AssetID == "" {
			continue
		}
		asset := draft.assets[element.AssetID]
		if len(asset.Frames) > 0 {
			registerEmbeddedAnimatedAsset(element.AssetID, asset)
		} else {
			registerEmbeddedStillAsset(element.AssetID, asset)
		}
	}
}

func (s *nativeEditorSession) deleteClipboardObjects(target *Slide, ids []string) (bool, error) {
	indices, err := editableObjectIndices(*target, ids)
	if err != nil {
		return false, err
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indices)))
	for _, index := range indices {
		target.Elements = append(target.Elements[:index], target.Elements[index+1:]...)
	}
	s.selected, s.selection = -1, map[int]bool{}
	return true, nil
}

func (s *nativeEditorSession) handlePasteObjectClipboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action nativeEditorAction
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxObjectClipboardBytes+(1<<20))).Decode(&action); err != nil || action.Action != "paste-objects" || action.Clipboard == nil {
		writeEditorFailure(w, r, nativeEditorAction{Action: "paste-objects"}, editorFailure("invalid-clipboard", "clipboard", "", errors.New("invalid object clipboard")), http.StatusBadRequest)
		return
	}
	warnings, err := s.prepareAndApplyObjectClipboardResult(r.Context(), action)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errClipboardPasteStale) {
			status = http.StatusConflict
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusRequestTimeout
		}
		writeEditorFailure(w, r, action, err, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	state := s.actionTransportState(action.Action)
	state.ClipboardWarnings = warnings
	_ = json.NewEncoder(w).Encode(state)
}

func (s *nativeEditorSession) handleObjectClipboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Version int64    `json:"version"`
		IDs     []string `json:"ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&request); err != nil || len(request.IDs) == 0 || len(request.IDs) > maxObjectClipboardElements {
		http.Error(w, "invalid clipboard selection", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	if request.Version != s.version {
		s.mu.RUnlock()
		http.Error(w, "the document changed; copy the selection again", http.StatusConflict)
		return
	}
	var slide Slide
	index := s.current
	if s.masterMode {
		index = s.currentMaster
		slide = *masterSlideAt(&s.deck, s.currentMaster)
	} else if s.current >= 0 && s.current < len(s.deck.Slides) {
		slide = s.deck.Slides[s.current]
	}
	indices := map[string]Element{}
	for _, element := range slide.Elements {
		indices[element.ID] = element
	}
	// Authorisation remains against authored IDs, but a detached copy needs
	// the effective placeholder query, not the sparse master override. Capture
	// only this owner and relevant masters; do not render media or copy a deck.
	scope, scopeErr := captureSlideResolutionScope(s.deck, index, s.masterMode)
	if scopeErr != nil {
		s.mu.RUnlock()
		http.Error(w, "clipboard target is no longer available", http.StatusBadRequest)
		return
	}
	var resolved Slide
	if s.masterMode {
		resolved = scope.resolved()
	} else {
		resolved = scope.deck.resolveSlideSource(scope.source, index, true)
	}
	resolved = withTrueTypeDefaults(resolved)
	effective := map[string]Element{}
	counts := map[string]int{}
	for _, element := range resolved.Elements {
		effective[element.ID] = element
		counts[element.ID]++
	}
	elements := make([]Element, 0, len(request.IDs))
	seen := map[string]bool{}
	for _, id := range request.IDs {
		value, ok := indices[id]
		if !ok || seen[id] || counts[id] > 1 {
			s.mu.RUnlock()
			http.Error(w, "clipboard target is no longer available", http.StatusBadRequest)
			return
		}
		seen[id] = true
		if resolvedElement, found := effective[id]; found {
			value = resolvedElement
		}
		// Explicit slide/master foreground is not a portable theme role. Keep
		// its current colour when the pasted object leaves that owner.
		q, _ := url.ParseQuery(value.Query)
		if value.Kind != "image" && !q.Has("fg") && !q.Has("header") && !q.Has("theme-fg") && (resolved.FG != "" || value.Kind == "heading" && resolved.HeaderFG != "") {
			q.Set("fg", scenePaintFor(value, resolved, 1, 1).Color)
			value.Query = q.Encode()
		}
		elements = append(elements, value)
	}
	bundle, err := snapshotObjectClipboard(s.deck, elements)
	cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	var geometryDeck Deck
	if err == nil {
		geometryDeck, _, err = captureMutationPreviewDeck(s.deck, index, s.masterMode, nil)
	}
	master := s.masterMode
	s.mu.RUnlock()
	if err == nil {
		err = captureClipboardGeometry(r.Context(), &bundle, geometryDeck, index, master, cols, rows)
	}
	if err == nil {
		err = bundle.validateContext(r.Context())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(bundle)
}
