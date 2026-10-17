package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const masterDeckVersion = 1

const (
	placeholderTitle    = "title"
	placeholderSubtitle = "subtitle"
	placeholderBody     = "body"
	placeholderCode     = "code"
	placeholderImage    = "image"
	pageNumberShow      = "show"
	pageNumberHide      = "hide"
)

type Deck struct {
	PageSize       *DeckPageSize
	Theme          *DeckTheme
	Typefaces      map[string]DeckTypeface
	Objects        *DeckObjects
	Appearance     *DeckAppearance
	Diagnostics    []documentDiagnostic `json:"-"`
	HideActivityQR bool
	Tabs           []DeckTab
	Slides         []Slide
	Masters        MasterDeck
	Assets         map[string]DeckAsset
	Extra          *jsonExtensions
}

type DeckAsset struct {
	Extra     *jsonExtensions  `json:"-"`
	Vector    *DeckAssetVector `json:"vector,omitempty"`
	Source    *DeckAssetSource `json:"source,omitempty"`
	MIME      string           `json:"mime"`
	Width     int              `json:"width"`
	Height    int              `json:"height"`
	Data      []byte           `json:"data,omitempty"`
	Frames    []DeckAssetFrame `json:"frames,omitempty"`
	LoopCount int              `json:"loopCount,omitempty"`
}

// Source retains the original full-resolution media independently of the
// compact derivative used for retro sampling. Keeping its encoded bytes avoids
// turning a compact JPEG into an enormous lossless PNG on deck import.
type DeckAssetSource struct {
	Extra  *jsonExtensions  `json:"-"`
	MIME   string           `json:"mime"`
	Width  int              `json:"width"`
	Height int              `json:"height"`
	Data   []byte           `json:"data"`
	Frames []DeckAssetFrame `json:"frames,omitempty"`
}

type DeckAssetFrame struct {
	Extra   *jsonExtensions `json:"-"`
	Data    []byte          `json:"data"`
	DelayMS int64           `json:"delayMs"`
}

type MasterDeck struct {
	Version int             `json:"version"`
	Base    MasterLayout    `json:"base"`
	Layouts []MasterLayout  `json:"layouts"`
	Extra   *jsonExtensions `json:"-"`
}

type MasterLayout struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Slide Slide           `json:"slide"`
	Extra *jsonExtensions `json:"-"`
}

func (m MasterDeck) MarshalJSON() ([]byte, error) {
	type stored MasterDeck
	return encodeJSONExtensions(stored(m), m.Extra)
}

func (m *MasterDeck) UnmarshalJSON(data []byte) error {
	type stored MasterDeck
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "version", "base", "layouts")
	if err != nil {
		return err
	}
	*m = MasterDeck(value)
	m.Extra = extra
	return nil
}

func (m MasterLayout) MarshalJSON() ([]byte, error) {
	type stored MasterLayout
	return encodeJSONExtensions(stored(m), m.Extra)
}

func (m *MasterLayout) UnmarshalJSON(data []byte) error {
	type stored MasterLayout
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "id", "name", "slide")
	if err != nil {
		return err
	}
	*m = MasterLayout(value)
	m.Extra = extra
	return nil
}

var stableIDCounter uint64

func newStableID(prefix string) string {
	count := atomic.AddUint64(&stableIDCounter, 1)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", prefix, time.Now().UnixNano(), count)))
	return prefix + "-" + hex.EncodeToString(sum[:6])
}

func defaultMasterDeck() MasterDeck {
	masters := MasterDeck{
		Version: masterDeckVersion,
		Base: MasterLayout{
			ID:   "base",
			Name: "Base Master",
			Slide: Slide{
				FG:         "37",
				BG:         "40",
				PageNumber: pageNumberShow,
				Elements:   []Element{defaultPageNumberElement("base-page-number")},
			},
		},
	}
	masters.Layouts = []MasterLayout{
		{ID: "blank", Name: "Blank", Slide: Slide{}},
		{ID: "title", Name: "Title", Slide: Slide{Elements: []Element{
			masterPlaceholder("title-title", placeholderTitle, "Title", "heading", 1),
		}}},
		{ID: "title-subtitle", Name: "Title + Subtitle", Slide: Slide{Elements: []Element{
			masterPlaceholder("title-subtitle-title", placeholderTitle, "Title", "heading", 1),
			masterPlaceholder("title-subtitle-subtitle", placeholderSubtitle, "Subtitle", "heading", 2),
		}}},
		{ID: "title-body", Name: "Title + Body", Slide: Slide{Elements: []Element{
			masterPlaceholder("title-body-title", placeholderTitle, "Title", "heading", 1),
			masterPlaceholder("title-body-body", placeholderBody, "Body text", "text", 0),
		}}},
	}
	return masters
}

func defaultPageNumberElement(id string) Element {
	if id == "" {
		id = newStableID("page-number")
	}
	return Element{
		Kind:  "page-number",
		Text:  "1",
		ID:    id,
		Query: "bottom=1&fg=%23aaaaaa&right=2",
	}
}

func masterPlaceholder(id, role, text, kind string, level int) Element {
	return Element{
		Kind:            kind,
		Level:           level,
		Text:            text,
		ID:              id,
		SlotID:          id,
		PlaceholderRole: role,
	}
}

