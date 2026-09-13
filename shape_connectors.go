package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/url"
)

type connectorPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type shapeConnector struct {
	ID     string           `json:"id"`
	Points []connectorPoint `json:"points"`
	Arrows string           `json:"arrows,omitempty"`
}

func decodeConnectorRoute(value string) []connectorPoint {
	if len(value) > 16000 {
		return nil
	}
	var points []connectorPoint
	if json.Unmarshal([]byte(value), &points) != nil || len(points) < 4 || len(points) > 128 {
		return nil
	}
	for i, p := range points {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) || math.Abs(p.X) > 10000 || math.Abs(p.Y) > 10000 {
			return nil
		}
		if i > 0 && p.X != points[i-1].X && p.Y != points[i-1].Y {
			return nil
		}
	}
	return points
}

func connectorPath(e Element, a, b shapePort) []connectorPoint {
	q, _ := url.ParseQuery(e.Query)
	start, end := connectorPoint{a.X, a.Y}, connectorPoint{b.X, b.Y}
	if q.Get("connector-mode") != "elbow" {
		return []connectorPoint{start, end}
	}
	horizontal := func(side string) bool { return side == "left" || side == "right" }
	if points := decodeConnectorRoute(q.Get("connector-route")); len(points) > 0 {
		// Only the two terminal legs follow their anchors. User-positioned inner
		// tracks stay put, and every segment remains axis aligned.
		points[0], points[len(points)-1] = start, end
		if horizontal(a.Side) {
			points[1].Y = a.Y
		} else {
			points[1].X = a.X
		}
		if horizontal(b.Side) {
			points[len(points)-2].Y = b.Y
		} else {
			points[len(points)-2].X = b.X
		}
		return points
	}
	stub := func(p shapePort) connectorPoint {
		v := connectorPoint{p.X, p.Y}
		switch p.Side {
		case "left":
			v.X -= 6
		case "right":
			v.X += 6
		case "top":
			v.Y -= 3
		case "bottom":
			v.Y += 3
		}
		return v
	}
	u, v := stub(a), stub(b)
	points := []connectorPoint{start, u}
	if horizontal(a.Side) && horizontal(b.Side) {
		x := (u.X + v.X) / 2
		if a.Side == b.Side {
			if a.Side == "left" {
				x = math.Min(u.X, v.X) - 6
			} else {
				x = math.Max(u.X, v.X) + 6
			}
		}
		points = append(points, connectorPoint{x, u.Y}, connectorPoint{x, v.Y})
	} else if !horizontal(a.Side) && !horizontal(b.Side) {
		y := (u.Y + v.Y) / 2
		if a.Side == b.Side {
			if a.Side == "top" {
				y = math.Min(u.Y, v.Y) - 3
			} else {
				y = math.Max(u.Y, v.Y) + 3
			}
		}
		points = append(points, connectorPoint{u.X, y}, connectorPoint{v.X, y})
	} else if horizontal(a.Side) {
		points = append(points, connectorPoint{v.X, u.Y})
	} else {
		points = append(points, connectorPoint{u.X, v.Y})
	}
	points = append(points, v, end)
	// Merge collinear tracks but keep at least four points, so moving an end
	// never moves the other end of a manually edited route.
	for i := 1; i < len(points)-1 && len(points) > 4; {
		p, c, n := points[i-1], points[i], points[i+1]
		if p.X == c.X && c.X == n.X || p.Y == c.Y && c.Y == n.Y {
			points = append(points[:i], points[i+1:]...)
		} else {
			i++
		}
	}
	return points
}

func slideShapeConnectors(slide Slide, lines []Line, width, height int) []shapeConnector {
	var result []shapeConnector
	ports := slideShapePorts(slide, lines, width, height)
	obstacles := connectorObstacles(slide, lines)
	for _, e := range slide.Elements {
		if e.Kind != "connector" {
			continue
		}
		a, b, ok := connectorEndpoints(e, ports)
		if !ok {
			continue
		}
		q, _ := url.ParseQuery(e.Query)
		result = append(result, shapeConnector{e.ID, routedConnectorPath(e, a, b, obstacles, width, height), q.Get("connector-arrows")})
	}
	return result
}

