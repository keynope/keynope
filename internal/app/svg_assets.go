package app

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

const svgMediaType = "image/svg+xml"
const maxSVGBytes = 2 << 20

// Versioned separately from the legacy pixel asset. Data is canonical,
// validated passive SVG; Source/Data continue to hold ordinary PNG fallbacks.
type DeckAssetVector struct {
	Extra   *jsonExtensions `json:"-"`
	Version int             `json:"version"`
	MIME    string          `json:"mime"`
	Width   int             `json:"width"`
	Height  int             `json:"height"`
	Data    []byte          `json:"data"`
}

type safeSVGNode struct {
	tag      string
	attrs    map[string]string
	children []*safeSVGNode
}

var svgNumberToken = regexp.MustCompile(`[-+]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][-+]?[0-9]+)?`)
var svgLocalID = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]{0,127}$`)
var svgPaintReference = regexp.MustCompile(`^url\(\s*#([A-Za-z_][A-Za-z0-9_.:-]{0,127})\s*\)$`)
var svgTransformToken = regexp.MustCompile(`^(matrix|translate|scale|rotate|skewX|skewY)\s*\(([^()]*)\)`)

var svgPresentationAttrs = map[string]bool{
	"fill": true, "stroke": true, "color": true, "fill-rule": true,
	"fill-opacity": true, "stroke-opacity": true, "opacity": true,
	"stroke-width": true, "stroke-linecap": true, "stroke-linejoin": true,
	"stroke-miterlimit": true, "stroke-dasharray": true, "stroke-dashoffset": true,
}
var svgGeometryAttrs = map[string]string{
	"svg": "width height viewBox preserveAspectRatio", "g": "",
	"defs": "", "path": "d", "rect": "x y width height rx ry",
	"circle": "cx cy r", "ellipse": "cx cy rx ry", "line": "x1 y1 x2 y2",
	"polygon": "points", "polyline": "points", "use": "href x y",
	"linearGradient": "href x1 y1 x2 y2 gradientUnits gradientTransform spreadMethod",
	"radialGradient": "href cx cy r fx fy gradientUnits gradientTransform spreadMethod",
	"stop":           "offset stop-color stop-opacity",
}

func svgHasAttr(tag, key string) bool {
	return svgPresentationAttrs[key] || key == "id" || key == "transform" ||
		strings.Contains(" "+svgGeometryAttrs[tag]+" ", " "+key+" ")
}

// This lexer validates bounded numeric syntax, not XML or executable content.
// XML structure and all properties are checked separately with an allowlist.
func svgNumbers(value string, commands bool) ([]float64, error) {
	var numbers []float64
	for len(value) > 0 {
		value = strings.TrimLeft(value, " \r\n\t,")
		if value == "" {
			break
		}
		if commands && strings.ContainsRune("MmLlHhVvCcSsQqTtAaZz", rune(value[0])) {
			value = value[1:]
			continue
		}
		location := svgNumberToken.FindStringIndex(value)
		if location == nil || location[0] != 0 {
			return nil, fmt.Errorf("invalid SVG numeric syntax")
		}
		number, err := strconv.ParseFloat(value[:location[1]], 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > 1e6 {
			return nil, fmt.Errorf("SVG coordinate exceeds the supported range")
		}
		numbers = append(numbers, number)
		if len(numbers) > 32768 {
			return nil, fmt.Errorf("SVG path exceeds the complexity limit")
		}
		value = value[location[1]:]
	}
	return numbers, nil
}

func svgLength(value string) (float64, error) {
	value = strings.TrimSpace(value)
	factor := 1.0
	for suffix, scale := range map[string]float64{"px": 1, "pt": 96.0 / 72, "pc": 16, "mm": 96 / 25.4, "cm": 96 / 2.54, "in": 96} {
		if strings.HasSuffix(value, suffix) {
			value = strings.TrimSuffix(value, suffix)
			factor = scale
			break
		}
	}
	n, err := svgNumbers(value, false)
	if err != nil || len(n) != 1 {
		return 0, fmt.Errorf("SVG dimensions must be ordinary numeric lengths")
	}
	return n[0] * factor, nil
}

func svgValidateAttr(tag, key, value string, numberBudget *int) (string, error) {
	value = strings.TrimSpace(value)
	switch key {
	case "id":
		if !svgLocalID.MatchString(value) {
			return "", fmt.Errorf("SVG has an unsupported local ID")
		}
		return value, nil
	case "href":
		if !strings.HasPrefix(value, "#") || !svgLocalID.MatchString(strings.TrimPrefix(value, "#")) {
			return "", fmt.Errorf("SVG external references are not allowed")
		}
		return value, nil
	case "fill", "stroke", "color", "stop-color":
		if value == "currentColor" {
			return value, nil
		}
		if value == "transparent" {
			return "none", nil
		}
		if reference := svgPaintReference.FindStringSubmatch(value); reference != nil && (key == "fill" || key == "stroke") {
			return "url(#" + reference[1] + ")", nil
		}
		if strings.ContainsAny(value, "\\;{}") || strings.Contains(strings.ToLower(value), "url") {
			return "", fmt.Errorf("SVG paint may only reference a local gradient")
		}
		// Restrict function syntax to the form supported by the offline renderer.
		if strings.Contains(value, "(") && !strings.HasPrefix(value, "rgb(") {
			return "", fmt.Errorf("SVG colour function is unsupported; use rgb() or hex colours")
		}
		if _, err := oksvg.ParseSVGColor(value); err != nil {
			return "", fmt.Errorf("SVG contains an unsupported colour")
		}
		return value, nil
	case "fill-rule":
		if value != "nonzero" && value != "evenodd" {
			return "", fmt.Errorf("unsupported SVG fill rule")
		}
		return value, nil
	case "stroke-linecap":
		if value != "butt" && value != "round" && value != "square" {
			return "", fmt.Errorf("unsupported SVG line cap")
		}
		return value, nil
	case "stroke-linejoin":
		if value != "miter" && value != "round" && value != "bevel" {
			return "", fmt.Errorf("unsupported SVG line join")
		}
		return value, nil
	case "gradientUnits":
		if value != "objectBoundingBox" && value != "userSpaceOnUse" {
			return "", fmt.Errorf("unsupported SVG gradient units")
		}
		return value, nil
	case "spreadMethod":
		if value != "pad" && value != "reflect" && value != "repeat" {
			return "", fmt.Errorf("unsupported SVG gradient spread")
		}
		return value, nil
	case "preserveAspectRatio":
		parts := strings.Fields(value)
		if len(parts) < 1 || len(parts) > 2 {
			return "", fmt.Errorf("unsupported SVG aspect ratio")
		}
		if parts[0] != "none" && !regexp.MustCompile(`^x(Min|Mid|Max)Y(Min|Mid|Max)$`).MatchString(parts[0]) {
			return "", fmt.Errorf("unsupported SVG aspect alignment")
		}
		if len(parts) == 2 && parts[1] != "meet" && parts[1] != "slice" {
			return "", fmt.Errorf("unsupported SVG aspect fit")
		}
		return value, nil
	case "transform", "gradientTransform":
		remaining := value
		operations := 0
		for remaining != "" {
			remaining = strings.TrimLeft(remaining, " \r\n\t,")
			if remaining == "" {
				break
			}
			match := svgTransformToken.FindStringSubmatchIndex(remaining)
			if match == nil {
				return "", fmt.Errorf("unsupported SVG transform")
			}
			name := remaining[match[2]:match[3]]
			numbers, err := svgNumbers(remaining[match[4]:match[5]], false)
			if err != nil {
				return "", err
			}
			count := len(numbers)
			valid := name == "matrix" && count == 6 || name == "translate" && (count == 1 || count == 2) || name == "scale" && (count == 1 || count == 2) || name == "rotate" && (count == 1 || count == 3) || (name == "skewX" || name == "skewY") && count == 1
			if !valid {
				return "", fmt.Errorf("invalid SVG transform arguments")
			}
			for _, v := range numbers {
				if math.Abs(v) > 10000 {
					return "", fmt.Errorf("SVG transform exceeds the supported range")
				}
			}
			if (name == "skewX" || name == "skewY") && math.Abs(numbers[0]) >= 85 {
				return "", fmt.Errorf("SVG skew exceeds the supported range")
			}
			operations++
			if operations > 16 {
				return "", fmt.Errorf("too many SVG transforms")
			}
			*numberBudget += count
			if *numberBudget > 65536 {
				return "", fmt.Errorf("SVG exceeds the coordinate budget")
			}
			remaining = remaining[match[1]:]
		}
		return value, nil
	case "stroke-dasharray":
		if value == "none" {
			return value, nil
		}
	}
	if key == "width" || key == "height" || key == "x" || key == "y" || key == "cx" || key == "cy" || key == "r" || key == "rx" || key == "ry" || key == "stroke-width" || key == "stroke-dashoffset" {
		if !strings.HasSuffix(value, "%") {
			n, err := svgLength(value)
			if err != nil {
				return "", err
			}
			value = strconv.FormatFloat(n, 'f', -1, 64)
		}
	}
	percentage := strings.HasSuffix(value, "%")
	if percentage {
		value = strings.TrimSuffix(value, "%")
	}
	numbers, err := svgNumbers(value, key == "d")
	if err != nil || len(numbers) == 0 {
		return "", fmt.Errorf("invalid SVG %s", key)
	}
	*numberBudget += len(numbers)
	if *numberBudget > 65536 {
		return "", fmt.Errorf("SVG exceeds the coordinate budget")
	}
	if key == "viewBox" && len(numbers) != 4 || key == "points" && len(numbers)%2 != 0 || key == "stroke-dasharray" && len(numbers) > 32 {
		return "", fmt.Errorf("invalid or oversized SVG %s", key)
	}
	if key != "viewBox" && key != "points" && key != "stroke-dasharray" && key != "d" && len(numbers) != 1 {
		return "", fmt.Errorf("invalid SVG scalar %s", key)
	}
	if percentage {
		if tag == "svg" && (key == "width" || key == "height") && numbers[0] <= 0 {
			return "", fmt.Errorf("SVG viewport percentage must be positive")
		}
		if key != "offset" && tag != "linearGradient" && tag != "radialGradient" && !(tag == "svg" && (key == "width" || key == "height")) {
			return "", fmt.Errorf("percentage SVG geometry is unsupported; use viewBox coordinates")
		}
		if key == "offset" {
			numbers[0] /= 100
			value = strconv.FormatFloat(numbers[0], 'f', -1, 64)
		} else {
			value += "%"
		}
	}
	if key == "opacity" || key == "fill-opacity" || key == "stroke-opacity" || key == "stop-opacity" || key == "offset" {
		if numbers[0] < 0 || numbers[0] > 1 {
			return "", fmt.Errorf("SVG opacity/offset must be between zero and one")
		}
		if key == "opacity" && (tag == "svg" || tag == "g" || tag == "use" || tag == "defs") && numbers[0] != 1 {
			return "", fmt.Errorf("SVG group opacity is unsupported by the offline fallback; flatten its compositing or import a PNG")
		}
	}
	if key == "stroke-width" || key == "stroke-miterlimit" {
		if numbers[0] < 0 || numbers[0] > 256 {
			return "", fmt.Errorf("SVG stroke exceeds the supported range")
		}
	}
	if key == "stroke-dasharray" {
		sum := 0.0
		for _, n := range numbers {
			if n < 0 {
				return "", fmt.Errorf("negative SVG dash length")
			}
			sum += n
		}
		if sum <= 0 {
			return "", fmt.Errorf("SVG dash pattern must have positive length")
		}
	}
	if key == "width" || key == "height" || key == "r" || key == "rx" || key == "ry" {
		if numbers[0] < 0 {
			return "", fmt.Errorf("negative SVG dimension")
		}
	}
	return value, nil
}

func sanitizeSVG(ctx context.Context, data []byte) ([]byte, int, int, error) {
	if len(data) == 0 || len(data) > maxSVGBytes || !utf8.Valid(data) {
		return nil, 0, 0, fmt.Errorf("SVG must be UTF-8 and at most 2 MiB")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root *safeSVGNode
	var stack []*safeSVGNode
	ids := map[string]*safeSVGNode{}
	nodes, numbers, skip := 0, 0, 0
	skipTag := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, 0, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, 0, fmt.Errorf("invalid SVG XML: %w", err)
		}
		switch t := token.(type) {
		case xml.Directive:
			return nil, 0, 0, fmt.Errorf("SVG DTDs and entity declarations are not allowed")
		case xml.ProcInst:
			if t.Target != "xml" {
				return nil, 0, 0, fmt.Errorf("SVG processing instructions are not allowed")
			}
		case xml.StartElement:
			nodes++
			if nodes > 2048 || len(stack)+skip >= 32 {
				return nil, 0, 0, fmt.Errorf("SVG exceeds the element/depth limit")
			}
			name := t.Name.Local
			attributes := make(map[xml.Name]bool, len(t.Attr))
			for _, a := range t.Attr {
				if attributes[a.Name] {
					return nil, 0, 0, fmt.Errorf("duplicate SVG attribute")
				}
				attributes[a.Name] = true
				if strings.HasPrefix(strings.ToLower(a.Name.Local), "on") {
					return nil, 0, 0, fmt.Errorf("SVG event handlers are not allowed")
				}
			}
			if name == "script" || name == "foreignObject" || strings.HasPrefix(name, "animate") || name == "set" {
				return nil, 0, 0, fmt.Errorf("SVG executable or animated content is not allowed")
			}
			if skip > 0 {
				skip++
				continue
			}
			if name == "metadata" || name == "title" || name == "desc" || name == "style" {
				skip, skipTag = 1, name
				continue
			}
			if t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg" {
				return nil, 0, 0, fmt.Errorf("SVG contains a foreign graphic namespace")
			}
			if _, ok := svgGeometryAttrs[name]; !ok {
				return nil, 0, 0, fmt.Errorf("SVG <%s> is unsupported; convert it to paths or import a PNG", name)
			}
			if name == "svg" && (root != nil || len(stack) > 0) {
				return nil, 0, 0, fmt.Errorf("nested SVG viewports are unsupported")
			}
			node := &safeSVGNode{tag: name, attrs: map[string]string{}}
			style := ""
			for _, a := range t.Attr {
				key := a.Name.Local
				if a.Name.Space == "xmlns" || key == "xmlns" || key == "version" || key == "class" || strings.HasPrefix(key, "aria-") || strings.HasPrefix(key, "data-") {
					continue
				}
				if a.Name.Space != "" && !(a.Name.Space == "http://www.w3.org/1999/xlink" && key == "href") {
					return nil, 0, 0, fmt.Errorf("SVG has an unsupported attribute namespace")
				}
				if key == "style" {
					style = a.Value
					continue
				}
				if !svgHasAttr(name, key) {
					return nil, 0, 0, fmt.Errorf("SVG attribute %q is unsupported", key)
				}
				if _, exists := node.attrs[key]; exists {
					return nil, 0, 0, fmt.Errorf("duplicate SVG attribute")
				}
				value, err := svgValidateAttr(name, key, a.Value, &numbers)
				if err != nil {
					return nil, 0, 0, err
				}
				node.attrs[key] = value
			}
			for _, declaration := range strings.Split(style, ";") {
				if strings.TrimSpace(declaration) == "" {
					continue
				}
				parts := strings.SplitN(declaration, ":", 2)
				if len(parts) != 2 || !svgPresentationAttrs[strings.TrimSpace(parts[0])] {
					return nil, 0, 0, fmt.Errorf("SVG CSS property is unsupported; use presentation attributes")
				}
				key := strings.TrimSpace(parts[0])
				value, err := svgValidateAttr(name, key, parts[1], &numbers)
				if err != nil {
					return nil, 0, 0, err
				}
				node.attrs[key] = value
			}
			if id := node.attrs["id"]; id != "" {
				if ids[id] != nil {
					return nil, 0, 0, fmt.Errorf("SVG local IDs must be unique")
				}
				ids[id] = node
			}
			if len(stack) == 0 {
				if root != nil || name != "svg" {
					return nil, 0, 0, fmt.Errorf("SVG needs one root <svg>")
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			if len(stack) == 0 {
				return nil, 0, 0, fmt.Errorf("invalid SVG structure")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if strings.TrimSpace(string(t)) != "" && (skip == 0 || skipTag == "style") {
				return nil, 0, 0, fmt.Errorf("SVG text/stylesheets are unsupported; convert text to paths or use inline styles")
			}
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, 0, 0, fmt.Errorf("SVG needs a complete root element")
	}
	width, height := 0.0, 0.0
	box, err := svgNumbers(root.attrs["viewBox"], false)
	if err != nil {
		return nil, 0, 0, err
	}
	if len(box) == 4 {
		if box[2] < 1 || box[3] < 1 || box[2] > 20000 || box[3] > 20000 {
			return nil, 0, 0, fmt.Errorf("SVG viewBox must have positive dimensions")
		}
		width, height = box[2], box[3]
	}
	if value := root.attrs["width"]; value != "" && !strings.HasSuffix(value, "%") {
		width, err = svgLength(value)
		if err != nil {
			return nil, 0, 0, err
		}
	}
	if value := root.attrs["height"]; value != "" && !strings.HasSuffix(value, "%") {
		height, err = svgLength(value)
		if err != nil {
			return nil, 0, 0, err
		}
	}
	if width < 1 || height < 1 || width > 20000 || height > 20000 {
		return nil, 0, 0, fmt.Errorf("SVG viewport must be between 1 and 20000 pixels per axis")
	}
	if len(box) == 0 {
		root.attrs["viewBox"] = "0 0 " + svgFloat(width) + " " + svgFloat(height)
	}
	width, height = math.Ceil(width), math.Ceil(height)
	// Persist explicit CSS pixel units; scene dimensions remain authoritative
	// because SVG naturalWidth/Height reporting differs between browsers.
	root.attrs["width"], root.attrs["height"] = svgFloat(width)+"px", svgFloat(height)+"px"
	budget := svgExpansionBudget{}
	normalized, err := normalizeSVGNode(ctx, root, ids, map[string]bool{}, "black", 1, 1, 0, &budget)
	if err != nil {
		return nil, 0, 0, err
	}
	if err := checkSVGTransforms(normalized, rasterx.Identity); err != nil {
		return nil, 0, 0, err
	}
	// Expanding a use must not introduce duplicate resource IDs. Refuse this
	// uncommon structure rather than emitting ambiguous paint references that
	// could differ between the browser and the offline renderer.
	if err := checkSVGResourceIDs(normalized, map[string]bool{}); err != nil {
		return nil, 0, 0, err
	}
	var out bytes.Buffer
	writeSVGNode(&out, normalized, true)
	if out.Len() > maxSVGBytes {
		return nil, 0, 0, fmt.Errorf("expanded SVG exceeds 2 MiB")
	}
	return out.Bytes(), int(math.Ceil(width)), int(math.Ceil(height)), nil
}

func svgFloat(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func checkSVGResourceIDs(n *safeSVGNode, seen map[string]bool) error {
	if id := n.attrs["id"]; id != "" {
		if seen[id] {
			return fmt.Errorf("SVG use duplicates a gradient definition; move shared gradients into root defs")
		}
		seen[id] = true
	}
	for _, child := range n.children {
		if err := checkSVGResourceIDs(child, seen); err != nil {
			return err
		}
	}
	return nil
}

// Bound cumulative transforms, not just individual scale operations. Otherwise
// harmless-looking nested groups can multiply into non-finite/huge geometry.
func checkSVGTransforms(n *safeSVGNode, parent rasterx.Matrix2D) error {
	if gradient := n.attrs["gradientTransform"]; gradient != "" {
		if err := checkSVGTransforms(&safeSVGNode{attrs: map[string]string{"transform": gradient}}, rasterx.Identity); err != nil {
			return err
		}
	}
	matrix, err := svgTransformMatrix(n.attrs["transform"], parent)
	if err != nil {
		return err
	}
	for _, v := range []float64{matrix.A, matrix.B, matrix.C, matrix.D} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1024 {
			return fmt.Errorf("cumulative SVG scale exceeds the supported range")
		}
	}
	for _, v := range []float64{matrix.E, matrix.F} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e6 {
			return fmt.Errorf("cumulative SVG translation exceeds the supported range")
		}
	}
	for _, child := range n.children {
		if err := checkSVGTransforms(child, matrix); err != nil {
			return err
		}
	}
	return nil
}

func svgTransformMatrix(remaining string, parent rasterx.Matrix2D) (rasterx.Matrix2D, error) {
	matrix := parent
	for strings.TrimSpace(remaining) != "" {
		remaining = strings.TrimLeft(remaining, " \r\n\t,")
		parts := svgTransformToken.FindStringSubmatchIndex(remaining)
		if parts == nil {
			return matrix, fmt.Errorf("invalid SVG transform")
		}
		name := remaining[parts[2]:parts[3]]
		values, err := svgNumbers(remaining[parts[4]:parts[5]], false)
		if err != nil {
			return matrix, err
		}
		switch name {
		case "matrix":
			matrix = matrix.Mult(rasterx.Matrix2D{A: values[0], B: values[1], C: values[2], D: values[3], E: values[4], F: values[5]})
		case "translate":
			y := 0.0
			if len(values) > 1 {
				y = values[1]
			}
			matrix = matrix.Translate(values[0], y)
		case "scale":
			y := values[0]
			if len(values) > 1 {
				y = values[1]
			}
			matrix = matrix.Scale(values[0], y)
		case "rotate":
			if len(values) == 3 {
				matrix = matrix.Translate(values[1], values[2])
			}
			matrix = matrix.Rotate(values[0] * math.Pi / 180)
			if len(values) == 3 {
				matrix = matrix.Translate(-values[1], -values[2])
			}
		case "skewX":
			matrix = matrix.SkewX(values[0] * math.Pi / 180)
		case "skewY":
			matrix = matrix.SkewY(values[0] * math.Pi / 180)
		}
		remaining = remaining[parts[1]:]
	}
	return matrix, nil
}

type svgExpansionBudget struct{ nodes, bytes int }

func normalizeSVGNode(ctx context.Context, n *safeSVGNode, ids map[string]*safeSVGNode, chain map[string]bool, currentColour string, fillOpacity, strokeOpacity float64, depth int, budget *svgExpansionBudget) (*safeSVGNode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	budget.nodes++
	budget.bytes += 256 + len(n.tag)*2
	for key, value := range n.attrs {
		budget.bytes += len(key) + len(value)*2
	}
	if budget.nodes > 4096 || budget.bytes > maxSVGBytes || depth > 32 {
		return nil, fmt.Errorf("SVG reference expansion exceeds its limit")
	}
	copy := &safeSVGNode{tag: n.tag, attrs: map[string]string{}}
	for k, v := range n.attrs {
		copy.attrs[k] = v
	}
	// Make implicit SVG corner radii explicit for the offline renderer.
	if n.tag == "rect" {
		if rx, ry := copy.attrs["rx"], copy.attrs["ry"]; rx != "" && ry == "" {
			copy.attrs["ry"] = rx
		} else if ry != "" && rx == "" {
			copy.attrs["rx"] = ry
		}
	}
	if n.tag == "path" {
		if err := validateSVGPath(copy.attrs["d"]); err != nil {
			return nil, fmt.Errorf("unsupported SVG path geometry: %w", err)
		}
	}
	if colour := copy.attrs["color"]; colour != "" && colour != "currentColor" {
		currentColour = colour
	}
	if value := copy.attrs["fill-opacity"]; value != "" {
		fillOpacity, _ = strconv.ParseFloat(value, 64)
	}
	if value := copy.attrs["stroke-opacity"]; value != "" {
		strokeOpacity, _ = strconv.ParseFloat(value, 64)
	}
	if n.tag == "svg" || n.tag == "g" || n.tag == "use" || n.tag == "defs" {
		delete(copy.attrs, "fill-opacity")
		delete(copy.attrs, "stroke-opacity")
	} else if !strings.HasSuffix(n.tag, "Gradient") && n.tag != "stop" {
		if fillOpacity != 1 {
			copy.attrs["fill-opacity"] = svgFloat(fillOpacity)
		}
		if strokeOpacity != 1 {
			copy.attrs["stroke-opacity"] = svgFloat(strokeOpacity)
		}
	}
	if n.tag == "use" {
		id := strings.TrimPrefix(n.attrs["href"], "#")
		target := ids[id]
		if target == nil || chain[id] || target.tag == "svg" || target.tag == "defs" || target.tag == "stop" || strings.HasSuffix(target.tag, "Gradient") {
			return nil, fmt.Errorf("SVG has a missing, cyclic or unsupported local use reference")
		}
		copy.tag = "g"
		delete(copy.attrs, "href")
		delete(copy.attrs, "x")
		delete(copy.attrs, "y")
		x, y := n.attrs["x"], n.attrs["y"]
		if x == "" {
			x = "0"
		}
		if y == "" {
			y = "0"
		}
		copy.attrs["transform"] = strings.TrimSpace(copy.attrs["transform"] + " translate(" + x + " " + y + ")")
		chain[id] = true
		child, err := normalizeSVGNode(ctx, target, ids, chain, currentColour, fillOpacity, strokeOpacity, depth+1, budget)
		delete(chain, id)
		if err != nil {
			return nil, err
		}
		copy.children = []*safeSVGNode{child}
	} else {
		if href := n.attrs["href"]; href != "" {
			id := strings.TrimPrefix(href, "#")
			parent := ids[id]
			if parent == nil || chain[id] || !strings.HasSuffix(n.tag, "Gradient") || !strings.HasSuffix(parent.tag, "Gradient") {
				return nil, fmt.Errorf("SVG has a missing or cyclic local gradient reference")
			}
			chain[id] = true
			inherited, err := normalizeSVGNode(ctx, parent, ids, chain, currentColour, fillOpacity, strokeOpacity, depth+1, budget)
			delete(chain, id)
			if err != nil {
				return nil, err
			}
			for k, v := range inherited.attrs {
				if k != "id" && svgHasAttr(n.tag, k) {
					if _, exists := copy.attrs[k]; !exists {
						copy.attrs[k] = v
					}
				}
			}
			if len(n.children) == 0 {
				copy.children = inherited.children
			}
			delete(copy.attrs, "href")
		}
		if colour := copy.attrs["color"]; colour != "" && colour != "currentColor" {
			currentColour = colour
		}
		for _, child := range n.children {
			normalized, err := normalizeSVGNode(ctx, child, ids, chain, currentColour, fillOpacity, strokeOpacity, depth+1, budget)
			if err != nil {
				return nil, err
			}
			copy.children = append(copy.children, normalized)
		}
	}
	for _, key := range []string{"fill", "stroke", "stop-color"} {
		if copy.attrs[key] == "currentColor" {
			copy.attrs[key] = currentColour
		}
		if match := svgPaintReference.FindStringSubmatch(copy.attrs[key]); match != nil {
			target := ids[match[1]]
			if target == nil || !strings.HasSuffix(target.tag, "Gradient") {
				return nil, fmt.Errorf("SVG paint references a missing local gradient")
			}
		}
	}
	// Resolve use placement before canonicalizing transforms. The offline
	// renderer misinterprets one-argument scale; matrices avoid that ambiguity.
	for _, key := range []string{"transform", "gradientTransform"} {
		if raw := copy.attrs[key]; raw != "" {
			m, err := svgTransformMatrix(raw, rasterx.Identity)
			if err != nil {
				return nil, err
			}
			copy.attrs[key] = "matrix(" + strings.Join([]string{svgFloat(m.A), svgFloat(m.B), svgFloat(m.C), svgFloat(m.D), svgFloat(m.E), svgFloat(m.F)}, " ") + ")"
		}
	}
	if !strings.HasSuffix(copy.tag, "Gradient") {
		delete(copy.attrs, "id")
	}
	return copy, nil
}

func validateSVGPath(source string) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("invalid SVG path geometry")
		}
	}()
	cursor := oksvg.PathCursor{}
	return cursor.CompilePath(source)
}

