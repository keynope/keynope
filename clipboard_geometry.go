package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
)

// Selected physical geometry only: no source slide, notes or activity archive.
// Optional on version-1 bundles so legacy/same-canvas paste remains compatible.
type objectClipboardGeometry struct {
	Version int                         `json:"version"`
	Width   int                         `json:"width"`
	Height  int                         `json:"height"`
	Cols    int                         `json:"cols"`
	Rows    int                         `json:"rows"`
	Bounds  map[string]sceneRect        `json:"bounds"`
	Routes  map[string][]connectorPoint `json:"routes,omitempty"`
}

func (g *objectClipboardGeometry) UnmarshalJSON(data []byte) error {
	fields, err := decodeMetadataJSONObject(data, "version", "width", "height", "cols", "rows", "bounds", "routes")
	if err != nil {
		return err
	}
	for _, name := range []string{"version", "width", "height", "cols", "rows", "bounds"} {
		if fields[name] == nil {
			return fmt.Errorf("clipboard geometry needs %s", name)
		}
	}
	for _, name := range []string{"bounds", "routes"} {
		if fields[name] == nil {
			continue
		}
		entries, err := decodeMetadataJSONObject(fields[name])
		if err != nil {
			return err
		}
		if len(entries) > maxObjectClipboardElements {
			return fmt.Errorf("too many clipboard geometry entries")
		}
		if name == "bounds" {
			for _, raw := range entries {
				values, err := decodeMetadataJSONObject(raw, "x", "y", "width", "height")
				if err != nil {
					return err
				}
				for _, key := range []string{"x", "y", "width", "height"} {
					if values[key] == nil {
						return fmt.Errorf("incomplete clipboard frame")
					}
				}
			}
		} else {
			for _, raw := range entries {
				decoder := json.NewDecoder(bytes.NewReader(raw))
				if token, err := decoder.Token(); err != nil || token != json.Delim('[') {
					return fmt.Errorf("clipboard route must be an array")
				}
				count := 0
				for decoder.More() {
					count++
					if count > 128 {
						return fmt.Errorf("too many clipboard route points")
					}
					var point json.RawMessage
					if err := decoder.Decode(&point); err != nil {
						return err
					}
					values, err := decodeMetadataJSONObject(point, "x", "y")
					if err != nil {
						return err
					}
					if values["x"] == nil || values["y"] == nil {
						return fmt.Errorf("incomplete clipboard route point")
					}
				}
			}
		}
	}
	type stored objectClipboardGeometry
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*g = objectClipboardGeometry(value)
	return nil
}

func (g *objectClipboardGeometry) validate(elements []Element) error {
	if g == nil {
		return nil
	}
	if g.Version != 1 {
		return fmt.Errorf("unsupported clipboard geometry")
	}
	if err := (&DeckPageSize{1, g.Width, g.Height}).Validate(); err != nil {
		return err
	}
	if err := validateAuthoredCanvas(g.Cols, g.Rows); err != nil {
		return err
	}
	owners := map[string]string{}
	for _, e := range elements {
		owners[e.ID] = e.Kind
		if e.Kind != "connector" {
			if _, ok := g.Bounds[e.ID]; !ok {
				return fmt.Errorf("missing clipboard frame for %s", e.ID)
			}
		}
	}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= 1e9 }
	for id, b := range g.Bounds {
		if owners[id] == "" || owners[id] == "connector" || !finite(b.X) || !finite(b.Y) || !finite(b.Width) || !finite(b.Height) || b.Width <= 0 || b.Height <= 0 {
			return fmt.Errorf("invalid clipboard frame for %s", id)
		}
	}
	for id, route := range g.Routes {
		if owners[id] != "connector" || len(route) < 2 || len(route) == 3 || len(route) > 128 {
			return fmt.Errorf("invalid clipboard route")
		}
		for i, p := range route {
			if !finite(p.X) || !finite(p.Y) || i > 0 && len(route) > 2 && p.X != route[i-1].X && p.Y != route[i-1].Y {
				return fmt.Errorf("invalid clipboard route points")
			}
		}
	}
	return nil
}

func captureClipboardGeometry(ctx context.Context, bundle *ObjectClipboard, deck Deck, index int, master bool, cols, rows int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	targets := map[string]bool{}
	for _, e := range bundle.Elements {
		targets[e.ID] = true
	}
	var resolved Slide
	if master {
		resolved = deck.applyShapeStrokeDefaults(masterViewPreview(deck.Masters, index))
	} else {
		resolved = deck.ResolveSlide(index, true)
	}
	scene, err := buildStyledSceneTargetsMeasured(deck, resolved, index, cols, rows, true, targets, nil)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	g := &objectClipboardGeometry{Version: 1, Width: int(scene.Width), Height: int(scene.Height), Cols: cols, Rows: rows, Bounds: map[string]sceneRect{}, Routes: map[string][]connectorPoint{}}
	for _, o := range scene.Objects {
		if o.Kind == "connector" {
			if len(o.Points) >= 2 {
				g.Routes[o.ID] = append([]connectorPoint(nil), o.Points...)
			}
		} else {
			g.Bounds[o.ID] = o.Bounds
		}
	}
	// Hidden objects remain copyable. Resolve only each missing hidden target
	// in an owned temporary view; do not shift already captured visible frames.
	for _, e := range bundle.Elements {
		if e.Kind == "connector" {
			continue
		}
		if _, ok := g.Bounds[e.ID]; ok {
			continue
		}
		hidden := cloneSlide(resolved)
		found := false
		for i := range hidden.Elements {
			if hidden.Elements[i].ID == e.ID && mustQuery(hidden.Elements[i].Query).Get("object-hidden") == "1" {
				hidden.Elements[i].Query = removeImageQueryKeys(hidden.Elements[i].Query, "object-hidden")
				found = true
			}
		}
		if found {
			projected, err := buildStyledSceneTargetsMeasured(deck, hidden, index, cols, rows, true, map[string]bool{e.ID: true}, nil)
			if err != nil {
				return err
			}
			for _, o := range projected.Objects {
				if o.ID == e.ID {
					g.Bounds[e.ID] = o.Bounds
				}
			}
		}
	}
	if err := g.validate(bundle.Elements); err != nil {
		return err
	}
	bundle.Geometry = g
	return nil
}

