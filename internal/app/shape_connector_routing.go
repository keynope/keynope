package app

import (
	"container/heap"
	"encoding/json"
	"math"
	"net/url"
	"sort"
	"strconv"
)

type connectorObstacle struct {
	X, Y                      float64
	W, H                      int
	Shape                     string
	ID                        string
	Rotation                  float64
	Width, Height             float64 // Continuous cells; zero retains legacy raster dimensions.
	Spec                      *ShapeSpec
	StrokeWidth               float64 // Resolved physical scene pixels, including inherited paint.
	CanvasWidth, CanvasHeight float64 // Derived, not authored.
}

func connectorObstacles(slide Slide, lines []Line, width, height int) []connectorObstacle {
	slide.Elements = append([]Element(nil), slide.Elements...)
	fitSlideShapeLabels(&slide, width, height)
	seen := map[int]bool{}
	var result []connectorObstacle
	for _, l := range lines {
		if l.Role != "shape" || l.Element < 0 || l.Element >= len(slide.Elements) || seen[l.Element] {
			continue
		}
		seen[l.Element] = true
		q, _ := url.ParseQuery(slide.Elements[l.Element].Query)
		x, y := preciseShapeAnchor(q, float64(l.Col), float64(l.Row), width, height)
		angle, _ := strconv.ParseFloat(q.Get("object-rotation"), 64)
		if math.IsNaN(angle) || math.IsInf(angle, 0) {
			angle = 0
		}
		spec := shapeSpecFromValues(q)
		result = append(result, connectorObstacle{X: x + shapeSubcellOffset(q, "shape-offset-x") + objectPlacementOffset(q, "x"), Y: y + shapeSubcellOffset(q, "shape-offset-y") + objectPlacementOffset(q, "y"), W: shapeHalfCells(q, "width", 12), H: shapeHalfCells(q, "height", 6), Width: shapeDimension(q, "width", 12), Height: shapeDimension(q, "height", 6), Shape: shapeName(slide.Elements[l.Element]), ID: slide.Elements[l.Element].ID, Rotation: math.Mod(angle, 360), Spec: &spec, StrokeWidth: resolvedShapeStrokeWidth(slide, slide.Elements[l.Element], q, width, height)})
	}
	canvas := slide.physicalCanvas()
	for i := range result {
		result[i].CanvasWidth, result[i].CanvasHeight = canvas.Width, canvas.Height
	}
	return result
}

type projectedConnectorObstacle struct {
	object                   connectorObstacle
	cx, cy, sx, sy, c, s     float64
	left, top, right, bottom float64
	geometry                 sceneShapeGeometry
	strokeSegments           []shapeBoundarySegment
	sweepSegments            []shapeBoundarySegment // Lazy world-pixel contour for swept line coverage.
}

func projectConnectorObstacle(o connectorObstacle, width, height int) projectedConnectorObstacle {
	if o.Width <= 0 {
		o.Width = float64(o.W) / 2
	}
	if o.Height <= 0 {
		o.Height = float64(o.H) / 2
	}
	p := projectedConnectorObstacle{object: o, cx: o.X + o.Width/2, cy: o.Y + o.Height/2, sx: 1920 / float64(max(1, width)), sy: 1080 / float64(max(1, height))}
	if o.CanvasWidth > 0 && o.CanvasHeight > 0 {
		p.sx, p.sy = o.CanvasWidth/float64(max(1, width)), o.CanvasHeight/float64(max(1, height))
	}
	spec := ShapeSpec{Kind: o.Shape}
	if o.Spec != nil {
		spec = *o.Spec
	}
	p.geometry = shapeGeometry(spec, o.Width*p.sx/(o.Height*p.sy))
	if o.StrokeWidth > 0 {
		p.strokeSegments = shapeBoundarySegments(p.geometry, o.Width*p.sx, o.Height*p.sy)
	}
	p.c, p.s = math.Cos(o.Rotation*math.Pi/180), math.Sin(o.Rotation*math.Pi/180)
	hw, hh := o.Width/2*p.sx, o.Height/2*p.sy
	rx, ry := (math.Abs(p.c)*hw+math.Abs(p.s)*hh+o.StrokeWidth/2)/p.sx, (math.Abs(p.s)*hw+math.Abs(p.c)*hh+o.StrokeWidth/2)/p.sy
	p.left, p.right, p.top, p.bottom = p.cx-rx, p.cx+rx, p.cy-ry, p.cy+ry
	return p
}

