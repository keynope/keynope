package pptx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type LineEnd struct{ Type, Width, Length string }
type LinePoint struct{ X, Y float64 }

// Only constant open polylines are understood; formulas and curves are outside
// this route codec and must not be interpreted as a valid polyline.
func parseLinePoints(n *node, x, y, w, h float64) []LinePoint {
	paths := findFirst(findFirst(n, "custGeom"), "pathLst")
	if paths == nil || len(paths.Children) != 1 {
		return nil
	}
	path := paths.Children[0]
	pw, e1 := strconv.ParseFloat(attr(path, "w"), 64)
	ph, e2 := strconv.ParseFloat(attr(path, "h"), 64)
	if e1 != nil || e2 != nil || pw <= 0 || ph <= 0 || math.IsInf(pw, 0) || math.IsInf(ph, 0) || len(path.Children) > 1024 {
		return nil
	}
	var points []LinePoint
	for i, command := range path.Children {
		if i == 0 && command.Name != "moveTo" || i > 0 && command.Name != "lnTo" {
			return nil
		}
		p := findChild(command, "pt")
		px, e1 := strconv.ParseFloat(attr(p, "x"), 64)
		py, e2 := strconv.ParseFloat(attr(p, "y"), 64)
		if e1 != nil || e2 != nil {
			return nil
		}
		points = append(points, LinePoint{X: x + px/pw*w, Y: y + py/ph*h})
	}
	if len(points) < 2 || validateLinePoints(points) != nil {
		return nil
	}
	return points
}

func validateLinePoints(points []LinePoint) error {
	if len(points) == 1 || len(points) > 1024 {
		return fmt.Errorf("invalid line route vertex count")
	}
	for _, p := range points {
		for _, v := range []float64{p.X, p.Y} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e9 {
				return fmt.Errorf("invalid line route vertex")
			}
		}
	}
	return nil
}

func routedLineGeometry(points []LinePoint) (string, float64, float64, float64, float64) {
	x, y, maxX, maxY := points[0].X, points[0].Y, points[0].X, points[0].Y
	for _, p := range points {
		x = math.Min(x, p.X)
		y = math.Min(y, p.Y)
		maxX = math.Max(maxX, p.X)
		maxY = math.Max(maxY, p.Y)
	}
	w, h := maxX-x, maxY-y
	var path strings.Builder
	for i, p := range points {
		tag := "lnTo"
		if i == 0 {
			tag = "moveTo"
		}
		fmt.Fprintf(&path, `<a:%s><a:pt x="%d" y="%d"/></a:%s>`, tag, emu(p.X-x), emu(p.Y-y), tag)
	}
	geometry := fmt.Sprintf(`<a:custGeom><a:avLst/><a:gdLst/><a:ahLst/><a:cxnLst/><a:rect l="0" t="0" r="r" b="b"/><a:pathLst><a:path w="%d" h="%d" fill="none">%s</a:path></a:pathLst></a:custGeom>`, max(int64(1), emu(w)), max(int64(1), emu(h)), path.String())
	return geometry, x, y, w, h
}

func parseLineEnd(n *node) LineEnd {
	return LineEnd{Type: attr(n, "type"), Width: attr(n, "w"), Length: attr(n, "len")}
}

func validLineEnd(e LineEnd) bool {
	switch e.Type {
	case "", "none", "triangle", "stealth", "diamond", "oval", "arrow":
	default:
		return false
	}
	for _, size := range []string{e.Width, e.Length} {
		switch size {
		case "", "sm", "med", "lg":
		default:
			return false
		}
	}
	return true
}

func lineEndXML(tag string, e LineEnd) string {
	if e.Type == "" {
		e.Type = "none"
	}
	if e.Width == "" {
		e.Width = "med"
	}
	if e.Length == "" {
		e.Length = "med"
	}
	return fmt.Sprintf(`<a:%s type="%s" w="%s" len="%s"/>`, tag, e.Type, e.Width, e.Length)
}

// Line bounds may contain signed endpoint deltas and may have a zero extent on
// one axis. DrawingML needs nonnegative extents; flips preserve the direction.
func connectorXML(o Object, id int, linkID string) string {
	x, y, w, h := o.X, o.Y, o.Width, o.Height
	horizontal, vertical := o.FlipH, o.FlipV
	geometry := `<a:prstGeom prst="line"><a:avLst/></a:prstGeom>`
	if len(o.LinePoints) > 1 {
		geometry, x, y, w, h = routedLineGeometry(o.LinePoints)
	}
	if w < 0 {
		x += w
		w = -w
		horizontal = !horizontal
	}
	if h < 0 {
		y += h
		h = -h
		vertical = !vertical
	}
	link := ""
	if linkID != "" {
		link = `<a:hlinkClick r:id="` + escapeAttr(linkID) + `"/>`
	}
	paint := `<a:noFill/>`
	if o.Stroke != "" {
		paint = objectSolidFill(o.Stroke, o)
	}
	return fmt.Sprintf(`<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="%d" name="%s">%s</p:cNvPr><p:cNvCxnSpPr>%s%s</p:cNvCxnSpPr><p:nvPr/></p:nvCxnSpPr><p:spPr><a:xfrm rot="%d" flipH="%d" flipV="%d"><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm>%s<a:ln w="%d">%s<a:round/>%s%s</a:ln></p:spPr></p:cxnSp>`, id, escapeAttr(objectDisplayName(o)), link, connectionXML("stCxn", o.LineStart), connectionXML("endCxn", o.LineEnd), int64(math.Round(o.Rotation*60000)), boolNumber(horizontal), boolNumber(vertical), emu(x), emu(y), emu(w), emu(h), geometry, int64(math.Round(o.StrokeWidth*12700)), paint, lineEndXML("headEnd", o.LineHead), lineEndXML("tailEnd", o.LineTail))
}