func applyClipboardGeometry(deck Deck, elements []Element, g *objectClipboardGeometry, grid ...[2]int) error {
	if g == nil {
		return nil
	}
	canvas := deck.physicalCanvas()
	cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	if len(grid) > 0 {
		cols, rows = grid[0][0], grid[0][1]
	}
	if canvas.Width == float64(g.Width) && canvas.Height == float64(g.Height) && cols == g.Cols && rows == g.Rows {
		return nil
	}
	sx, sy := float64(cols)/canvas.Width, float64(rows)/canvas.Height
	for i, e := range elements {
		q, err := url.ParseQuery(e.Query)
		if err != nil {
			return err
		}
		if e.Kind == "connector" {
			route := g.Routes[e.ID]
			if len(route) == 0 {
				for _, p := range decodeConnectorRoute(q.Get("connector-route")) {
					route = append(route, connectorPoint{p.X * float64(g.Width) / float64(g.Cols), p.Y * float64(g.Height) / float64(g.Rows)})
				}
			}
			if len(route) >= 4 {
				points := make([]connectorPoint, len(route))
				for j, p := range route {
					points[j] = connectorPoint{p.X * sx, p.Y * sy}
				}
				encoded, err := json.Marshal(points)
				if err != nil || decodeConnectorRoute(string(encoded)) == nil {
					return fmt.Errorf("pasted connector exceeds destination coordinate limits")
				}
				q.Set("connector-route", string(encoded))
			}
		} else {
			b := g.Bounds[e.ID]
			x, y := b.X*sx, b.Y*sy
			if !boundedPlacementCoordinate("left", x) || !boundedPlacementCoordinate("top", y) {
				return fmt.Errorf("pasted frame exceeds destination coordinate limits")
			}
			if q.Get("align") == "justify" && !q.Has("text-align") {
				q.Set("text-align", "justify")
			}
			for _, key := range []string{"left_pct", "right_pct", "right", "bottom", "align", "valign", "row_delta", "shape-offset-x", "shape-offset-y", "object-offset-x", "object-offset-y"} {
				q.Del(key)
			}
			q.Set("left", strconv.FormatFloat(math.Floor(x), 'f', 0, 64))
			q.Set("top", strconv.FormatFloat(math.Floor(y), 'f', 0, 64))
			if v := x - math.Floor(x); v != 0 {
				q.Set("object-offset-x", strconv.FormatFloat(v, 'f', -1, 64))
			}
			if v := y - math.Floor(y); v != 0 {
				q.Set("object-offset-y", strconv.FormatFloat(v, 'f', -1, 64))
			}
			text := e.Kind == "text" || e.Kind == "heading" || e.Kind == "code" || e.Kind == "bullet" || e.Kind == "text-image" || e.Kind == "page-number"
			for _, axis := range []struct {
				key   string
				value float64
			}{{"width", b.Width * sx}, {"height", b.Height * sy}} {
				if text && !q.Has(axis.key) {
					continue
				}
				minimum := 1.
				if e.Kind == "shape" {
					minimum = .5
				}
				if axis.value < minimum-1e-9 || axis.value > 1000+1e-9 {
					return fmt.Errorf("pasted %s cannot retain its physical %s within destination limits", e.Kind, axis.key)
				}
				q.Set(axis.key, strconv.FormatFloat(axis.value, 'f', -1, 64))
			}
			if e.Kind == "image" && imageFit(q.Get("image-fit")) == "" {
				q.Set("image-fit", "stretch")
			}
		}
		elements[i].Query = q.Encode()
	}
	return nil
}

func clipboardGeometryWarnings(deck Deck, bundle ObjectClipboard) []string {
	g := bundle.Geometry
	if g == nil {
		return nil
	}
	canvas := deck.physicalCanvas()
	cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	if canvas.Width == float64(g.Width) && canvas.Height == float64(g.Height) && cols == g.Cols && rows == g.Rows {
		return nil
	}
	for _, e := range bundle.Elements {
		q := mustQuery(e.Query)
		labelShadow := false
		if label, ok := shapeLabel(e); ok {
			labelShadow = mustQuery(label.Query).Get("shadow") != ""
		}
		if e.Kind == "connector" || q.Get("shadow") != "" || q.Get("label-shadow") != "" || labelShadow || e.Kind == "image" && (q.Get("image-style") == "retro" || q.Get("element-style") == "retro" || q.Get("glyph") != "") {
			return []string{"Object frames retain their physical size; grid-based shadows, connector thickness and Retro image detail follow the destination canvas."}
		}
	}
	return nil
}
