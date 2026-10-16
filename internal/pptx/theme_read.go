package pptx

import (
	"archive/zip"
	"fmt"
	"path"
	"sort"
	"strings"
)

const officeRelationshipNamespace = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

// Request-local and read-only. Bound both XML input weight and entry count so
// decks with many distinct masters cannot retain an unbounded parse forest.
const themePartCacheBytes = 4 << 20
const themePartCacheEntries = 128

type themePart struct {
	root *node
	rels map[string]string
}

type themePartCache struct {
	entries           map[string]themePart
	bytes             uint64
	tableStylesLoaded bool
	tableStyles       map[string]*node
	defaultTableStyle string
}

func readThemePart(files map[string]*zip.File, part string, limits Limits, cache *themePartCache) (*node, map[string]string, error) {
	if cache != nil {
		if hit, ok := cache.entries[part]; ok {
			return hit.root, hit.rels, nil
		}
	}
	root, rels, err := readPartRootAndRelationships(files, part, limits)
	if err != nil || cache == nil {
		return root, rels, err
	}
	weight := files[part].UncompressedSize64
	relPart := path.Join(path.Dir(part), "_rels", path.Base(part)+".rels")
	if rel := files[relPart]; rel != nil {
		weight += rel.UncompressedSize64
	}
	if len(cache.entries) < themePartCacheEntries && weight <= themePartCacheBytes-cache.bytes {
		if cache.entries == nil {
			cache.entries = make(map[string]themePart)
		}
		cache.entries[part] = themePart{root: root, rels: rels}
		cache.bytes += weight
	}
	return root, rels, nil
}

// Relationship attributes and ordinary numeric IDs are different namespaces.
// Use declarations, not attribute order or a hard-coded 'r' prefix.
func relationshipID(n *node, scopes ...*node) string {
	if n == nil {
		return ""
	}
	value := ""
	for key, candidate := range n.Attr {
		colon := strings.IndexByte(key, ':')
		if colon < 0 || key[colon+1:] != "id" {
			continue
		}
		declaration := "xmlns:" + key[:colon]
		namespace := attr(n, declaration)
		for _, scope := range scopes {
			if namespace != "" {
				break
			}
			namespace = attr(scope, declaration)
		}
		if namespace != officeRelationshipNamespace && namespace != "http://purl.oclc.org/ooxml/officeDocument/relationships" {
			continue
		}
		if value != "" {
			return ""
		}
		value = candidate
	}
	if value != "" {
		return value
	}
	// Preserve bounded legacy fixtures with absent/abbreviated declarations.
	namespace := attr(n, "xmlns:r")
	for _, scope := range scopes {
		if namespace != "" {
			break
		}
		namespace = attr(scope, "xmlns:r")
	}
	if value = attr(n, "r:id"); value != "" && (namespace == "" || namespace == "r") {
		return value
	}
	return attr(n, "id")
}

func typedRelationshipTarget(files map[string]*zip.File, owner, kind string, limits Limits) (string, error) {
	part := path.Join(path.Dir(owner), "_rels", path.Base(owner)+".rels")
	if files[part] == nil {
		return "", nil
	}
	data, err := readPart(files, part, limits.MaxXMLPart)
	if err != nil {
		return "", err
	}
	root, err := parseXML(data, 128)
	if err != nil {
		return "", err
	}
	target := ""
	for _, rel := range children(root, "Relationship") {
		if !strings.HasSuffix(attr(rel, "Type"), "/"+kind) || strings.EqualFold(attr(rel, "TargetMode"), "External") {
			continue
		}
		resolved, ok := resolvePart(owner, attr(rel, "Target"))
		if !ok {
			return "", fmt.Errorf("unsafe %s relationship", kind)
		}
		if target != "" {
			return "", fmt.Errorf("ambiguous %s relationship", kind)
		}
		target = resolved
	}
	return target, nil
}

func defaultColourResolver() colourResolver {
	return colourResolver{colours: map[string]string{}, mapping: map[string]string{"bg1": "lt1", "tx1": "dk1", "bg2": "lt2", "tx2": "dk2"}}
}

func themeColourResolver(files map[string]*zip.File, themePart string, limits Limits, cache *themePartCache) (colourResolver, error) {
	resolver := defaultColourResolver()
	resolver.parts = cache
	if themePart == "" {
		return resolver, nil
	}
	root, _, err := readThemePart(files, themePart, limits, cache)
	if err != nil {
		return resolver, err
	}
	resolver.colourPart, resolver.fontPart = themePart, themePart
	if scheme := findFirst(root, "clrScheme"); scheme != nil {
		for _, child := range scheme.Children {
			if colour, _ := rawColourNode(firstColourChild(child)); colour != "" {
				resolver.colours[child.Name] = colour
			}
		}
	}
	fonts := findFirst(root, "fontScheme")
	resolver.majorFont = attr(findFirst(findChild(fonts, "majorFont"), "latin"), "typeface")
	resolver.minorFont = attr(findFirst(findChild(fonts, "minorFont"), "latin"), "typeface")
	c := resolver.colours
	theme := &Theme{SourcePart: themePart, Name: attr(root, "name"), Dark1: c["dk1"], Light1: c["lt1"], Dark2: c["dk2"], Light2: c["lt2"], Hyperlink: c["hlink"], FollowedHyperlink: c["folHlink"], HeadingFont: resolver.majorFont, BodyFont: resolver.minorFont}
	for i := range theme.Accent {
		theme.Accent[i] = c[fmt.Sprintf("accent%d", i+1)]
	}
	if validateTheme(theme) == nil {
		resolver.theme = theme
	}
	return resolver, nil
}