func (deck *Deck) EnsureDefaultMasters() {
	if deck == nil {
		return
	}
	// An empty named-layout list is valid authored data, not a missing archive.
	// Never replace a user's Base, its appearance/content or future metadata.
	if !deckHasMasterData(deck.Masters) {
		deck.Masters = defaultMasterDeck()
	}
}

func deckHasMasterData(masters MasterDeck) bool {
	return masters.Version != 0 || len(masters.Layouts) > 0 || !reflect.DeepEqual(masters.Base, MasterLayout{}) || masters.Extra != nil && len(*masters.Extra) > 0
}

func (masters *MasterDeck) Normalize() {
	if masters == nil {
		return
	}
	masters.Version = masterDeckVersion
	if masters.Base.ID == "" {
		masters.Base.ID = "base"
	}
	if masters.Base.Name == "" {
		masters.Base.Name = "Base Master"
	}
	masters.Base.Slide.PageNumber = normalizePageNumberMode(masters.Base.Slide.PageNumber)
	if masters.Base.Slide.PageNumber == pageNumberShow {
		ensurePageNumberElement(&masters.Base.Slide, masters.Base.ID)
	} else {
		removePageNumberElements(&masters.Base.Slide)
	}
	normalizeMasterSlide(&masters.Base.Slide, "base")
	seenLayouts := map[string]bool{masters.Base.ID: true}
	for index := range masters.Layouts {
		layout := &masters.Layouts[index]
		if layout.ID == "" || seenLayouts[layout.ID] {
			layout.ID = newStableID("layout")
		}
		seenLayouts[layout.ID] = true
		if strings.TrimSpace(layout.Name) == "" {
			layout.Name = fmt.Sprintf("Layout %d", index+1)
		}
		layout.Slide.PageNumber = normalizePageNumberMode(layout.Slide.PageNumber)
		if layout.Slide.PageNumber == pageNumberShow {
			ensurePageNumberElement(&layout.Slide, layout.ID)
		} else {
			removePageNumberElements(&layout.Slide)
		}
		normalizeMasterSlide(&layout.Slide, layout.ID)
		if layout.ID == activityLayoutID {
			normalizeActivityMaster(layout)
		}
	}
}

func normalizeActivityMaster(layout *MasterLayout) {
	if layout == nil {
		return
	}
	for index := range layout.Slide.Elements {
		element := &layout.Slide.Elements[index]
		switch element.PlaceholderRole {
		case activityTitleRole:
			element.Kind = "heading"
			element.Level = 2
		case activityQRCodeRole:
			element.Kind = "code"
			values, _ := url.ParseQuery(element.Query)
			if values.Get("valign") == "middle" && values.Get("top") == "" {
				values.Del("valign")
				values.Set("top", "12")
			} else if values.Get("top") == "9" {
				values.Set("top", "12")
			}
			element.Query = encodeQueryStable(values)
		case activityURLRole:
			element.Kind = "text"
		}
	}
}

func normalizePageNumberMode(mode string) string {
	switch mode {
	case pageNumberShow, pageNumberHide:
		return mode
	default:
		return ""
	}
}

func nextPageNumberOverride(mode string) string {
	switch normalizePageNumberMode(mode) {
	case pageNumberShow:
		return pageNumberHide
	case pageNumberHide:
		return ""
	default:
		return pageNumberShow
	}
}

func pageNumberModeLabel(mode string, allowInherit bool) string {
	switch normalizePageNumberMode(mode) {
	case pageNumberShow:
		return "show"
	case pageNumberHide:
		return "hide"
	default:
		if allowInherit {
			return "inherit"
		}
		return "hide"
	}
}

func normalizeMasterSlide(slide *Slide, prefix string) {
	if slide == nil {
		return
	}
	seen := map[string]bool{}
	for index := range slide.Elements {
		element := &slide.Elements[index]
		if element.ID == "" || seen[element.ID] {
			element.ID = newStableID(prefix + "-element")
		}
		seen[element.ID] = true
		if standardElementKind(element.Kind) && element.PlaceholderRole != "" {
			if element.SlotID == "" {
				element.SlotID = element.ID
			}
			element.PlaceholderRole = normalizePlaceholderRole(element.PlaceholderRole)
		}
	}
}

func normalizePlaceholderRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case placeholderTitle:
		return placeholderTitle
	case placeholderSubtitle:
		return placeholderSubtitle
	case placeholderCode:
		return placeholderCode
	case placeholderImage:
		return placeholderImage
	case activityTitleRole:
		return activityTitleRole
	case activityQRCodeRole:
		return activityQRCodeRole
	case activityURLRole:
		return activityURLRole
	default:
		return placeholderBody
	}
}

func encodeMasterDeckMetadata(masters MasterDeck) (string, error) {
	masters.Normalize()
	payload, err := json.Marshal(masters)
	if err != nil {
		return "", err
	}
	if len(payload) > maxMasterMetadataBytes {
		return "", fmt.Errorf("masters metadata exceeds %d bytes", maxMasterMetadataBytes)
	}
	return fmt.Sprintf("<!-- keynope-masters version=%d base64:%s -->", masterDeckVersion, base64.StdEncoding.EncodeToString(payload)), nil
}