func writeSVGNode(out *bytes.Buffer, n *safeSVGNode, root bool) {
	out.WriteString("<" + n.tag)
	if root {
		out.WriteString(` xmlns="http://www.w3.org/2000/svg"`)
	}
	keys := make([]string, 0, len(n.attrs))
	for key := range n.attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.WriteString(" " + key + `="`)
		_ = xml.EscapeText(out, []byte(n.attrs[key]))
		out.WriteByte('"')
	}
	out.WriteByte('>')
	for _, child := range n.children {
		writeSVGNode(out, child, false)
	}
	out.WriteString("</" + n.tag + ">")
}

func rasterizeSVG(ctx context.Context, data []byte, width, height int) (result []byte, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = fmt.Errorf("SVG geometry could not be rendered safely")
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.StrictErrorMode)
	if err != nil {
		return nil, fmt.Errorf("unsupported SVG geometry: %w", err)
	}
	if len(icon.SVGPaths) > 4096 || icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil, fmt.Errorf("SVG geometry exceeds the renderer limits")
	}
	// Canonical source's intrinsic viewport is independent of viewBox. Preserve
	// the SVG meet/slice alignment rather than stretching its raster fallback.
	decoder := xml.NewDecoder(bytes.NewReader(data))
	token, _ := decoder.Token()
	root := token.(xml.StartElement)
	aspect := "xMidYMid meet"
	for _, a := range root.Attr {
		if a.Name.Local == "preserveAspectRatio" {
			aspect = a.Value
		}
	}
	w, h := float64(width), float64(height)
	x, y := 0.0, 0.0
	if aspect != "none" {
		scale := math.Min(w/icon.ViewBox.W, h/icon.ViewBox.H)
		if strings.Contains(aspect, "slice") {
			scale = math.Max(w/icon.ViewBox.W, h/icon.ViewBox.H)
		}
		tw, th := icon.ViewBox.W*scale, icon.ViewBox.H*scale
		x, y = (w-tw)/2, (h-th)/2
		if strings.Contains(aspect, "xMin") {
			x = 0
		}
		if strings.Contains(aspect, "xMax") {
			x = w - tw
		}
		if strings.Contains(aspect, "YMin") {
			y = 0
		}
		if strings.Contains(aspect, "YMax") {
			y = h - th
		}
		w, h = tw, th
	}
	icon.Transform = rasterx.Matrix2D{A: w / icon.ViewBox.W, D: h / icon.ViewBox.H,
		E: x - icon.ViewBox.X*w/icon.ViewBox.W, F: y - icon.ViewBox.Y*h/icon.ViewBox.H}
	viewport := icon.Transform
	prepared, err := prepareSVGForRaster(ctx, data, viewport)
	if err != nil {
		return nil, err
	}
	icon, err = oksvg.ReadIconStream(bytes.NewReader(prepared), oksvg.StrictErrorMode)
	if err != nil {
		return nil, err
	}
	icon.Transform = viewport
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	scanner := rasterx.NewScannerGV(width, height, canvas, canvas.Bounds())
	painter := rasterx.NewDasher(width, height, scanner)
	for _, path := range icon.SVGPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path.DrawTransformed(painter, 1, icon.Transform)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// The rasterizer transforms path coordinates, but not stroke widths/dashes.
