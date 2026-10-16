package pptx

import (
	"archive/zip"
	"fmt"
	"strings"
)

// One bounded, immutable stylesheet per Read, shared across effective themes.
// Definitions are resolved using each table's active palette, not the palette
// in use when the package part first entered this cache.
func loadTableStyles(files map[string]*zip.File, limits Limits, cache *themePartCache) error {
	if cache == nil || cache.tableStylesLoaded {
		return nil
	}
	part, err := typedRelationshipTarget(files, "ppt/presentation.xml", "tableStyles", limits)
	if err != nil {
		return err
	}
	if part == "" {
		cache.tableStylesLoaded = true
		return nil
	}
	file := files[part]
	if file == nil {
		return fmt.Errorf("missing table styles part")
	}
	if file.UncompressedSize64 > 2<<20 {
		return fmt.Errorf("table styles part exceeds 2 MiB")
	}
	root, _, err := readThemePart(files, part, limits, cache)
	if err != nil {
		return err
	}
	if root.Name != "tblStyleLst" {
		return fmt.Errorf("invalid table styles root")
	}
	entries := children(root, "tblStyle")
	if len(entries) > 256 {
		return fmt.Errorf("table styles exceed 256 definitions")
	}
	styles := make(map[string]*node, len(entries))
	for _, style := range entries {
		if !boundedTableStyle(style) {
			return fmt.Errorf("table style exceeds 512 XML nodes")
		}
		id := strings.ToUpper(strings.TrimSpace(attr(style, "styleId")))
		if id == "" || len(id) > 128 || styles[id] != nil {
			return fmt.Errorf("missing, oversized or duplicate table style identity")
		}
		styles[id] = style
	}
	cache.tableStyles, cache.defaultTableStyle, cache.tableStylesLoaded = styles, strings.ToUpper(strings.TrimSpace(attr(root, "def"))), true
	return nil
}

func resolvedTableStyle(pr *node, cache *themePartCache, warn func(string, string)) *node {
	inline, reference := findChild(pr, "tableStyle"), findChild(pr, "tableStyleId")
	if inline != nil && reference != nil {
		warn("table-style-approximated", "An ambiguous inline and referenced table style uses the inline definition.")
	}
	if inline != nil {
		if !boundedTableStyle(inline) {
			warn("table-style-approximated", "An oversized inline table style was omitted; explicit cell formatting was retained.")
			return nil
		}
		return inline
	}
	id := strings.ToUpper(strings.TrimSpace(referenceText(reference)))
	if id == "" && reference == nil && cache != nil {
		id = cache.defaultTableStyle
	}
	if id == "" {
		return nil
	}
	if cache != nil && cache.tableStyles[id] != nil {
		return cache.tableStyles[id]
	}
	warn("table-style-approximated", "The referenced PowerPoint table style was unavailable; explicit cell formatting was retained.")
	return nil
}

func boundedTableStyle(style *node) bool {
	count := 0
	var visit func(*node) bool
	visit = func(n *node) bool {
		if n == nil {
			return true
		}
		count++
		if count > 512 {
			return false
		}
		for _, child := range n.Children {
			if !visit(child) {
				return false
			}
		}
		return true
	}
	return visit(style)
}

func referenceText(n *node) string {
	if n == nil {
		return ""
	}
	return n.Text
}

// Property-choice replacement matters: explicit noFill must remove inherited
// solidFill, and a new scheme colour must not keep old colour transforms.
func tablePropertyKey(name string) string {
	switch name {
	case "solidFill", "noFill", "gradFill", "blipFill", "pattFill", "grpFill":
		return "fill"
	case "srgbClr", "schemeClr", "sysClr", "scrgbClr", "prstClr", "hslClr":
		return "colour"
	}
	return name
}

// Copies only the property spine. Source/style subtrees are immutable and can
// be shared; expanding a style over 10,000 cells never clones its XML payload.
func overlayTableProperties(base, override *node, name string) *node {
	out := &node{Name: name, Attr: map[string]string{}}
	if base != nil {
		for k, v := range base.Attr {
			out.Attr[k] = v
		}
		out.Children = append(out.Children, base.Children...)
	}
	if override == nil {
		return out
	}
	for k, v := range override.Attr {
		out.Attr[k] = v
	}
	for _, child := range override.Children {
		key := tablePropertyKey(child.Name)
		found := -1
		for i, old := range out.Children {
			if tablePropertyKey(old.Name) == key {
				found = i
				break
			}
		}
		if found < 0 {
			out.Children = append(out.Children, child)
			continue
		}
		if strings.HasPrefix(child.Name, "ln") && child.Name != "lnSpc" {
			out.Children[found] = overlayTableProperties(out.Children[found], child, child.Name)
		} else {
			out.Children[found] = child
		}
	}
	return out
}

type tableStyleRegion struct {
	name                     string
	top, left, bottom, right int
}