func encodeMasterDeckMetadataForPath(masters MasterDeck, deckPath string) (string, error) {
	stored := masters.Clone()
	base := filepath.Dir(deckPath)
	visitMasterImages(&stored, func(path string) string {
		if path == "" || !filepath.IsAbs(path) {
			return path
		}
		if relative, err := filepath.Rel(base, path); err == nil && !strings.HasPrefix(relative, "..") {
			return relative
		}
		return path
	})
	return encodeMasterDeckMetadata(stored)
}

func resolveMasterDeckImagePaths(masters *MasterDeck, base string) {
	visitMasterImages(masters, func(path string) string {
		if path == "" || filepath.IsAbs(path) || isExternalImageSource(path) {
			return path
		}
		return filepath.Clean(filepath.Join(base, path))
	})
}

func visitMasterImages(masters *MasterDeck, transform func(string) string) {
	if masters == nil || transform == nil {
		return
	}
	visit := func(slide *Slide) {
		for index := range slide.Elements {
			if slide.Elements[index].Kind == "image" {
				slide.Elements[index].Path = transform(slide.Elements[index].Path)
			}
		}
	}
	visit(&masters.Base.Slide)
	for index := range masters.Layouts {
		visit(&masters.Layouts[index].Slide)
	}
}

func decodeMasterDeckMetadata(text string) (MasterDeck, string, error) {
	payload, remaining, err := masterMetadataEnvelope.decode(text)
	if err != nil || payload == nil {
		return MasterDeck{}, text, err
	}
	if strings.TrimSpace(string(payload)) == "null" {
		return MasterDeck{}, text, fmt.Errorf("master deck needs a JSON object")
	}
	var masters MasterDeck
	if err := json.Unmarshal(payload, &masters); err != nil {
		return MasterDeck{}, text, fmt.Errorf("parse master deck: %w", err)
	}
	if masters.Version != 0 && masters.Version != masterDeckVersion {
		return MasterDeck{}, text, fmt.Errorf("unsupported master deck payload version %d", masters.Version)
	}
	masters.Normalize()
	return masters, remaining, nil
}

func (masters MasterDeck) Layout(id string) (MasterLayout, bool) {
	for _, layout := range masters.Layouts {
		if layout.ID == id {
			return layout, true
		}
	}
	return MasterLayout{}, false
}

func (masters MasterDeck) LayoutIndex(id string) int {
	for index, layout := range masters.Layouts {
		if layout.ID == id {
			return index
		}
	}
	return -1
}

func (masters MasterDeck) Clone() MasterDeck {
	out := masters
	out.Extra = cloneJSONExtensions(masters.Extra)
	out.Base.Extra = cloneJSONExtensions(masters.Base.Extra)
	out.Base.Slide = cloneSlide(masters.Base.Slide)
	if masters.Layouts != nil {
		out.Layouts = make([]MasterLayout, len(masters.Layouts))
	}
	for index, layout := range masters.Layouts {
		out.Layouts[index] = layout
		out.Layouts[index].Extra = cloneJSONExtensions(layout.Extra)
		out.Layouts[index].Slide = cloneSlide(layout.Slide)
	}
	return out
}

// cloneDeckForRender owns all mutable document metadata but borrows immutable
// asset payload bytes. Renderers must never modify encoded media in place.
// Imported/replaced assets are assigned as new values, not edited byte-by-byte.
// History and persistence snapshots still use cloneDeck's deep ownership.
func cloneDeckForRender(deck Deck) Deck {
	assets := deck.Assets
	typefaces := deck.Typefaces
	deck.Typefaces = nil
	deck.Assets = nil
	out := cloneDeck(deck)
	out.Typefaces = cloneTypefaces(typefaces, false)
	if len(assets) > 0 {
		out.Assets = make(map[string]DeckAsset, len(assets))
		for id, asset := range assets {
			out.Assets[id] = cloneDeckAssetContent(asset, false)
		}
	}
	return out
}

// Scene geometry never consumes saved activity responses. Omit archives before
// taking the owned render snapshot, rather than copying private result payloads
// from every slide and then discarding them during scene encoding. Keep activity
// definitions: resolving activity artwork still needs them. This is deliberately
// separate from general rendering/history snapshots that can need the results.
func cloneDeckForScene(deck Deck) Deck {
	deck.Slides = append([]Slide(nil), deck.Slides...)
	for i := range deck.Slides {
		deck.Slides[i].EngagementResult = nil
	}
	deck.Masters.Base.Slide.EngagementResult = nil
	deck.Masters.Layouts = append([]MasterLayout(nil), deck.Masters.Layouts...)
	for i := range deck.Masters.Layouts {
		deck.Masters.Layouts[i].Slide.EngagementResult = nil
	}
	return cloneDeckForRender(deck)
}

func cloneDeckAsset(asset DeckAsset) DeckAsset {
	return cloneDeckAssetContent(asset, true)
}