// Ports are measured in canvas cells, at the silhouette rather than its box.
type shapePort struct {
	ID   string  `json:"id"`
	Side string  `json:"side"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

func shapePorts(element Element, x, y float64) []shapePort {
	q, _ := url.ParseQuery(element.Query)
	w, h := shapeHalfCells(q, "width", 12), shapeHalfCells(q, "height", 6)
	x += shapeSubcellOffset(q, "shape-offset-x")
	y += shapeSubcellOffset(q, "shape-offset-y")
	cx, cy := (w-1)/2, (h-1)/2
	var result []shapePort
	for _, side := range []string{"top", "right", "bottom", "left"} {
		for step := 0; step < max(w, h); step++ {
			px, py := cx, cy
			switch side {
			case "top":
				py = step
			case "bottom":
				py = h - 1 - step
			case "left":
				px = step
			case "right":
				px = w - 1 - step
			}
			if px < 0 || py < 0 || px >= w || py >= h {
				break
			}
			if !shapeCellFilled(shapeName(element), px, py, w, h) {
				continue
			}
			port := shapePort{ID: element.ID, Side: side, X: x + float64(w)/4, Y: y + float64(h)/4}
			switch side {
			case "top":
				port.Y = y + float64(py)/2
			case "bottom":
				port.Y = y + float64(py+1)/2
			case "left":
				port.X = x + float64(px)/2
			case "right":
				port.X = x + float64(px+1)/2
			}
			result = append(result, port)
			break
		}
	}
	return result
}

func slideShapePorts(slide Slide, lines []Line, width, height int) []shapePort {
	slide = cloneSlide(slide)
	fitSlideShapeLabels(&slide, width, height)
	seen := map[int]bool{}
	var ports []shapePort
	for _, line := range lines {
		if line.Role != "shape" || seen[line.Element] || line.Element < 0 || line.Element >= len(slide.Elements) {
			continue
		}
		seen[line.Element] = true
		e := slide.Elements[line.Element]
		ports = append(ports, shapePorts(e, float64(line.Col), float64(line.Row))...)
	}
	return ports
}

func connectorEndpoints(e Element, ports []shapePort) (shapePort, shapePort, bool) {
	q, _ := url.ParseQuery(e.Query)
	var a, b shapePort
	for _, p := range ports {
		if p.ID == q.Get("connector-from") && p.Side == q.Get("connector-from-side") {
			a = p
		}
		if p.ID == q.Get("connector-to") && p.Side == q.Get("connector-to-side") {
			b = p
		}
	}
	return a, b, a.ID != "" && b.ID != "" && a.ID != b.ID
}

func validShapeConnector(e Element, slide Slide) bool {
	if e.Kind != "connector" {
		return false
	}
	var ports []shapePort
	for _, shape := range slide.Elements {
		if shape.Kind == "shape" {
			ports = append(ports, shapePorts(shape, 0, 0)...)
		}
	}
	_, _, ok := connectorEndpoints(e, ports)
	return ok
}

// Render through the ordinary block pipeline so export and participants see
// exactly the same line. Dangling references remain inert (and undoable).
func shapeConnectorLines(slide Slide, lines []Line, width, height int) []Line {
	hasConnector := false
	for _, e := range slide.Elements {
		if e.Kind == "connector" {
			hasConnector = true
			break
		}
	}
	if !hasConnector {
		return nil
	}
	ports := slideShapePorts(slide, lines, width, height)
	obstacles := connectorObstacles(slide, lines)
	var result []Line
	for index, e := range slide.Elements {
		if e.Kind != "connector" {
			continue
		}
		a, b, ok := connectorEndpoints(e, ports)
		if !ok {
			continue
		}
		points := routedConnectorPath(e, a, b, obstacles, width, height)
		result = append(result, connectorPathLines(e, index, points, connectorRasterBounds{a, b, obstacles})...)
	}
	return result
}

// Both the drag preview and saved connectors use this exact semi-block raster.
type connectorRasterBounds struct {
	From, To shapePort
	Shapes   []connectorObstacle
}

func connectorPathLines(e Element, index int, points []connectorPoint, bounds ...connectorRasterBounds) []Line {
	if len(points) < 2 {
		return nil
	}
	var result []Line
	q, _ := url.ParseQuery(e.Query)
	lineWidth := connectorNumber(q, "connector-width", 1, 8)
	arrowWidth := connectorNumber(q, "connector-arrow-width", 6, 20)
	minX, minY, maxX, maxY := points[0].X, points[0].Y, points[0].X, points[0].Y
	for _, p := range points {
		minX = math.Min(minX, p.X)
		minY = math.Min(minY, p.Y)
		maxX = math.Max(maxX, p.X)
		maxY = math.Max(maxY, p.Y)
	}
	padding := int(math.Ceil(math.Max(lineWidth, arrowWidth)*2)) + 2
	x0, y0 := int(math.Floor(minX))-padding, int(math.Floor(minY))-padding
	w, h := int(math.Ceil(maxX))-x0+padding+1, int(math.Ceil(maxY))-y0+padding+1
	if w < 1 || h < 1 || w > 4000 || h > 4000 {
		return nil
	}
	mask := make([][]bool, h*2)
	for y := range mask {
		mask[y] = make([]bool, w*2)
	}
	var caps []shapePort
	var endpointShapes []connectorObstacle
	if len(bounds) > 0 {
		for _, shape := range bounds[0].Shapes {
			if shape.ID != "" && (shape.ID == bounds[0].From.ID || shape.ID == bounds[0].To.ID) {
				endpointShapes = append(endpointShapes, shape)
			}
		}
	}
	paint := func(px, py float64) {
		x := int(math.Round((px - float64(x0)) * 2))
		y := int(math.Round((py - float64(y0)) * 2))
		// Samples identify filled half-cell areas, not dimensionless points.
		// A tip/cap touching the boundary must not fill the cell inside it.
		left, top := float64(x0)+float64(x)/2, float64(y0)+float64(y)/2
		for _, cap := range caps {
			switch cap.Side {
			case "left":
				if left+.5 > cap.X+1e-9 {
					return
				}
			case "right":
				if left < cap.X-1e-9 {
					return
				}
			case "top":
				if top+.5 > cap.Y+1e-9 {
					return
				}
			case "bottom":
				if top < cap.Y-1e-9 {
					return
				}
			}
		}
		// Wide tips beside curved/sloping edges must also respect the actual
		// source/target silhouette. Other shapes retain normal layer ordering.
		for _, shape := range endpointShapes {
			sx, sy := int(math.Floor((left+.25-shape.X)*2)), int(math.Floor((top+.25-shape.Y)*2))
			if sx >= 0 && sy >= 0 && sx < shape.W && sy < shape.H && shapeCellFilled(shape.Shape, sx, sy, shape.W, shape.H) {
				return
			}
		}
		if y >= 0 && y < len(mask) && x >= 0 && x < len(mask[y]) {
			mask[y][x] = true
		}
	}
	// Half-cells are twice as tall as wide. Quantize each brush dimension
	// once, rather than rounding several overlapping samples into extra rows.
	strokeCols, strokeRows := max(1, int(math.Ceil(lineWidth*2))), max(1, int(math.Ceil(lineWidth)))
	for i := 1; i < len(points); i++ {
		p, n := points[i-1], points[i]
		if p == n {
			continue
		}
		caps = nil
		if len(bounds) > 0 {
			if p == points[0] {
				caps = append(caps, bounds[0].From)
			}
			if n == points[len(points)-1] {
				caps = append(caps, bounds[0].To)
			}
		}
		steps := max(1, int(math.Ceil(math.Max(math.Abs(p.X-n.X), math.Abs(p.Y-n.Y))*4)))
		previous := p
		for step := 0; step <= steps; step++ {
			t := float64(step) / float64(steps)
			x, y := p.X+(n.X-p.X)*t, p.Y+(n.Y-p.Y)*t
			for col := 0; col < strokeCols; col++ {
				for row := 0; row < strokeRows; row++ {
					ox, oy := float64(col-strokeCols/2)/2, float64(row-strokeRows/2)/2
					paint(x+ox, y+oy)
					// Join diagonal half-cells along an edge, not just a corner.
					paint(x+ox, previous.Y+oy)
				}
			}
			previous = connectorPoint{x, y}
		}
	}
	arrow := func(tip, previous connectorPoint) {
		// Canvas cells are roughly twice as tall as wide. Work in square
		// optical units so vertical and horizontal heads have the same size.
		dx, dy := tip.X-previous.X, (tip.Y-previous.Y)*2
		length := math.Hypot(dx, dy)
		if length == 0 {
			return
		}
		dx /= length
		dy /= length
		arrowLength := math.Max(3, arrowWidth*1.5)
		for depth := 0.0; depth <= arrowLength; depth += .25 {
			half := depth / arrowLength * arrowWidth / 2
			for side := -half; side <= half; side += .25 {
				paint(tip.X-dx*depth-dy*side, tip.Y+(-dy*depth+dx*side)/2)
			}
		}
	}
	if q.Get("connector-arrows") == "end" || q.Get("connector-arrows") == "both" {
		caps = nil
		if len(bounds) > 0 {
			caps = []shapePort{bounds[0].To}
		}
		for i := len(points) - 2; i >= 0; i-- {
			if points[i] != points[len(points)-1] {
				arrow(points[len(points)-1], points[i])
				break
			}
		}
	}
	if q.Get("connector-arrows") == "start" || q.Get("connector-arrows") == "both" {
		caps = nil
		if len(bounds) > 0 {
			caps = []shapePort{bounds[0].From}
		}
		for i := 1; i < len(points); i++ {
			if points[i] != points[0] {
				arrow(points[0], points[i])
				break
			}
		}
	}
	for y, row := range maskToQuadrantsPadded(mask) {
		result = append(result, Line{Row: y0 + y, Col: x0, Text: row, Role: "connector", Element: index, Query: e.Query})
	}
	return result
}

func (s *nativeEditorSession) handleConnectorPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Element Element   `json:"element"`
		From    shapePort `json:"from"`
		To      shapePort `json:"to"`
		Cols    int       `json:"cols"`
		Rows    int       `json:"rows"`
		Page    int       `json:"page"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&request); err != nil || request.Cols < 1 || request.Cols > 1000 || request.Rows < 1 || request.Rows > 1000 {
		http.Error(w, "invalid connector preview", http.StatusBadRequest)
		return
	}
	for _, p := range []shapePort{request.From, request.To} {
		if math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) || p.X < 0 || p.X > float64(request.Cols) || p.Y < 0 || p.Y > float64(request.Rows) {
			http.Error(w, "invalid connector point", http.StatusBadRequest)
			return
		}
	}
	s.mu.RLock()
	deck := cloneDeck(s.deck)
	current, masterMode, currentMaster := s.current, s.masterMode, s.currentMaster
	s.mu.RUnlock()
	var slide Slide
	if masterMode {
		slide = masterViewPreview(deck.Masters, currentMaster)
	} else if current >= 0 && current < len(deck.Slides) {
		slide = deck.ResolvedSlides()[current]
	} else {
		http.Error(w, "missing slide", http.StatusBadRequest)
		return
	}
	lines := displayLines(slide, request.Cols, request.Rows, request.Page)
	obstacles := connectorObstacles(slide, lines)
	points := routedConnectorPath(request.Element, request.From, request.To, obstacles, request.Cols, request.Rows)
	raster := connectorPathLines(request.Element, -1, points, connectorRasterBounds{request.From, request.To, obstacles})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Lines  []exportLine     `json:"lines"`
		Points []connectorPoint `json:"points"`
	}{exportLines(raster, slide, request.Cols, request.Rows, len(deck.Slides)), points})
}

func freshPastedConnectorIDs(elements []Element) {
	ids := map[string]string{}
	for i := range elements {
		old := elements[i].ID
		elements[i].ID = newStableID(elements[i].Kind)
		if old != "" {
			ids[old] = elements[i].ID
		}
	}
	for i := range elements {
		if elements[i].Kind == "connector" {
			q, _ := url.ParseQuery(elements[i].Query)
			for _, key := range []string{"connector-from", "connector-to"} {
				if id := ids[q.Get(key)]; id != "" {
					q.Set(key, id)
				}
			}
			elements[i].Query = q.Encode()
		}
	}
}