func (p *projectedConnectorObstacle) contains(x, y float64) bool {
	if x < p.left || x > p.right || y < p.top || y > p.bottom {
		return false
	}
	dx, dy := (x-p.cx)*p.sx, (y-p.cy)*p.sy
	x, y = p.cx+(dx*p.c+dy*p.s)/p.sx, p.cy+(-dx*p.s+dy*p.c)/p.sy
	o := &p.object
	if o.Width <= 0 || o.Height <= 0 {
		return false
	}
	if p.geometry.contains((x-o.X)/o.Width, (y-o.Y)/o.Height) {
		return true
	}
	// The round-joined outline is a union of physical segment capsules. Do
	// not enlarge the whole rectangular frame: concave silhouettes keep their
	// usable negative space, and rotated outlines retain the same thickness.
	point := connectorPoint{(x - o.X) * p.sx, (y - o.Y) * p.sy}
	for _, segment := range p.strokeSegments {
		radius := o.StrokeWidth / 2
		if point.X < math.Min(segment.a.X, segment.b.X)-radius || point.X > math.Max(segment.a.X, segment.b.X)+radius || point.Y < math.Min(segment.a.Y, segment.b.Y)-radius || point.Y > math.Max(segment.a.Y, segment.b.Y)+radius {
			continue
		}
		if shapeSegmentDistanceSquared(point, segment) <= o.StrokeWidth*o.StrokeWidth/4 {
			return true
		}
	}
	return false
}
func connectorNumber(q url.Values, key string, fallback, limit float64) float64 {
	v, err := strconv.ParseFloat(q.Get(key), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return fallback
	}
	minimum := .5
	if key == "connector-width" {
		minimum = 1
	}
	return math.Max(minimum, math.Min(limit, v))
}

// A weighted rectilinear shortest-path search. Actual silhouette pixels, not
// bounding rectangles, incur crossing cost. Crossings remain possible when
// blocked in; clear routes always win over ordinary distance/bend costs.
type routeNode struct {
	state    int
	cost     float64
	priority float64
}
type routeHeap []routeNode

func (h routeHeap) Len() int           { return len(h) }
func (h routeHeap) Less(i, j int) bool { return h[i].priority < h[j].priority }
func (h routeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *routeHeap) Push(v any)        { *h = append(*h, v.(routeNode)) }
func (h *routeHeap) Pop() any          { old := *h; v := old[len(old)-1]; *h = old[:len(old)-1]; return v }

// A routing pass borrows immutable obstacles. Compute their fingerprint lazily,
// once per pass, rather than serializing the whole slide for every connector.
type connectorRoutingPass struct {
	canvas    sceneRect
	obstacles []connectorObstacle
	digest    [32]byte
	ready     bool
}

func (pass *connectorRoutingPass) key(a, b shapePort, width, height int, lineWidth float64, footprint ...any) [32]byte {
	if !pass.ready {
		data, _ := json.Marshal(pass.obstacles)
		pass.digest = connectorRouteKey(data)
		pass.ready = true
	}
	data, _ := json.Marshal([]any{a, b, pass.digest, width, height, lineWidth, footprint, pass.canvas})
	return connectorRouteKey(data)
}

func routedConnectorPath(e Element, a, b shapePort, obstacles []connectorObstacle, width, height int, canvas ...sceneRect) []connectorPoint {
	pass := connectorRoutingPass{obstacles: obstacles}
	if len(canvas) > 0 {
		pass.canvas = canvas[0]
	}
	return pass.path(e, a, b, width, height)
}

