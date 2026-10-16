package pptx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const GeometryUnits int64 = 1000000

type GeometryPoint struct{ X, Y int64 }
type GeometryCommand struct {
	Kind   string
	Points []GeometryPoint
}
type GeometryPath struct {
	Fill     bool
	NoStroke bool
	Commands []GeometryCommand
}
type CustomGeometry struct {
	Paths    []GeometryPath
	TextRect [4]int64
}

func cloneCustomGeometry(g *CustomGeometry) *CustomGeometry {
	if g == nil {
		return nil
	}
	out := *g
	out.Paths = append([]GeometryPath(nil), g.Paths...)
	for i := range out.Paths {
		out.Paths[i].Commands = append([]GeometryCommand(nil), g.Paths[i].Commands...)
		for j := range out.Paths[i].Commands {
			out.Paths[i].Commands[j].Points = append([]GeometryPoint(nil), g.Paths[i].Commands[j].Points...)
		}
	}
	return &out
}

func validateCustomGeometry(g *CustomGeometry) error {
	if g == nil {
		return nil
	}
	if len(g.Paths) == 0 || len(g.Paths) > 16 {
		return fmt.Errorf("invalid custom shape path count")
	}
	if g.TextRect[0] < 0 || g.TextRect[1] < 0 || g.TextRect[2] > GeometryUnits || g.TextRect[3] > GeometryUnits || g.TextRect[2] <= g.TextRect[0] || g.TextRect[3] <= g.TextRect[1] {
		return fmt.Errorf("invalid custom shape text rectangle")
	}
	count := 0
	for _, path := range g.Paths {
		open := false
		for _, command := range path.Commands {
			count++
			if count > 2048 {
				return fmt.Errorf("custom shape command limit exceeded")
			}
			points := 0
			switch command.Kind {
			case "move":
				if path.Fill && open {
					return fmt.Errorf("unclosed custom shape contour")
				}
				points = 1
				open = true
			case "line":
				points = 1
				if !open {
					return fmt.Errorf("custom line before move")
				}
			case "cubic":
				points = 3
				if !open {
					return fmt.Errorf("custom curve before move")
				}
			case "close":
				if !open {
					return fmt.Errorf("custom close before move")
				}
				open = false
			default:
				return fmt.Errorf("unsupported custom shape command")
			}
			if len(command.Points) != points {
				return fmt.Errorf("invalid custom shape point count")
			}
			for _, p := range command.Points {
				if p.X < -4*GeometryUnits || p.X > 4*GeometryUnits || p.Y < -4*GeometryUnits || p.Y > 4*GeometryUnits {
					return fmt.Errorf("custom shape coordinate exceeds bounds")
				}
			}
		}
		if len(path.Commands) == 0 || path.Fill && open {
			return fmt.Errorf("invalid open/empty filled custom path")
		}
	}
	return nil
}

func customGeometryXML(g *CustomGeometry, adjustments map[string]int64, connectionSites ...[]ConnectionSite) string {
	var sites []ConnectionSite
	if len(connectionSites) > 0 {
		sites = connectionSites[0]
	}
	var b strings.Builder
	b.WriteString(`<a:custGeom>` + shapeAdjustmentXML(adjustments) + `<a:gdLst>`)
	for i, name := range []string{"knTxL", "knTxT", "knTxR", "knTxB"} {
		axis := "w"
		if i%2 != 0 {
			axis = "h"
		}
		fmt.Fprintf(&b, `<a:gd name="%s" fmla="*/ %s %d %d"/>`, name, axis, g.TextRect[i], GeometryUnits)
	}
	for i, s := range sites {
		fmt.Fprintf(&b, `<a:gd name="knSiteX%d" fmla="*/ w %d 1000000"/><a:gd name="knSiteY%d" fmla="*/ h %d 1000000"/>`, i, s.X, i, s.Y)
	}
	b.WriteString(`</a:gdLst><a:ahLst/><a:cxnLst>`)
	for i, s := range sites {
		fmt.Fprintf(&b, `<a:cxn ang="%d"><a:pos x="knSiteX%d" y="knSiteY%d"/></a:cxn>`, s.Angle, i, i)
	}
	b.WriteString(`</a:cxnLst><a:rect l="knTxL" t="knTxT" r="knTxR" b="knTxB"/><a:pathLst>`)
	for _, path := range g.Paths {
		fill := "none"
		if path.Fill {
			fill = "norm"
		}
		fmt.Fprintf(&b, `<a:path w="%d" h="%d" fill="%s" stroke="%d">`, GeometryUnits, GeometryUnits, fill, boolNumber(!path.NoStroke))
		for _, command := range path.Commands {
			tag := map[string]string{"move": "moveTo", "line": "lnTo", "cubic": "cubicBezTo", "close": "close"}[command.Kind]
			if command.Kind == "close" {
				b.WriteString(`<a:close/>`)
				continue
			}
			fmt.Fprintf(&b, `<a:%s>`, tag)
			for _, p := range command.Points {
				fmt.Fprintf(&b, `<a:pt x="%d" y="%d"/>`, p.X, p.Y)
			}
			fmt.Fprintf(&b, `</a:%s>`, tag)
		}
		b.WriteString(`</a:path>`)
	}
	b.WriteString(`</a:pathLst></a:custGeom>`)
	return b.String()
}