func (resolver colourResolver) typeface(face string) string {
	switch face {
	case "+mj-lt":
		if resolver.majorFont != "" {
			return resolver.majorFont
		}
	case "+mn-lt":
		if resolver.minorFont != "" {
			return resolver.minorFont
		}
	}
	return face
}

// Shape style references are defaults, below paragraph/list/run formatting.
// idx="none" deliberately does not select a major/minor family.
func shapeThemeFont(n *node, resolver colourResolver) string {
	ref := findChild(findChild(n, "style"), "fontRef")
	switch attr(ref, "idx") {
	case "major":
		return resolver.majorFont
	case "minor":
		return resolver.minorFont
	default:
		return ""
	}
}

func applyColourMap(resolver colourResolver, mapping *node) colourResolver {
	if mapping == nil {
		return resolver
	}
	copy := make(map[string]string, len(resolver.mapping))
	for key, value := range resolver.mapping {
		copy[key] = value
	}
	resolver.mapping = copy
	for key, value := range mapping.Attr {
		if !strings.Contains(key, ":") {
			resolver.mapping[key] = value
		}
	}
	return resolver
}

func applyColourMapOverride(resolver colourResolver, root *node, master map[string]string) (colourResolver, error) {
	if len(children(root, "clrMapOvr")) > 1 {
		return resolver, fmt.Errorf("duplicate colour-map override")
	}
	override := findChild(root, "clrMapOvr")
	if override == nil {
		return resolver, nil
	}
	useMaster, custom := children(override, "masterClrMapping"), children(override, "overrideClrMapping")
	if len(useMaster)+len(custom) != 1 {
		return resolver, fmt.Errorf("ambiguous or missing colour-map override")
	}
	if len(useMaster) == 1 {
		// Maps are immutable; applyColourMap makes a copy before any edit.
		resolver.mapping = master
		return resolver, nil
	}
	return applyColourMap(resolver, custom[0]), nil
}

func masterColourResolver(files map[string]*zip.File, part string, limits Limits, fallback colourResolver) (colourResolver, error) {
	root, _, err := readThemePart(files, part, limits, fallback.parts)
	if err != nil {
		return fallback, err
	}
	theme, err := typedRelationshipTarget(files, part, "theme", limits)
	if err != nil {
		return fallback, err
	}
	if theme != "" {
		fallback, err = themeColourResolver(files, theme, limits, fallback.parts)
		if err != nil {
			return fallback, err
		}
	}
	fallback.textStyles = findChild(root, "txStyles")
	return applyColourMap(fallback, findChild(root, "clrMap")), nil
}

// An override replaces only the schemes it supplies. Keep the declared
// presentation theme identity separate from this slide-local effective paint.
func applyThemeOverride(files map[string]*zip.File, owner string, limits Limits, resolver colourResolver) (colourResolver, error) {
	part, err := typedRelationshipTarget(files, owner, "themeOverride", limits)
	if err != nil || part == "" {
		return resolver, err
	}
	root, _, err := readThemePart(files, part, limits, resolver.parts)
	if err != nil {
		return resolver, err
	}
	if root.Name != "themeOverride" {
		return resolver, fmt.Errorf("invalid theme override root in %s", part)
	}
	for _, kind := range []string{"clrScheme", "fontScheme", "fmtScheme"} {
		if len(children(root, kind)) > 1 {
			return resolver, fmt.Errorf("duplicate %s in theme override", kind)
		}
	}
	if scheme := findChild(root, "clrScheme"); scheme != nil {
		colours := make(map[string]string)
		for _, child := range scheme.Children {
			if colour, _ := rawColourNode(firstColourChild(child)); colour != "" {
				colours[child.Name] = colour
			}
		}
		resolver.colours = colours
		resolver.colourPart = part
	}
	if fonts := findChild(root, "fontScheme"); fonts != nil {
		resolver.fontPart = part
		resolver.majorFont = attr(findChild(findChild(fonts, "majorFont"), "latin"), "typeface")
		resolver.minorFont = attr(findChild(findChild(fonts, "minorFont"), "latin"), "typeface")
	}
	return resolver, nil
}

func slideColourResolver(files map[string]*zip.File, part string, root *node, rels map[string]string, limits Limits, fallback colourResolver) (colourResolver, error) {
	masterMapping := fallback.mapping
	layout, err := typedRelationshipTarget(files, part, "slideLayout", limits)
	if err != nil {
		return fallback, err
	}
	if layout == "" {
		layout = relationshipTargetContaining(rels, "slideLayouts/")
	}
	if layout != "" {
		layoutRoot, layoutRels, err := readThemePart(files, layout, limits, fallback.parts)
		if err != nil {
			return fallback, err
		}
		master, err := typedRelationshipTarget(files, layout, "slideMaster", limits)
		if err != nil {
			return fallback, err
		}
		if master == "" {
			master = relationshipTargetContaining(layoutRels, "slideMasters/")
		}
		if master != "" {
			fallback, err = masterColourResolver(files, master, limits, fallback)
			if err != nil {
				return fallback, err
			}
		}
		masterMapping = fallback.mapping
		fallback, err = applyThemeOverride(files, layout, limits, fallback)
		if err != nil {
			return fallback, err
		}
		fallback, err = applyColourMapOverride(fallback, layoutRoot, masterMapping)
		if err != nil {
			return fallback, err
		}
	}
	fallback, err = applyThemeOverride(files, part, limits, fallback)
	if err != nil {
		return fallback, err
	}
	return applyColourMapOverride(fallback, root, masterMapping)
}

func sortedXMLParts(files map[string]*zip.File, prefix string) []string {
	var result []string
	for name := range files {
		if strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".xml") {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}