func (pass *connectorRoutingPass) path(e Element, a, b shapePort, width, height int) []connectorPoint {
	cw, ch := pass.canvas.Width, pass.canvas.Height
	if cw <= 0 || ch <= 0 {
		cw, ch = 1920, 1080
	}
	cellRatio := (cw / float64(max(1, width))) / (ch / float64(max(1, height)))
	obstacles := pass.obstacles
	if a.Direction != "" {
		a.Side = a.Direction
	}
	if b.Direction != "" {
		b.Side = b.Direction
	}
	q, _ := url.ParseQuery(e.Query)
	fallback := connectorPath(e, a, b)
	if q.Get("connector-mode") != "elbow" || len(decodeConnectorRoute(q.Get("connector-route"))) > 0 {
		return fallback
	}
	lineWidth := connectorNumber(q, "connector-width", 1, 8)
	arrows := q.Get("connector-arrows")
	arrowWidth := 0.0
	if arrows == "start" || arrows == "end" || arrows == "both" {
		arrowWidth = connectorNumber(q, "connector-arrow-width", 6, 20)
	}
	key := pass.key(a, b, width, height, lineWidth, arrows, arrowWidth)
	cached := connectorRouteCache.get(key)
	if cached != nil {
		return cached
	}
	stub := func(p shapePort, head bool) connectorPoint {
		v := connectorPoint{p.X, p.Y}
		length := math.Max(4, lineWidth/2+.5)
		if head {
			// Keep the complete head on a terminal leg, not across the first
			// elbow. Retro's head is longer than the native marker; reserve both.
			length = math.Max(length, math.Max(3, arrowWidth*1.5))
		}
		switch p.Side {
		case "left":
			v.X -= length
		case "right":
			v.X += length
		case "top":
			vertical := length / 2
			if head {
				vertical = math.Max(vertical, arrowWidth*cellRatio)
			}
			v.Y -= vertical
		case "bottom":
			vertical := length / 2
			if head {
				vertical = math.Max(vertical, arrowWidth*cellRatio)
			}
			v.Y += vertical
		}
		v.X = math.Max(0, math.Min(float64(width), v.X))
		v.Y = math.Max(0, math.Min(float64(height), v.Y))
		return v
	}
	u, v := stub(a, arrows == "start" || arrows == "both"), stub(b, arrows == "end" || arrows == "both")
	coords := func(size int, step float64, values []float64) []float64 {
		for x := 0.0; x <= float64(size); x += step {
			values = append(values, x)
		}
		values = append(values, float64(size))
		sort.Float64s(values)
		out := values[:0]
		for _, x := range values {
			if x < 0 || x > float64(size) {
				continue
			}
			if len(out) == 0 || x != out[len(out)-1] {
				out = append(out, x)
			}
		}
		return out
	}
	xs, ys := []float64{u.X, v.X}, []float64{u.Y, v.Y}
	projected := make([]projectedConnectorObstacle, 0, len(obstacles))
	for _, o := range obstacles {
		p := projectConnectorObstacle(o, width, height)
		projected = append(projected, p)
		xs = append(xs, p.left-lineWidth-1, p.right+lineWidth+1)
		ys = append(ys, p.top-lineWidth/2-.5, p.bottom+lineWidth/2+.5)
	}
	xs = coords(width, math.Max(2, math.Ceil(float64(width)/160)), xs)
	ys = coords(height, math.Max(1, math.Ceil(float64(height)/100)), ys)
	nx, ny := len(xs), len(ys)
	// Dense slides add two exact silhouette-clearance coordinates per object.
	// The previous 100k-cell cutoff was low enough for ordinary workshop
	// diagrams to fall back to a crossing elbow even when a clear route existed.
	// Keep a hard memory bound, but let the A* search below handle representative
	// dense scenes without exploring the complete Cartesian grid.
	if nx*ny > 400000 {
		return fallback
	}
	obstacleIndex := indexConnectorObstacles(projected)
	// The native stroke uses square scene pixels; sampled strokes use optical
	// half-height cells. Reserve the union rather than three disconnected rails,
	// which can miss a narrow silhouette entirely between the samples.
	rx := lineWidth / 2
	ry := math.Max(lineWidth/4, lineWidth*cellRatio/2)
	edgeCost := func(p, n connectorPoint) float64 {
		distance := math.Abs(p.X-n.X) + 2*math.Abs(p.Y-n.Y)
		steps := max(1, int(math.Ceil(distance*2)))
		hits := 0
		for i := 0; i < steps; i++ {
			t0, t1 := float64(i)/float64(steps), float64(i+1)/float64(steps)
			x0, y0 := p.X+(n.X-p.X)*t0, p.Y+(n.Y-p.Y)*t0
			x1, y1 := p.X+(n.X-p.X)*t1, p.Y+(n.Y-p.Y)*t1
			if obstacleIndex.intersectsRect(math.Min(x0, x1)-rx, math.Min(y0, y1)-ry, math.Max(x0, x1)+rx, math.Max(y0, y1)+ry) {
				hits++
			}
		}
		return distance + float64(hits)/float64(steps)*distance*1e6
	}
	// Cache each undirected edge's silhouette sampling during this search.
	edges := map[[2]int]float64{}
	start := (sort.SearchFloat64s(ys, u.Y)*nx + sort.SearchFloat64s(xs, u.X)) * 2
	goal := sort.SearchFloat64s(ys, v.Y)*nx + sort.SearchFloat64s(xs, v.X)
	dist := make([]float64, nx*ny*2)
	prev := make([]int, len(dist))
	for i := range dist {
		dist[i] = math.Inf(1)
		prev[i] = -1
	}
	dist[start], dist[start+1] = 0, 0
	heuristic := func(cell int) float64 {
		x, y := cell%nx, cell/nx
		return math.Abs(xs[x]-v.X) + 2*math.Abs(ys[y]-v.Y)
	}
	queue := &routeHeap{{state: start, priority: heuristic(start / 2)}, {state: start + 1, priority: heuristic(start / 2)}}
	heap.Init(queue)
	end := -1
	for queue.Len() > 0 {
		cur := heap.Pop(queue).(routeNode)
		if cur.cost != dist[cur.state] {
			continue
		}
		cell := cur.state / 2
		x, y := cell%nx, cell/nx
		if cell == goal {
			end = cur.state
			break
		}
		for _, d := range [][3]int{{-1, 0, 0}, {1, 0, 0}, {0, -1, 1}, {0, 1, 1}} {
			xx, yy := x+d[0], y+d[1]
			if xx < 0 || yy < 0 || xx >= nx || yy >= ny {
				continue
			}
			nextCell := yy*nx + xx
			state := nextCell*2 + d[2]
			k := [2]int{min(cell, nextCell), max(cell, nextCell)}
			cost, ok := edges[k]
			if !ok {
				cost = edgeCost(connectorPoint{xs[x], ys[y]}, connectorPoint{xs[xx], ys[yy]})
				edges[k] = cost
			}
			if cur.state%2 != d[2] {
				cost += 4
			}
			total := cur.cost + cost
			if total < dist[state] {
				dist[state] = total
				prev[state] = cur.state
				heap.Push(queue, routeNode{state: state, cost: total, priority: total + heuristic(nextCell)})
			}
		}
	}
	if end < 0 {
		return fallback
	}
	var route []connectorPoint
	for s := end; s >= 0; s = prev[s] {
		cell := s / 2
		route = append(route, connectorPoint{xs[cell%nx], ys[cell/nx]})
	}
	for i, j := 0, len(route)-1; i < j; i, j = i+1, j-1 {
		route[i], route[j] = route[j], route[i]
	}
	points := []connectorPoint{{a.X, a.Y}}
	points = append(points, route...)
	points = append(points, connectorPoint{b.X, b.Y})
	// Keep the terminal stubs for editable endpoint legs; simplify the interior.
	for i := 2; i < len(points)-2; {
		p, c, n := points[i-1], points[i], points[i+1]
		if p.X == c.X && c.X == n.X || p.Y == c.Y && c.Y == n.Y {
			points = append(points[:i], points[i+1:]...)
		} else {
			i++
		}
	}
	connectorRouteCache.put(key, points)
	return points
}