// Render snapshots borrow immutable pixels, but own every mutable metadata
// container. History/clipboard snapshots own both pixels and metadata.
func cloneDeckAssetContent(asset DeckAsset, copyPixels bool) DeckAsset {
	asset.Extra = cloneJSONExtensions(asset.Extra)
	if asset.Vector != nil {
		vector := *asset.Vector
		vector.Extra = cloneJSONExtensions(vector.Extra)
		if copyPixels {
			vector.Data = append([]byte(nil), vector.Data...)
		}
		asset.Vector = &vector
	}
	if asset.Source != nil {
		source := *asset.Source
		source.Extra = cloneJSONExtensions(source.Extra)
		if copyPixels {
			source.Data = append([]byte(nil), source.Data...)
		}
		source.Frames = append([]DeckAssetFrame(nil), source.Frames...)
		for i := range source.Frames {
			source.Frames[i].Extra = cloneJSONExtensions(source.Frames[i].Extra)
			if copyPixels {
				source.Frames[i].Data = append([]byte(nil), source.Frames[i].Data...)
			}
		}
		asset.Source = &source
	}
	if copyPixels {
		asset.Data = append([]byte(nil), asset.Data...)
	}
	if len(asset.Frames) > 0 {
		asset.Frames = append([]DeckAssetFrame(nil), asset.Frames...)
		for i := range asset.Frames {
			asset.Frames[i].Extra = cloneJSONExtensions(asset.Frames[i].Extra)
			if copyPixels {
				asset.Frames[i].Data = append([]byte(nil), asset.Frames[i].Data...)
			}
		}
	}
	return asset
}

func cloneDeck(deck Deck) Deck {
	out := Deck{Slides: cloneSlides(deck.Slides), Masters: deck.Masters.Clone()}
	out.PageSize = deck.PageSize.Clone()
	out.Objects = deck.Objects.Clone()
	out.Theme = deck.Theme.Clone()
	out.Typefaces = cloneTypefaces(deck.Typefaces, true)
	out.Appearance = deck.Appearance.Clone()
	out.Diagnostics = append([]documentDiagnostic(nil), deck.Diagnostics...)
	out.HideActivityQR = deck.HideActivityQR
	if deck.Tabs != nil {
		out.Tabs = make([]DeckTab, len(deck.Tabs))
		copy(out.Tabs, deck.Tabs)
	}
	for index := range out.Tabs {
		out.Tabs[index].Extra = cloneJSONExtensions(deck.Tabs[index].Extra)
	}
	out.Extra = cloneJSONExtensions(deck.Extra)
	if deck.Assets != nil {
		out.Assets = make(map[string]DeckAsset, len(deck.Assets))
		for id, asset := range deck.Assets {
			out.Assets[id] = cloneDeckAsset(asset)
		}
	}
	return out
}

func (deck Deck) ResolvedSlides() []Slide {
	resolved := make([]Slide, len(deck.Slides))
	cols, rows := authoredRenderSize(245, 56)
	for index := range deck.Slides {
		resolved[index] = deck.slideRenderPreview(index, cols, rows)
	}
	return resolved
}

// Render only the requested slide during local editing. Resolving every scene
// makes each keystroke pay for unrelated slides and their media conversions.
func (deck Deck) slideRenderPreview(index, cols, rows int) Slide {
	preview := deck.ResolveSlide(index, false)
	return deck.resolvedSlideRenderPreview(preview, index, cols, rows)
}

// The workspace has already resolved this owned slide to test dependencies.
// Reuse it instead of cloning and merging the same masters a second time.
func (deck Deck) resolvedSlideRenderPreview(preview Slide, index, cols, rows int) Slide {
	if index < 0 || index >= len(deck.Slides) {
		return preview
	}
	if scene, err := buildStyledSceneProjection(deck, preview, index, cols, rows, true, nil, nil, &preview.previewGeometry); err == nil {
		preview.ModernScene = &scene
		preview.previewGeometry.scene = preview.ModernScene
	}
	return preview
}

func (deck Deck) masterRenderPreview(index, cols, rows int) Slide {
	preview := deck.applyShapeStrokeDefaults(masterViewPreview(deck.Masters, index))
	preview.ThemeColors = deck.themeColors(deck.AppearanceMode())
	if scene, err := buildStyledSceneProjection(deck, preview, index, cols, rows, true, nil, nil, &preview.previewGeometry); err == nil {
		preview.ModernScene = &scene
		preview.previewGeometry.scene = preview.ModernScene
	}
	return preview
}

func (deck Deck) ResolvedSlidesForEditing() []Slide {
	resolved := make([]Slide, len(deck.Slides))
	for index := range deck.Slides {
		resolved[index] = deck.ResolveSlide(index, true)
	}
	return resolved
}

func (deck Deck) ResolveSlide(index int, includePlaceholders bool) Slide {
	if index < 0 || index >= len(deck.Slides) {
		return Slide{}
	}
	return deck.resolveSlideSource(deck.Slides[index], index, includePlaceholders)
}