func tableStyleRegions(pr *node, r, c, rows, cols int) []tableStyleRegion {
	regions := []tableStyleRegion{{"wholeTbl", 0, 0, rows - 1, cols - 1}}
	flag := func(key string) bool { return drawingMLBool(attr(pr, key)) }
	// Conditional parts overlay broad formatting before edge/corner formatting.
	if flag("bandRow") {
		regions = append(regions, tableStyleRegion{[]string{"band1H", "band2H"}[r%2], r, 0, r, cols - 1})
	}
	if flag("bandCol") {
		regions = append(regions, tableStyleRegion{[]string{"band1V", "band2V"}[c%2], 0, c, rows - 1, c})
	}
	if flag("firstCol") && c == 0 {
		regions = append(regions, tableStyleRegion{"firstCol", 0, 0, rows - 1, 0})
	}
	if flag("lastCol") && c == cols-1 {
		regions = append(regions, tableStyleRegion{"lastCol", 0, c, rows - 1, c})
	}
	if flag("firstRow") && r == 0 {
		regions = append(regions, tableStyleRegion{"firstRow", 0, 0, 0, cols - 1})
	}
	if flag("lastRow") && r == rows-1 {
		regions = append(regions, tableStyleRegion{"lastRow", r, 0, r, cols - 1})
	}
	for _, corner := range []struct {
		name, row, col string
		y, x           int
	}{{"nwCell", "firstRow", "firstCol", 0, 0}, {"neCell", "firstRow", "lastCol", 0, cols - 1}, {"swCell", "lastRow", "firstCol", rows - 1, 0}, {"seCell", "lastRow", "lastCol", rows - 1, cols - 1}} {
		if r == corner.y && c == corner.x && flag(corner.row) && flag(corner.col) {
			regions = append(regions, tableStyleRegion{corner.name, r, c, r, c})
		}
	}
	return regions
}

func tableStyleText(part *node, warn func(string, string)) *node {
	text := findChild(part, "tcTxStyle")
	if text == nil {
		return nil
	}
	pr := &node{Name: "defRPr", Attr: map[string]string{}}
	for _, key := range []string{"b", "i"} {
		if value := attr(text, key); value != "" && value != "def" {
			pr.Attr[key] = value
		}
	}
	if ref := findChild(text, "fontRef"); ref != nil {
		face := ""
		switch attr(ref, "idx") {
		case "major":
			face = "+mj-lt"
		case "minor":
			face = "+mn-lt"
		}
		if face != "" {
			pr.Children = append(pr.Children, &node{Name: "latin", Attr: map[string]string{"typeface": face}})
		} else {
			warn("table-style-approximated", "An unsupported table-style font reference was not retained.")
		}
	}
	if font := findChild(text, "font"); font != nil {
		if latin := findChild(font, "latin"); latin != nil {
			pr.Children = append(pr.Children, latin)
		}
	}
	if colour := firstColourChild(text); colour != nil {
		pr.Children = append(pr.Children, &node{Name: "solidFill", Children: []*node{colour}})
	}
	return pr
}

func tableStyleCellProperties(part *node, region tableStyleRegion, r, c int, warn func(string, string)) *node {
	style := findChild(part, "tcStyle")
	pr := &node{Name: "tcPr"}
	if fill := findChild(style, "fill"); fill != nil {
		for _, child := range fill.Children {
			if tablePropertyKey(child.Name) == "fill" {
				pr.Children = append(pr.Children, child)
				break
			}
		}
	}
	if findChild(style, "fillRef") != nil || findChild(style, "effectRef") != nil || findChild(style, "effectLst") != nil || findChild(style, "cell3D") != nil {
		warn("table-style-approximated", "Table-style matrix fills or effects were not retained; supported direct paints remain editable.")
	}
	borders := findChild(style, "tcBdr")
	if findChild(borders, "tl2br") != nil || findChild(borders, "tr2bl") != nil {
		warn("table-style-approximated", "Diagonal table-style borders were not retained.")
	}
	for _, side := range []struct {
		name, inside, output string
		outer                bool
	}{{"left", "insideV", "lnL", c == region.left}, {"right", "insideV", "lnR", c == region.right}, {"top", "insideH", "lnT", r == region.top}, {"bottom", "insideH", "lnB", r == region.bottom}} {
		key := side.name
		if !side.outer {
			key = side.inside
		}
		if edge := findChild(borders, key); edge != nil {
			if line := findChild(edge, "ln"); line != nil {
				copy := *line
				copy.Name = side.output
				pr.Children = append(pr.Children, &copy)
			}
			if findChild(edge, "lnRef") != nil {
				warn("table-style-approximated", "A table-style matrix border was not retained.")
			}
		}
	}
	return pr
}

func styledTableCell(tc, style, pr *node, r, c, rows, cols int, warn func(string, string)) *node {
	var text, cell *node
	if findChild(style, "tblBg") != nil {
		warn("table-style-approximated", "The separate table-style background was not retained; cell fills remain editable.")
	}
	for _, region := range tableStyleRegions(pr, r, c, rows, cols) {
		part := findChild(style, region.name)
		if part == nil {
			continue
		}
		text = overlayTableProperties(text, tableStyleText(part, warn), "defRPr")
		cell = overlayTableProperties(cell, tableStyleCellProperties(part, region, r, c, warn), "tcPr")
	}
	result := *tc
	result.Children = nil
	for _, child := range tc.Children {
		switch child.Name {
		case "tcPr":
			continue
		case "txBody":
			body := *child
			body.Children = nil
			for _, p := range child.Children {
				if p.Name != "p" {
					body.Children = append(body.Children, p)
					continue
				}
				paragraph := *p
				paragraph.Children = nil
				oldPr := findChild(p, "pPr")
				pPr := overlayTableProperties(nil, oldPr, "pPr")
				pPr.Children = nil
				if oldPr != nil {
					for _, item := range oldPr.Children {
						if item.Name != "defRPr" {
							pPr.Children = append(pPr.Children, item)
						}
					}
				}
				pPr.Children = append(pPr.Children, overlayTableProperties(text, findChild(oldPr, "defRPr"), "defRPr"))
				paragraph.Children = append(paragraph.Children, pPr)
				for _, item := range p.Children {
					if item.Name != "pPr" {
						paragraph.Children = append(paragraph.Children, item)
					}
				}
				body.Children = append(body.Children, &paragraph)
			}
			result.Children = append(result.Children, &body)
		default:
			result.Children = append(result.Children, child)
		}
	}
	result.Children = append(result.Children, overlayTableProperties(cell, findChild(tc, "tcPr"), "tcPr"))
	return &result
}