// Bounded numeric geometry only: no formula evaluator, arbitrary XML, arcs or
// unbounded point arrays. Unrepresentable input remains explicitly diagnosed.
func parseCustomGeometry(n *node) (*CustomGeometry, bool) {
	if n == nil {
		return nil, false
	}
	g := &CustomGeometry{TextRect: [4]int64{0, 0, GeometryUnits, GeometryUnits}}
	if guides := findChild(n, "gdLst"); guides != nil {
		if len(guides.Children) > 64 {
			return nil, true
		}
		seen := map[string]bool{}
		for _, guide := range guides.Children {
			name := attr(guide, "name")
			if seen[name] {
				return nil, true
			}
			seen[name] = true
		}
	}
	if rect := findChild(n, "rect"); rect != nil {
		for i, key := range []string{"l", "t", "r", "b"} {
			name := attr(rect, key)
			if name == []string{"knTxL", "knTxT", "knTxR", "knTxB"}[i] {
				found := false
				if guides := findChild(n, "gdLst"); guides != nil {
					for _, guide := range guides.Children {
						if attr(guide, "name") != name {
							continue
						}
						fields := strings.Fields(attr(guide, "fmla"))
						axis := "w"
						if i%2 != 0 {
							axis = "h"
						}
						if len(fields) != 4 || fields[0] != "*/" || fields[1] != axis || fields[3] != "1000000" {
							return nil, true
						}
						v, err := strconv.ParseInt(fields[2], 10, 32)
						if err != nil {
							return nil, true
						}
						g.TextRect[i] = v
						found = true
						break
					}
				}
				if !found {
					return nil, true
				}
			} else {
				// Only the default full-frame rectangle is understood here.
				// Do not silently interpret an unsupported formula as an edge.
				expected := []string{"0", "0", "r", "b"}[i]
				if name != expected {
					return nil, true
				}
			}
		}
	}
	list := findChild(n, "pathLst")
	if list == nil || len(list.Children) > 16 {
		return nil, true
	}
	count := 0
	for _, path := range list.Children {
		if path.Name != "path" {
			return nil, true
		}
		if fill := attr(path, "fill"); fill != "" && fill != "norm" && fill != "none" {
			return nil, true
		}
		if stroke := attr(path, "stroke"); stroke != "" && stroke != "true" && stroke != "1" && stroke != "false" && stroke != "0" {
			return nil, true
		}
		w, ew := strconv.ParseInt(attr(path, "w"), 10, 32)
		h, eh := strconv.ParseInt(attr(path, "h"), 10, 32)
		if ew != nil || eh != nil || w <= 0 || h <= 0 {
			return nil, true
		}
		p := GeometryPath{Fill: attr(path, "fill") != "none", NoStroke: attr(path, "stroke") == "false" || attr(path, "stroke") == "0"}
		for _, command := range path.Children {
			count++
			if count > 2048 {
				return nil, true
			}
			kind := map[string]string{"moveTo": "move", "lnTo": "line", "cubicBezTo": "cubic", "close": "close"}[command.Name]
			if kind == "" {
				return nil, true
			}
			c := GeometryCommand{Kind: kind}
			if len(command.Children) > 3 {
				return nil, true
			}
			for _, point := range command.Children {
				if point.Name != "pt" {
					return nil, true
				}
				x, ex := strconv.ParseInt(attr(point, "x"), 10, 32)
				y, ey := strconv.ParseInt(attr(point, "y"), 10, 32)
				if ex != nil || ey != nil {
					return nil, true
				}
				c.Points = append(c.Points, GeometryPoint{int64(math.Round(float64(x) / float64(w) * float64(GeometryUnits))), int64(math.Round(float64(y) / float64(h) * float64(GeometryUnits)))})
			}
			p.Commands = append(p.Commands, c)
		}
		g.Paths = append(g.Paths, p)
	}
	if validateCustomGeometry(g) != nil {
		return nil, true
	}
	return g, false
}
