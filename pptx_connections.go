package main

import (
	"encoding/json"
	"math"
	"net/url"
	"strings"

	"keynope/internal/pptx"
)

var pptxPortSides = []string{"top", "right", "bottom", "left"}

// Only connected bodies need custom connection lists. Keep ordinary presets
// for unconnected shapes; connected shapes retain the exact registry geometry.
func exportPPTXConnections(target *pptx.Slide, source Slide, scene slideScene, attachments *sceneAttachmentGeometry, cols, rows int) []pptx.Warning {
	connected := false
	for _, e := range source.Elements {
		if e.Kind == "connector" {
			connected = true
			break
		}
	}
	if !connected {
		return nil
	}
	var ports []shapePort
	if attachments != nil {
		ports = attachments.ports
	}
	portIndex := indexConnectorPorts(ports)
	objects := map[string]int{}
	for i, o := range target.Objects {
		if _, exists := objects[o.ID]; exists {
			objects[o.ID] = -1
		} else {
			objects[o.ID] = i
		}
	}
	geometry := map[string]*sceneShapeGeometry{}
	for _, o := range scene.Objects {
		if o.Kind == "shape" {
			geometry[o.ID] = o.ShapeGeometry
		}
	}
	prepare := func(id string) bool {
		i, exists := objects[id]
		if !exists || i < 0 || target.Objects[i].Kind != "shape" || geometry[id] == nil {
			return false
		}
		o := &target.Objects[i]
		if len(o.ConnectionSites) == 4 {
			return true
		}
		g, ok := registryPPTXGeometry(geometry[id])
		if !ok {
			return false
		}
		if o.CustomGeometry != nil {
			g = o.CustomGeometry
		}
		var sites []pptx.ConnectionSite
		for side, name := range pptxPortSides {
			p, ok := portIndex[[2]string{id, name}]
			if !ok {
				return false
			}
			px, py := p.X*scene.Width/float64(cols), p.Y*scene.Height/float64(rows)
			cx, cy := o.X+o.Width/2, o.Y+o.Height/2
			s, c := math.Sincos(-o.Rotation * math.Pi / 180)
			dx, dy := px-cx, py-cy
			x, y := cx+dx*c-dy*s, cy+dx*s+dy*c
			angle := []int64{16200000, 0, 5400000, 10800000}[side]
			sites = append(sites, pptx.ConnectionSite{X: int64(math.Round((x - o.X) / o.Width * float64(pptx.GeometryUnits))), Y: int64(math.Round((y - o.Y) / o.Height * float64(pptx.GeometryUnits))), Angle: angle})
		}
		o.CustomGeometry, o.Shape, o.ShapeAdjustments, o.ConnectionSites = g, "custom", customPPTXShapeMarkers(geometry[id].Spec), sites
		return true
	}
	var warnings []pptx.Warning
	for _, e := range source.Elements {
		if e.Kind != "connector" {
			continue
		}
		i, exists := objects[e.ID]
		if !exists || i < 0 {
			continue
		}
		q, _ := url.ParseQuery(e.Query)
		for _, end := range []struct {
			target, side string
			start        bool
		}{{"connector-from", "connector-from-side", true}, {"connector-to", "connector-to-side", false}} {
			id := q.Get(end.target)
			site := -1
			for j, name := range pptxPortSides {
				if name == q.Get(end.side) {
					site = j
				}
			}
			if site < 0 || !prepare(id) {
				warnings = append(warnings, pptx.Warning{Object: e.ID, Code: "connector-attachment-omitted", Message: "Connector keeps its explicit path; its endpoint could not be exported as a native shape connection."})
				continue
			}
			c := &pptx.Connection{Target: id, Site: site}
			if end.start {
				target.Objects[i].LineStart = c
			} else {
				target.Objects[i].LineEnd = c
			}
		}
	}
	return warnings
}

func pptxWorldLinePoints(o pptx.Object) []pptx.LinePoint {
	points := append([]pptx.LinePoint(nil), o.LinePoints...)
	if len(points) < 2 {
		points = []pptx.LinePoint{{X: o.X, Y: o.Y}, {X: o.X + o.Width, Y: o.Y + o.Height}}
	}
	cx, cy := o.X+o.Width/2, o.Y+o.Height/2
	s, c := math.Sincos(o.Rotation * math.Pi / 180)
	for i, p := range points {
		x, y := p.X-cx, p.Y-cy
		if o.FlipH {
			x = -x
		}
		if o.FlipV {
			y = -y
		}
		points[i] = pptx.LinePoint{X: cx + x*c - y*s, Y: cy + x*s + y*c}
	}
	return points
}