// Resolve an owned source without manufacturing unrelated slide entries.
// Preserve the original index for derived page-number identity and text.
func (deck Deck) resolveSlideSource(slide Slide, index int, includePlaceholders bool) Slide {
	source := cloneSlide(slide)
	source.DefaultStyle = deck.slideDefaultStyle(source)
	if source.LayoutID == "" {
		if source.PageNumber == pageNumberShow {
			source.Elements = append(source.Elements, resolvedPageNumberElement(deck.Masters, "", index))
		}
		return deck.applyShapeStrokeDefaults(deck.applyActivityQRSetting(source))
	}
	layout, ok := deck.Masters.Layout(source.LayoutID)
	if !ok {
		return deck.applyShapeStrokeDefaults(deck.applyActivityQRSetting(source))
	}
	resolved := inheritedSlideStyle(deck.Masters, source.LayoutID)
	applySourceStyle(&resolved, source)
	resolved.LayoutID = source.LayoutID
	resolved.ID = source.ID
	resolved.TabID = source.TabID
	resolved.Notes = source.Notes
	resolved.Extra = cloneJSONExtensions(source.Extra)
	resolved.Engagement = cloneEngagement(source.Engagement)
	resolved.EngagementResult = cloneEngagementResult(source.EngagementResult)
	bound := map[string]Element{}
	for _, element := range source.Elements {
		if element.MasterSlotID != "" {
			bound[element.MasterSlotID] = element
		}
	}
	appendMasterElements := func(elements []Element) {
		for _, masterElement := range elements {
			// Effective views are mutable drafts. Never lend a master's opaque
			// metadata buffers to callers that may edit or detach this projection.
			masterElement = cloneElement(masterElement)
			if masterElement.Kind == "page-number" {
				continue
			}
			if source.Engagement != nil && (masterElement.PlaceholderRole == activityTitleRole || masterElement.PlaceholderRole == activityQRCodeRole || masterElement.PlaceholderRole == activityURLRole) {
				effective := resolveActivityElement(masterElement, source.Engagement)
				effective.Inherited = true
				effective.Placeholder = false
				resolved.Elements = append(resolved.Elements, effective)
				continue
			}
			if masterElement.PlaceholderRole == "" {
				inherited := masterElement
				inherited.Inherited = true
				inherited.Placeholder = false
				resolved.Elements = append(resolved.Elements, inherited)
				continue
			}
			slotID := masterElement.SlotID
			if slotID == "" {
				slotID = masterElement.ID
			}
			override, exists := bound[slotID]
			if !exists {
				if includePlaceholders {
					placeholder := masterElement
					placeholder.MasterSlotID = slotID
					placeholder.Placeholder = true
					placeholder.Inherited = false
					resolved.Elements = append(resolved.Elements, placeholder)
				}
				continue
			}
			if override.Placeholder && !includePlaceholders {
				continue
			}
			effective := masterElement
			// Unknown instance fields belong to the authored child, not the
			// template. Do not infer inheritance semantics for opaque metadata.
			effective.Extra = cloneJSONExtensions(override.Extra)
			effective.ID = override.ID
			effective.MasterSlotID = slotID
			effective.Placeholder = override.Placeholder
			effective.Inherited = false
			effective.Query = mergeQueries(masterElement.Query, override.Query)
			if !override.Placeholder {
				effective.Text = override.Text
				effective.Path = override.Path
			}
			resolved.Elements = append(resolved.Elements, effective)
		}
	}
	appendMasterElements(deck.Masters.Base.Slide.Elements)
	appendMasterElements(layout.Slide.Elements)
	if source.Engagement != nil && source.LayoutID == activityLayoutID {
		for _, role := range []string{activityTitleRole, activityQRCodeRole, activityURLRole} {
			found := false
			for _, element := range resolved.Elements {
				if element.PlaceholderRole == role {
					found = true
					break
				}
			}
			if !found {
				if element, ok := activityRoleElement(role); ok {
					element = resolveActivityElement(element, source.Engagement)
					element.Inherited = true
					element.Placeholder = false
					resolved.Elements = append(resolved.Elements, element)
				}
			}
		}
	}
	resolved.PageNumber = deck.effectivePageNumberMode(source)
	if resolved.PageNumber == pageNumberShow {
		resolved.Elements = append(resolved.Elements, resolvedPageNumberElement(deck.Masters, source.LayoutID, index))
	}
	for _, element := range source.Elements {
		if element.MasterSlotID == "" {
			resolved.Elements = append(resolved.Elements, element)
		}
	}
	return deck.applyShapeStrokeDefaults(deck.applyActivityQRSetting(resolved))
}

func (deck Deck) effectivePageNumberMode(source Slide) string {
	mode := deck.Masters.Base.Slide.PageNumber
	if layout, ok := deck.Masters.Layout(source.LayoutID); ok && layout.Slide.PageNumber != "" {
		mode = layout.Slide.PageNumber
	}
	if source.PageNumber != "" {
		mode = source.PageNumber
	}
	return mode
}

func resolvedPageNumberElement(masters MasterDeck, layoutID string, slideIndex int) Element {
	var template Element
	if layout, ok := masters.Layout(layoutID); ok {
		template, _ = pageNumberElement(layout.Slide)
	}
	if template.Kind == "" {
		template, _ = pageNumberElement(masters.Base.Slide)
	}
	if template.Kind == "" {
		// Fallback artwork is a derived consumer, not a newly authored object.
		// A fresh random ID on every resolution made otherwise identical pages
		// change identity and defeated cache/cold parity on detached slides.
		template = defaultPageNumberElement("keynope-default-page-number-" + strconv.Itoa(slideIndex+1))
	}
	template = cloneElement(template)
	template.Text = strconv.Itoa(slideIndex + 1)
	template.Placeholder = false
	template.PlaceholderRole = ""
	template.SlotID = ""
	template.MasterSlotID = ""
	template.Inherited = true
	return template
}

func pageNumberElement(slide Slide) (Element, bool) {
	for _, element := range slide.Elements {
		if element.Kind == "page-number" {
			return element, true
		}
	}
	return Element{}, false
}

func removePageNumberElements(slide *Slide) {
	if slide == nil {
		return
	}
	filtered := slide.Elements[:0]
	for _, element := range slide.Elements {
		if element.Kind != "page-number" {
			filtered = append(filtered, element)
		}
	}
	slide.Elements = filtered
}

