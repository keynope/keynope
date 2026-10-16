package pptx

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Connection struct {
	Target   string
	Site     int
	nativeID int
}

// Normalised shape-transform coordinates, not path coordinates. Outward angle
// uses DrawingML's clockwise 1/60000-degree units and rotates with the shape.
type ConnectionSite struct {
	X, Y  int64
	Angle int64
}

func hasExportBounds(o Object) bool {
	if o.Kind != "line" {
		return o.Width > 0 && o.Height > 0
	}
	if len(o.LinePoints) > 1 {
		for _, p := range o.LinePoints[1:] {
			if p != o.LinePoints[0] {
				return true
			}
		}
		return false
	}
	return o.Width != 0 || o.Height != 0
}

func parseConnection(n *node) *Connection {
	if n == nil {
		return nil
	}
	id, err := strconv.ParseUint(attr(n, "id"), 10, 32)
	index, ei := strconv.ParseUint(attr(n, "idx"), 10, 16)
	if err != nil || ei != nil || id == 0 || index > 1023 {
		return nil
	}
	return &Connection{Target: strconv.FormatUint(id, 10), Site: int(index)}
}

func validateConnections(o Object) error {
	if len(o.ConnectionSites) > 16 || len(o.ConnectionSites) > 0 && o.CustomGeometry == nil {
		return fmt.Errorf("invalid custom connection site count/geometry")
	}
	for _, s := range o.ConnectionSites {
		if s.X < -4*GeometryUnits || s.X > 4*GeometryUnits || s.Y < -4*GeometryUnits || s.Y > 4*GeometryUnits || s.Angle < 0 || s.Angle >= 21600000 {
			return fmt.Errorf("invalid custom connection site")
		}
	}
	for _, c := range []*Connection{o.LineStart, o.LineEnd} {
		if c != nil && (o.Kind != "line" || c.Target == "" || len(c.Target) > 256 || c.Site < 0 || c.Site > 1023) {
			return fmt.Errorf("invalid connector attachment")
		}
	}
	return nil
}

func connectionXML(tag string, c *Connection) string {
	if c == nil || c.nativeID == 0 {
		return ""
	}
	return fmt.Sprintf(`<a:%s id="%d" idx="%d"/>`, tag, c.nativeID, c.Site)
}

func parseConnectionSites(n *node) []ConnectionSite {
	list := findChild(n, "cxnLst")
	if list == nil || len(list.Children) == 0 || len(list.Children) > 16 {
		return nil
	}
	guides := map[string]string{}
	if gd := findChild(n, "gdLst"); gd != nil {
		if len(gd.Children) > 64 {
			return nil
		}
		for _, g := range gd.Children {
			name := attr(g, "name")
			if _, exists := guides[name]; exists {
				return nil
			}
			guides[name] = attr(g, "fmla")
		}
	}
	coordinate := func(name, axis string) (int64, bool) {
		f := strings.Fields(guides[name])
		if len(f) != 4 || f[0] != "*/" || f[1] != axis || f[3] != "1000000" {
			return 0, false
		}
		v, e := strconv.ParseInt(f[2], 10, 32)
		return v, e == nil && math.Abs(float64(v)) <= 4*float64(GeometryUnits)
	}
	var sites []ConnectionSite
	for _, site := range list.Children {
		if site.Name != "cxn" {
			return nil
		}
		pos := findChild(site, "pos")
		x, ex := coordinate(attr(pos, "x"), "w")
		y, ey := coordinate(attr(pos, "y"), "h")
		angle, ea := strconv.ParseInt(attr(site, "ang"), 10, 32)
		if !ex || !ey || ea != nil || angle < 0 || angle >= 21600000 {
			return nil
		}
		sites = append(sites, ConnectionSite{X: x, Y: y, Angle: angle})
	}
	return sites
}

// Resolve both forward and backward links against objects that will actually
// be emitted. Duplicate names and missing/invalid sites never create a wrong ID.
func resolveSlideConnections(o Object, ids map[string]int, sites map[string]int) (Object, []Warning) {
	var warnings []Warning
	resolve := func(c *Connection) *Connection {
		if c == nil {
			return nil
		}
		if ids[c.Target] == 0 || c.Site >= sites[c.Target] {
			warnings = append(warnings, Warning{Object: o.ID, Code: "connector-attachment-omitted", Message: "Missing, ambiguous or unsupported connection site; the explicit line path remains."})
			return nil
		}
		v := *c
		v.nativeID = ids[c.Target]
		return &v
	}
	o.LineStart, o.LineEnd = resolve(o.LineStart), resolve(o.LineEnd)
	return o, warnings
}
