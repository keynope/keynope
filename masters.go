package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
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

var masterDeckMetaRE = regexp.MustCompile(`(?m)^<!--\s*keynope-masters\s+version=([0-9]+)\s+base64:([A-Za-z0-9+/=]+)\s*-->\s*`)

type Deck struct {
	Slides  []Slide
	Masters MasterDeck
}

type MasterDeck struct {
	Version int            `json:"version"`
	Base    MasterLayout   `json:"base"`
	Layouts []MasterLayout `json:"layouts"`
}

type MasterLayout struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Slide Slide  `json:"slide"`
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
	if deck == nil || len(deck.Masters.Layouts) > 0 {
		return
	}
	deck.Masters = defaultMasterDeck()
}

func deckHasMasterData(masters MasterDeck) bool {
	return masters.Version != 0 || masters.Base.ID != "" || len(masters.Base.Slide.Elements) > 0 || len(masters.Layouts) > 0
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
		if element.PlaceholderRole != "" {
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
		if path == "" || filepath.IsAbs(path) {
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
	match := masterDeckMetaRE.FindStringSubmatch(text)
	if match == nil {
		return MasterDeck{}, text, nil
	}
	version := 0
	if _, err := fmt.Sscanf(match[1], "%d", &version); err != nil || version != masterDeckVersion {
		return MasterDeck{}, text, fmt.Errorf("unsupported master deck version %q", match[1])
	}
	payload, err := base64.StdEncoding.DecodeString(match[2])
	if err != nil {
		return MasterDeck{}, text, fmt.Errorf("decode master deck: %w", err)
	}
	var masters MasterDeck
	if err := json.Unmarshal(payload, &masters); err != nil {
		return MasterDeck{}, text, fmt.Errorf("parse master deck: %w", err)
	}
	if masters.Version != 0 && masters.Version != masterDeckVersion {
		return MasterDeck{}, text, fmt.Errorf("unsupported master deck payload version %d", masters.Version)
	}
	masters.Normalize()
	return masters, masterDeckMetaRE.ReplaceAllString(text, ""), nil
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
	out.Base.Slide = cloneSlide(masters.Base.Slide)
	out.Layouts = make([]MasterLayout, len(masters.Layouts))
	for index, layout := range masters.Layouts {
		out.Layouts[index] = layout
		out.Layouts[index].Slide = cloneSlide(layout.Slide)
	}
	return out
}

func cloneDeck(deck Deck) Deck {
	return Deck{Slides: cloneSlides(deck.Slides), Masters: deck.Masters.Clone()}
}

func (deck Deck) ResolvedSlides() []Slide {
	resolved := make([]Slide, len(deck.Slides))
	for index := range deck.Slides {
		resolved[index] = deck.ResolveSlide(index, false)
	}
	return resolved
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
	source := cloneSlide(deck.Slides[index])
	if source.LayoutID == "" {
		if source.PageNumber == pageNumberShow {
			source.Elements = append(source.Elements, resolvedPageNumberElement(deck.Masters, "", index))
		}
		return source
	}
	layout, ok := deck.Masters.Layout(source.LayoutID)
	if !ok {
		return source
	}
	resolved := inheritedSlideStyle(deck.Masters, source.LayoutID)
	applySourceStyle(&resolved, source)
	resolved.LayoutID = source.LayoutID
	resolved.Notes = source.Notes
	bound := map[string]Element{}
	for _, element := range source.Elements {
		if element.MasterSlotID != "" {
			bound[element.MasterSlotID] = element
		}
	}
	appendMasterElements := func(elements []Element) {
		for _, masterElement := range elements {
			if masterElement.Kind == "page-number" {
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
	resolved.PageNumber = deck.effectivePageNumberMode(source)
	if resolved.PageNumber == pageNumberShow {
		resolved.Elements = append(resolved.Elements, resolvedPageNumberElement(deck.Masters, source.LayoutID, index))
	}
	for _, element := range source.Elements {
		if element.MasterSlotID == "" {
			resolved.Elements = append(resolved.Elements, element)
		}
	}
	return resolved
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
		template = defaultPageNumberElement("")
	}
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
		if layer.EffectSet || layer.Effect != "" {
			resolved.Effect = layer.Effect
			resolved.EffectSet = true
		}
		if layer.BackgroundSet || layer.Background != "" {
			resolved.Background = layer.Background
			resolved.BackgroundSet = true
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
	if source.EffectSet {
		resolved.Effect = source.Effect
		resolved.EffectSet = true
	}
	if source.BackgroundSet {
		resolved.Background = source.Background
		resolved.BackgroundSet = true
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
		if element.Placeholder && overrideQuery == "" {
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
	target.EffectSet = effective.Effect != inherited.Effect
	target.BackgroundSet = effective.Background != inherited.Background
	target.FGSet = effective.FG != inherited.FG
	target.BGSet = effective.BG != inherited.BG
	target.HeaderFGSet = effective.HeaderFG != inherited.HeaderFG
	target.Effect = ""
	target.Background = ""
	target.FG = ""
	target.BG = ""
	target.HeaderFG = ""
	if target.EffectSet {
		target.Effect = effective.Effect
	}
	if target.BackgroundSet {
		target.Background = effective.Background
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
	var local []Element
	for _, element := range source.Elements {
		if element.MasterSlotID == "" {
			local = append(local, element)
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
	for oldID, element := range byID {
		if used[oldID] {
			continue
		}
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
	switch key {
	case "top", "bottom", "left", "right", "left_pct", "right_pct", "row_delta", "align", "width", "height", "stretch", "transparent", "orientation", "render", "source", "scale", "text-size", "fg", "bg", "header", "color", "glyph", "shape", "outline", "brightness", "contrast", "saturation", "sharpness", "alpha", "link", "slide":
		return true
	default:
		return false
	}
}