func ensurePageNumberElement(slide *Slide, idPrefix string) {
	if slide == nil {
		return
	}
	if _, ok := pageNumberElement(*slide); ok {
		return
	}
	slide.Elements = append(slide.Elements, defaultPageNumberElement(newStableID(idPrefix+"-page-number")))
}

func inheritedSlideStyle(masters MasterDeck, layoutID string) Slide {
	resolved := Slide{}
	applyMasterStyle := func(layer Slide) {
		if validElementStyle(layer.DefaultStyle) {
			resolved.DefaultStyle = layer.DefaultStyle
		}
		if layer.TTFSize > 0 {
			resolved.TTFSize = layer.TTFSize
		}
		if layer.TTFWidth > 0 {
			resolved.TTFWidth = layer.TTFWidth
		}
		if layer.EffectSet || layer.Effect != "" {
			resolved.Effect = layer.Effect
			resolved.EffectSet = true
		}
		if layer.BackgroundSet || layer.Background != "" {
			resolved.Background = layer.Background
			resolved.BackgroundSet = true
		}
		if layer.BackgroundMediaSet {
			resolved.BackgroundMedia = cloneSlideBackgroundMedia(layer.BackgroundMedia)
			resolved.BackgroundMediaSet = true
		}
		if layer.FGSet || layer.FG != "" {
			resolved.FG = layer.FG
			resolved.FGSet = true
		}
		if layer.BGSet || layer.BG != "" {
			resolved.BG = layer.BG
			resolved.BGSet = true
		}
		if layer.HeaderFGSet || layer.HeaderFG != "" {
			resolved.HeaderFG = layer.HeaderFG
			resolved.HeaderFGSet = true
		}
	}
	applyMasterStyle(masters.Base.Slide)
	if layout, ok := masters.Layout(layoutID); ok {
		applyMasterStyle(layout.Slide)
	}
	return resolved
}

func applySourceStyle(resolved *Slide, source Slide) {
	if resolved == nil {
		return
	}
	if validElementStyle(source.DefaultStyle) {
		resolved.DefaultStyle = source.DefaultStyle
	}
	if source.TTFSize > 0 {
		resolved.TTFSize = source.TTFSize
	}
	if source.TTFWidth > 0 {
		resolved.TTFWidth = source.TTFWidth
	}
	if source.EffectSet {
		resolved.Effect = source.Effect
		resolved.EffectSet = true
	}
	if source.BackgroundSet {
		resolved.Background = source.Background
		resolved.BackgroundSet = true
	}
	if source.BackgroundMediaSet {
		resolved.BackgroundMedia = cloneSlideBackgroundMedia(source.BackgroundMedia)
		resolved.BackgroundMediaSet = true
	}
	if source.FGSet {
		resolved.FG = source.FG
		resolved.FGSet = true
	}
	if source.BGSet {
		resolved.BG = source.BG
		resolved.BGSet = true
	}
	if source.HeaderFGSet {
		resolved.HeaderFG = source.HeaderFG
		resolved.HeaderFGSet = true
	}
}

func (deck *Deck) StoreResolvedSlide(index int, effective Slide) {
	if deck == nil || index < 0 || index >= len(deck.Slides) {
		return
	}
	// Outline defaults belong to this projection, not the authored slide or
	// its undo snapshots. They will be resolved from the current theme again.
	effective.ShapeStrokeDefaults = nil
	source := deck.Slides[index]
	if source.LayoutID == "" {
		var local []Element
		for _, element := range effective.Elements {
			if element.Inherited {
				continue
			}
			element.Inherited = false
			local = append(local, element)
		}
		effective.Elements = local
		deck.Slides[index] = effective
		return
	}
	masterSlots := deck.masterSlotMap(source.LayoutID)
	next := source
	next.Elements = nil
	next.Notes = effective.Notes
	inherited := inheritedSlideStyle(deck.Masters, source.LayoutID)
	setStyleOverrides(&next, inherited, effective)
	for _, element := range effective.Elements {
		if element.Inherited {
			continue
		}
		if element.MasterSlotID == "" {
			element.Inherited = false
			next.Elements = append(next.Elements, element)
			continue
		}
		masterElement, ok := masterSlots[element.MasterSlotID]
		if !ok {
			element.MasterSlotID = ""
			element.Inherited = false
			next.Elements = append(next.Elements, element)
			continue
		}
		overrideQuery := queryDifference(masterElement.Query, element.Query)
		if element.Placeholder && overrideQuery == "" && element.Extra == nil && element.AssetID == "" && element.ObjectID == "" {
			continue
		}
		override := Element{
			Kind:         element.Kind,
			Level:        element.Level,
			Text:         element.Text,
			Path:         element.Path,
			ID:           element.ID,
			MasterSlotID: element.MasterSlotID,
			Query:        overrideQuery,
			Placeholder:  element.Placeholder,
			AssetID:      element.AssetID,
			ObjectID:     element.ObjectID,
			Extra:        cloneJSONExtensions(element.Extra),
		}
		if override.ID == "" {
			override.ID = newStableID("slide-element")
		}
		next.Elements = append(next.Elements, override)
	}
	deck.Slides[index] = next
}

func stripRuntimeElementState(elements []Element) []Element {
	out := make([]Element, len(elements))
	copy(out, elements)
	for index := range out {
		out[index].Inherited = false
	}
	return out
}