// Restore native attached lines only after every body has its final unique ID.
// Unattached/unsupported artwork retains the existing diagnosed shape fallback.
func importPPTXConnections(source pptx.Slide, target *Slide, indices map[int]int, scale, offsetX, offsetY float64, canvas ...sceneRect) (map[string]bool, []pptx.Warning) {
	canvasWidth, canvasHeight := pptxCanvasDimensions(canvas)
	var warnings []pptx.Warning
	objectID := func(i int) string {
		if j, ok := indices[i]; ok {
			return target.Elements[j].ID
		}
		return source.Objects[i].ID
	}
	attached := false
	for i, o := range source.Objects {
		if o.Kind != "line" {
			continue
		}
		if o.Inherited {
			if o.LineStart != nil || o.LineEnd != nil {
				warnings = append(warnings, pptx.Warning{Object: objectID(i), Code: "connector-attachment-approximated", Message: "An inherited Office connector keeps its artwork fallback; cross-part connection identities are not supported."})
			}
			continue
		}
		if o.LineStart != nil && o.LineEnd != nil {
			attached = true
		}
		if (o.LineStart == nil) != (o.LineEnd == nil) {
			warnings = append(warnings, pptx.Warning{Object: objectID(i), Code: "connector-attachment-approximated", Message: "A line with only one supported endpoint uses editable artwork fallback."})
		}
	}
	if !attached {
		return map[string]bool{}, warnings
	}
	ids := map[string]int{}
	for i, o := range source.Objects {
		if o.Inherited {
			continue
		}
		if _, dup := ids[o.ID]; dup {
			ids[o.ID] = -1
		} else {
			ids[o.ID] = i
		}
	}
	portSource := *target
	portSource.canvasWidth, portSource.canvasHeight = canvasWidth, canvasHeight
	ports := slideShapePorts(portSource, layout(withTrueTypeDefaults(portSource), 245, 56), 245, 56)
	byID := map[string][]shapePort{}
	for _, p := range ports {
		byID[p.ID] = append(byID[p.ID], p)
	}
	restored := map[string]bool{}
	for objectIndex, o := range source.Objects {
		if o.Kind != "line" || o.Inherited || o.LineStart == nil || o.LineEnd == nil {
			continue
		}
		targetIndex, exists := indices[objectIndex]
		if !exists || ids[o.ID] != objectIndex {
			continue
		}
		points := pptxWorldLinePoints(o)
		if len(points) > 128 {
			continue
		}
		for i, p := range points {
			points[i] = pptx.LinePoint{X: p.X*scale + offsetX, Y: p.Y*scale + offsetY}
		}
		pick := func(c *pptx.Connection, p pptx.LinePoint) (shapePort, bool, bool) {
			i, exists := ids[c.Target]
			if !exists || i < 0 {
				return shapePort{}, false, false
			}
			j, exists := indices[i]
			if !exists || target.Elements[j].Kind != "shape" {
				return shapePort{}, false, false
			}
			body := source.Objects[i]
			if body.Kind != "shape" {
				return shapePort{}, false, false
			}
			approximate := len(body.ConnectionSites) == 0
			if !approximate {
				if c.Site < 0 || c.Site >= len(body.ConnectionSites) {
					return shapePort{}, false, false
				}
				site := body.ConnectionSites[c.Site]
				x, y := float64(site.X)/float64(pptx.GeometryUnits)*body.Width-body.Width/2, float64(site.Y)/float64(pptx.GeometryUnits)*body.Height-body.Height/2
				if body.FlipH {
					x = -x
				}
				if body.FlipV {
					y = -y
				}
				s, co := math.Sincos(body.Rotation * math.Pi / 180)
				p = pptx.LinePoint{X: (body.X+body.Width/2+x*co-y*s)*scale + offsetX, Y: (body.Y+body.Height/2+x*s+y*co)*scale + offsetY}
			}
			best := shapePort{}
			distance := math.Inf(1)
			for _, port := range byID[target.Elements[j].ID] {
				d := math.Hypot(port.X*canvasWidth/245-p.X, port.Y*canvasHeight/56-p.Y)
				if d < distance {
					best, distance = port, d
				}
			}
			// Do not silently attach far-away artwork to an unrelated side.
			return best, best.ID != "" && distance <= 40, approximate || distance > 1
		}
		a, okA, approxA := pick(o.LineStart, points[0])
		b, okB, approxB := pick(o.LineEnd, points[len(points)-1])
		if !okA || !okB || a.ID == b.ID {
			warnings = append(warnings, pptx.Warning{Object: objectID(objectIndex), Code: "connector-attachment-approximated", Message: "Connector endpoints were missing, ambiguous, self-referencing or outside supported shape ports; editable artwork fallback retained."})
			continue
		}
		q := url.Values{"connector-from": {a.ID}, "connector-from-side": {a.Side}, "connector-to": {b.ID}, "connector-to-side": {b.Side}, "connector-mode": {"straight"}, "fg": {o.Stroke}}
		width := o.StrokeWidth * 2 * scale * 245 / canvasWidth
		q.Set("connector-width", decimal(clampFloat(width, 1, 8)))
		widthClamped := math.Abs(width-clampFloat(width, 1, 8)) > .00001
		if len(points) > 2 {
			route := make([]connectorPoint, len(points))
			valid := true
			for i, p := range points {
				route[i] = connectorPoint{X: p.X * 245 / canvasWidth, Y: p.Y * 56 / canvasHeight}
				if i > 0 {
					if math.Abs(route[i].X-route[i-1].X) < .0001 {
						route[i].X = route[i-1].X
					} else if math.Abs(route[i].Y-route[i-1].Y) < .0001 {
						route[i].Y = route[i-1].Y
					} else {
						valid = false
					}
				}
			}
			if !valid {
				continue
			}
			horizontal := func(p shapePort) bool {
				side := p.Direction
				if side == "" {
					side = p.Side
				}
				return side == "left" || side == "right"
			}
			if horizontal(a) && route[0].Y != route[1].Y || !horizontal(a) && route[0].X != route[1].X {
				route = append([]connectorPoint{route[0]}, route...)
			}
			n := len(route)
			if horizontal(b) && route[n-1].Y != route[n-2].Y || !horizontal(b) && route[n-1].X != route[n-2].X {
				route = append(route, route[n-1])
			}
			if len(route) == 3 {
				route = append([]connectorPoint{route[0]}, route...)
			}
			data, _ := json.Marshal(route)
			if len(decodeConnectorRoute(string(data))) == 0 {
				continue
			}
			q.Set("connector-mode", "elbow")
			q.Set("connector-route", string(data))
		}
		arrows := "none"
		head, tail := o.LineHead.Type != "" && o.LineHead.Type != "none", o.LineTail.Type != "" && o.LineTail.Type != "none"
		if head && tail {
			arrows = "both"
		} else if head {
			arrows = "start"
		} else if tail {
			arrows = "end"
		}
		q.Set("connector-arrows", arrows)
		if head || tail {
			q.Set("connector-arrow-width", decimal(clampFloat(width*4, 2, 20)))
			warnings = append(warnings, pptx.Warning{Object: objectID(objectIndex), Code: "connector-arrow-size-approximated", Message: "Office arrowheads use Keynope's triangular heads and closest supported width."})
		}
		if o.Link != "" {
			q.Set("link", o.Link)
		}
		if o.OpacitySet || o.Opacity > 0 && o.Opacity < 1 {
			q.Set("modern-opacity", decimal(o.Opacity))
		}
		old, _ := url.ParseQuery(target.Elements[targetIndex].Query)
		// The import adapter already resolved source-theme ownership while
		// preparing the artwork fallback. Restoring semantic endpoints must not
		// turn that verified stroke role back into a fixed RGB override.
		if token := old.Get("theme-fg"); validThemeColorToken(token) && !old.Has("fg") {
			q.Del("fg")
			q.Set("theme-fg", token)
		}
		if z := old.Get("z-index"); z != "" {
			q.Set("z-index", z)
		}
		if name := old.Get("object-name"); name != "" {
			q.Set("object-name", name)
		}
		target.Elements[targetIndex].Kind = "connector"
		target.Elements[targetIndex].Text = ""
		target.Elements[targetIndex].Query = q.Encode()
		restored[target.Elements[targetIndex].ID] = true
		if widthClamped {
			warnings = append(warnings, pptx.Warning{Object: objectID(objectIndex), Code: "connector-width-clamped", Message: "Connector width was clamped to Keynope's supported range."})
		}
		if approxA || approxB {
			warnings = append(warnings, pptx.Warning{Object: objectID(objectIndex), Code: "connector-port-approximated", Message: "Office connection site was matched to the nearest supported Keynope shape port."})
		}
	}
	return restored, warnings
}

func removeRestoredLineWarnings(warnings []pptx.Warning, restored map[string]bool, slide int) []pptx.Warning {
	result := warnings[:0]
	for _, w := range warnings {
		if w.Slide == slide && restored[w.Object] && (strings.HasPrefix(w.Code, "line-route-") || w.Code == "line-arrowheads-omitted") {
			continue
		}
		result = append(result, w)
	}
	return result
}