// Supply pixel-space stroke metrics on leaves without changing the portable
// SVG. Refuse anisotropic/skewed strokes rather than flattening them wrongly.
func prepareSVGForRaster(ctx context.Context, data []byte, viewport rasterx.Matrix2D) ([]byte, error) {
	type style struct {
		matrix               rasterx.Matrix2D
		width                float64
		stroke, dash, offset string
	}
	stack := []style{{matrix: rasterx.Identity, width: 1, stroke: "none"}}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var out bytes.Buffer
	encoder := xml.NewEncoder(&out)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch node := token.(type) {
		case xml.StartElement:
			current := stack[len(stack)-1]
			values := map[string]string{}
			for _, attr := range node.Attr {
				values[attr.Name.Local] = attr.Value
			}
			current.matrix, err = svgTransformMatrix(values["transform"], current.matrix)
			if err != nil {
				return nil, err
			}
			if v, ok := values["stroke"]; ok {
				current.stroke = v
			}
			if v, ok := values["stroke-width"]; ok {
				current.width, _ = strconv.ParseFloat(v, 64)
			}
			if v, ok := values["stroke-dasharray"]; ok {
				current.dash = v
			}
			if v, ok := values["stroke-dashoffset"]; ok {
				current.offset = v
			}
			stack = append(stack, current)
			switch node.Name.Local {
			case "path", "rect", "circle", "ellipse", "line", "polygon", "polyline":
				m := viewport.Mult(current.matrix)
				sx, sy := math.Hypot(m.A, m.B), math.Hypot(m.C, m.D)
				if current.stroke != "none" && (math.Abs(sx-sy) > .02*math.Max(sx, sy) || math.Abs(m.A*m.C+m.B*m.D) > .02*sx*sy) {
					return nil, fmt.Errorf("SVG nonuniform or skewed strokes are unsupported by the offline fallback; convert strokes to paths")
				}
				scale := (sx + sy) / 2
				if current.width*scale > 8192 {
					return nil, fmt.Errorf("SVG stroke exceeds the raster complexity limit")
				}
				// rasterx uses signed 26.6 coordinates. Bound intermediates before
				// conversion, including off-viewport artwork, to avoid wraparound.
				largest := 0.0
				for _, key := range []string{"d", "points", "x", "y", "width", "height", "cx", "cy", "r", "rx", "ry", "x1", "x2", "y1", "y2"} {
					if value := values[key]; value != "" {
						numbers, _ := svgNumbers(value, key == "d")
						for _, number := range numbers {
							largest = math.Max(largest, math.Abs(number))
						}
					}
				}
				if largest*(math.Abs(m.A)+math.Abs(m.C))+math.Abs(m.E) > 16_000_000 || largest*(math.Abs(m.B)+math.Abs(m.D))+math.Abs(m.F) > 16_000_000 {
					return nil, fmt.Errorf("SVG transformed geometry exceeds the raster coordinate range")
				}
				set := func(key, value string) {
					for i := range node.Attr {
						if node.Attr[i].Name.Local == key {
							node.Attr[i].Value = value
							return
						}
					}
					node.Attr = append(node.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: value})
				}
				set("stroke-width", svgFloat(current.width*scale))
				if current.dash != "" && current.dash != "none" {
					numbers, _ := svgNumbers(current.dash, false)
					parts := make([]string, len(numbers))
					for i, n := range numbers {
						parts[i] = svgFloat(n * scale)
					}
					set("stroke-dasharray", strings.Join(parts, " "))
				}
				if current.offset != "" {
					offset, _ := strconv.ParseFloat(current.offset, 64)
					set("stroke-dashoffset", svgFloat(offset*scale))
				}
			}
			token = node
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
		if err := encoder.EncodeToken(token); err != nil {
			return nil, err
		}
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func looksLikeSVG(data []byte) bool {
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}))
	return len(trimmed) > 0 && trimmed[0] == '<'
}