func setStyleOverrides(target *Slide, inherited, effective Slide) {
	target.TTFSize, target.TTFWidth = 0, 0
	if effective.TTFSize != inherited.TTFSize {
		target.TTFSize = effective.TTFSize
	}
	if effective.TTFWidth != inherited.TTFWidth {
		target.TTFWidth = effective.TTFWidth
	}
	target.EffectSet = effective.Effect != inherited.Effect
	target.BackgroundSet = effective.Background != inherited.Background
	target.BackgroundMediaSet = !sameSlideBackgroundMedia(effective.BackgroundMedia, inherited.BackgroundMedia)
	target.FGSet = effective.FG != inherited.FG
	target.BGSet = effective.BG != inherited.BG
	target.HeaderFGSet = effective.HeaderFG != inherited.HeaderFG
	target.Effect = ""
	target.Background = ""
	target.BackgroundMedia = nil
	target.FG = ""
	target.BG = ""
	target.HeaderFG = ""
	if target.EffectSet {
		target.Effect = effective.Effect
	}
	if target.BackgroundSet {
		target.Background = effective.Background
	}
	if target.BackgroundMediaSet {
		target.BackgroundMedia = cloneSlideBackgroundMedia(effective.BackgroundMedia)
	}
	if target.FGSet {
		target.FG = effective.FG
	}
	if target.BGSet {
		target.BG = effective.BG
	}
	if target.HeaderFGSet {
		target.HeaderFG = effective.HeaderFG
	}
}

func sameSlideBackgroundMedia(a, b *slideBackgroundMedia) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.AssetID == b.AssetID && a.Fit == b.Fit && a.Opacity == b.Opacity && a.OpacitySet == b.OpacitySet && a.Query == b.Query
}

func (deck Deck) masterSlotMap(layoutID string) map[string]Element {
	out := map[string]Element{}
	appendSlots := func(elements []Element) {
		for _, element := range elements {
			if element.PlaceholderRole == "" {
				continue
			}
			id := element.SlotID
			if id == "" {
				id = element.ID
			}
			out[id] = element
		}
	}
	appendSlots(deck.Masters.Base.Slide.Elements)
	if layout, ok := deck.Masters.Layout(layoutID); ok {
		appendSlots(layout.Slide.Elements)
	}
	return out
}

func (deck Deck) masterSlots(layoutID string) []Element {
	var out []Element
	appendSlots := func(elements []Element) {
		for _, element := range elements {
			if element.PlaceholderRole != "" {
				out = append(out, element)
			}
		}
	}
	appendSlots(deck.Masters.Base.Slide.Elements)
	if layout, ok := deck.Masters.Layout(layoutID); ok {
		appendSlots(layout.Slide.Elements)
	}
	return out
}

func (deck Deck) NewSlideFromLayout(layoutID string) Slide {
	if _, ok := deck.Masters.Layout(layoutID); !ok {
		return Slide{}
	}
	return Slide{LayoutID: layoutID}
}

func (deck *Deck) RebindSlideLayout(index int, layoutID string) bool {
	if deck == nil || index < 0 || index >= len(deck.Slides) {
		return false
	}
	if _, ok := deck.Masters.Layout(layoutID); !ok {
		return false
	}
	source := deck.Slides[index]
	if source.LayoutID == layoutID {
		return true
	}
	oldSlots := deck.masterSlotMap(source.LayoutID)
	newSlots := deck.masterSlots(layoutID)
	byID := map[string]Element{}
	byRole := map[string][]Element{}
	for _, element := range source.Elements {
		if element.MasterSlotID == "" {
			continue
		}
		if oldMaster, ok := oldSlots[element.MasterSlotID]; ok {
			element.Query = mergeQueries(oldMaster.Query, element.Query)
		}
		byID[element.MasterSlotID] = element
		role := ""
		if oldMaster, ok := oldSlots[element.MasterSlotID]; ok {
			role = oldMaster.PlaceholderRole
		}
		byRole[role] = append(byRole[role], element)
	}
	used := map[string]bool{}
	var rebound []Element
	for _, slot := range newSlots {
		slotID := slot.SlotID
		if slotID == "" {
			slotID = slot.ID
		}
		candidate, ok := byID[slotID]
		if !ok {
			roleCandidates := byRole[slot.PlaceholderRole]
			for len(roleCandidates) > 0 && used[roleCandidates[0].MasterSlotID] {
				roleCandidates = roleCandidates[1:]
			}
			if len(roleCandidates) > 0 {
				candidate = roleCandidates[0]
				ok = true
				byRole[slot.PlaceholderRole] = roleCandidates[1:]
			}
		}
		if !ok {
			continue
		}
		used[candidate.MasterSlotID] = true
		candidate.MasterSlotID = slotID
		candidate.Query = queryDifference(slot.Query, candidate.Query)
		candidate.Kind = slot.Kind
		candidate.Level = slot.Level
		rebound = append(rebound, candidate)
	}
	var local []Element
	// Preserve authored local/unmatched order. Map iteration would randomise
	// overlapping objects' paint order and the next saved Markdown document.
	for _, original := range source.Elements {
		oldID := original.MasterSlotID
		if oldID == "" {
			local = append(local, original)
			continue
		}
		if used[oldID] {
			continue
		}
		element := byID[oldID]
		used[oldID] = true
		element.MasterSlotID = ""
		local = append(local, element)
	}
	source.LayoutID = layoutID
	source.Elements = append(rebound, local...)
	deck.Slides[index] = source
	return true
}

func (deck *Deck) DetachSlide(index int) bool {
	if deck == nil || index < 0 || index >= len(deck.Slides) || deck.Slides[index].LayoutID == "" {
		return false
	}
	pageNumberMode := deck.effectivePageNumberMode(deck.Slides[index])
	effective := deck.ResolveSlide(index, false)
	effective.LayoutID = ""
	removePageNumberElements(&effective)
	if pageNumberMode == pageNumberShow {
		effective.PageNumber = pageNumberShow
	} else {
		effective.PageNumber = ""
	}
	effective.Elements = stripRuntimeElementState(effective.Elements)
	for elementIndex := range effective.Elements {
		effective.Elements[elementIndex].MasterSlotID = ""
		effective.Elements[elementIndex].SlotID = ""
		effective.Elements[elementIndex].PlaceholderRole = ""
	}
	effective.EffectSet = effective.Effect != ""
	effective.BackgroundSet = effective.Background != ""
	effective.BackgroundMediaSet = effective.BackgroundMedia != nil
	effective.FGSet = effective.FG != ""
	effective.BGSet = effective.BG != ""
	effective.HeaderFGSet = effective.HeaderFG != ""
	deck.Slides[index] = effective
	return true
}

func mergeQueries(base, override string) string {
	baseValues, _ := url.ParseQuery(base)
	overrideValues, _ := url.ParseQuery(override)
	for _, key := range strings.Split(overrideValues.Get("master-clear"), ",") {
		if key = strings.TrimSpace(key); key != "" {
			baseValues.Del(key)
		}
	}
	overrideValues.Del("master-clear")
	// Legacy justify is paragraph formatting, unlike left/centre/right which
	// are placement anchors. Let it override a master's newer text-align key.
	if overrideValues.Get("align") == "justify" && !overrideValues.Has("text-align") {
		baseValues.Del("text-align")
	}
	// A child's outline preset and a master's arbitrary stroke are competing
	// representations of one edge, not independent decorations. Resolve older
	// authored overrides too, even when they predate explicit master-clear keys.
	// A child width remains independent and is overlaid below as usual.
	if overrideValues.Has("outline") {
		baseValues.Del("shape-stroke")
		baseValues.Del("shape-stroke-width")
	}
	if overrideValues.Has("shape-stroke") {
		baseValues.Del("outline")
	}
	if overrideValues.Get("label-style-inherit") == "1" && baseValues.Get("shape-label") != "" && overrideValues.Get("shape-label") != "" {
		overrideValues.Set("shape-label", mergeShapeLabelMetadata(baseValues.Get("shape-label"), overrideValues.Get("shape-label")))
	}
	for key, values := range overrideValues {
		baseValues.Del(key)
		for _, value := range values {
			baseValues.Add(key, value)
		}
	}
	return encodeQueryStable(baseValues)
}

func queryDifference(base, effective string) string {
	baseValues, _ := url.ParseQuery(base)
	effectiveValues, _ := url.ParseQuery(effective)
	out := url.Values{}
	var cleared []string
	for key := range baseValues {
		if key == "master-clear" {
			continue
		}
		if _, exists := effectiveValues[key]; !exists {
			cleared = append(cleared, key)
		}
	}
	sort.Strings(cleared)
	if len(cleared) > 0 {
		out.Set("master-clear", strings.Join(cleared, ","))
	}
	for key, values := range effectiveValues {
		if equalStringSlices(baseValues[key], values) {
			continue
		}
		for _, value := range values {
			out.Add(key, value)
		}
	}
	return encodeQueryStable(out)
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func encodeQueryStable(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		for _, value := range values[key] {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func isMasterOverrideQueryKey(key string) bool {
	// Style and geometry suppression must survive the same Markdown round trip
	// as their values. These keys were added after the original legacy list;
	// dropping their clear markers silently restores master values on reopen.
	for _, styleKey := range objectStyleKeys {
		if styleKey != "" && key == styleKey {
			return true
		}
	}
	if key == "label-style-inherit" {
		return true
	}
	if strings.HasPrefix(key, "label-") && labelStyleKey(strings.TrimPrefix(key, "label-")) {
		return true
	}
	switch key {
	case "object-offset-x", "object-offset-y", "shape-offset-x", "shape-offset-y",
		"object-rotation", "text-box", "modern-runs", "theme-fg", "element-style", "image-style", "image-fit", "modern-crop", "modern-mask":
		return true
	case "text-align", "text-valign", "ttf-size", "ttf-weight", "text-bold":
		return true
	case "shape-stroke", "shape-stroke-width":
		return true
	case "connector-mode", "connector-route":
		return true
	case "top", "bottom", "left", "right", "left_pct", "right_pct", "row_delta", "align", "valign", "width", "height", "stretch", "transparent", "orientation", "render", "source", "scale", "text-size", "ttf-width", "font", "fg", "bg", "header", "color", "glyph", "shape", "outline", "brightness", "contrast", "saturation", "tint", "sharpness", "alpha", "gradient-start", "gradient-end", "gradient-dir", "shadow", "shadow-color", "shadow-x", "shadow-y", "link", "slide":
		return true
	default:
		return false
	}
}