func validateVectorAsset(ctx context.Context, asset DeckAsset) error {
	if asset.MIME == svgMediaType || asset.Source != nil && asset.Source.MIME == svgMediaType {
		return fmt.Errorf("SVG needs a versioned vector source and PNG fallback")
	}
	if asset.Vector == nil {
		return nil
	}
	v := asset.Vector
	if v.Version != 1 || v.MIME != svgMediaType || asset.MIME != "image/png" || asset.Source == nil || asset.Source.MIME != "image/png" || len(asset.Frames) != 0 {
		return fmt.Errorf("unsupported SVG asset version or missing PNG fallback")
	}
	for _, fallback := range []struct {
		data          []byte
		width, height int
	}{
		{asset.Data, asset.Width, asset.Height}, {asset.Source.Data, asset.Source.Width, asset.Source.Height},
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		config, err := png.DecodeConfig(bytes.NewReader(fallback.data))
		if err != nil || config.Width != fallback.width || config.Height != fallback.height || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 4_194_304 {
			return fmt.Errorf("SVG PNG fallback is invalid or exceeds 4 megapixels")
		}
	}
	canonical, w, h, err := sanitizeSVG(ctx, v.Data)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, v.Data) || w != v.Width || h != v.Height {
		return fmt.Errorf("SVG source is not canonical or dimensions do not match")
	}
	return nil
}

func embeddedSVGAsset(ctx context.Context, data []byte) (string, DeckAsset, string, error) {
	canonical, w, h, err := sanitizeSVG(ctx, data)
	if err != nil {
		return "", DeckAsset{}, "", err
	}
	pw, ph := boundedImageSize(w, h, 2048)
	preview, err := rasterizeSVG(ctx, canonical, pw, ph)
	if err != nil {
		return "", DeckAsset{}, "", err
	}
	dw, dh := boundedImageSize(w, h, 384)
	derivative, err := rasterizeSVG(ctx, canonical, dw, dh)
	if err != nil {
		return "", DeckAsset{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return "", DeckAsset{}, "", err
	}
	return makeDeckAssetWithVector(derivative, "image/png", dw, dh, &DeckAssetSource{MIME: "image/png", Width: pw, Height: ph, Data: preview}, &DeckAssetVector{Version: 1, MIME: svgMediaType, Width: w, Height: h, Data: canonical})
}
