package main

import (
	"container/heap"
	"encoding/json"
	"math"
	"net/url"
	"sort"
	"strconv"
)

type connectorObstacle struct {
	X, Y          float64
	W, H          int
	Shape         string
	ID            string
	Rotation      float64
	Width, Height float64 // Continuous cells; zero retains legacy raster dimensions.
}

func connectorObstacles(slide Slide, lines []Line, width, height int) []connectorObstacle {
	seen := map[int]bool{}
	var result []connectorObstacle
	for _, l := range lines {
		if l.Role != "shape" || l.Element < 0 || l.Element >= len(slide.Elements) || seen[l.Element] {
			continue
		}
		seen[l.Element] = true
		q, _ := url.ParseQuery(l.Query)
		x, y := preciseShapeAnchor(q, float64(l.Col), float64(l.Row), width, height)
		angle, _ := strconv.ParseFloat(q.Get("object-rotation"), 64)
		if math.IsNaN(angle) || math.IsInf(angle, 0) {
			angle = 0
		}
		result = append(result, connectorObstacle{X: x + shapeSubcellOffset(q, "shape-offset-x") + objectPlacementOffset(q, "x"), Y: y + shapeSubcellOffset(q, "shape-offset-y") + objectPlacementOffset(q, "y"), W: shapeHalfCells(q, "width", 12), H: shapeHalfCells(q, "height", 6), Width: shapeDimension(q, "width", 12), Height: shapeDimension(q, "height", 6), Shape: shapeName(slide.Elements[l.Element]), ID: slide.Elements[l.Element].ID, Rotation: math.Mod(angle, 360)})
	}
	return result
}

type projectedConnectorObstacle struct {
	object                   connectorObstacle
	cx, cy, sx, sy, c, s     float64
	left, top, right, bottom float64
}

func projectConnectorObstacle(o connectorObstacle, width, height int) projectedConnectorObstacle {
	if o.Width <= 0 {
		o.Width = float64(o.W) / 2
	}
	if o.Height <= 0 {
		o.Height = float64(o.H) / 2
	}
	p := projectedConnectorObstacle{object: o, cx: o.X + o.Width/2, cy: o.Y + o.Height/2, sx: 1920 / float64(max(1, width)), sy: 1080 / float64(max(1, height))}
	p.c, p.s = math.Cos(o.Rotation*math.Pi/180), math.Sin(o.Rotation*math.Pi/180)
	hw, hh := o.Width/2*p.sx, o.Height/2*p.sy
	rx, ry := (math.Abs(p.c)*hw+math.Abs(p.s)*hh)/p.sx, (math.Abs(p.s)*hw+math.Abs(p.c)*hh)/p.sy
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
	px, py := int(math.Floor((x-o.X)*float64(o.W)/o.Width)), int(math.Floor((y-o.Y)*float64(o.H)/o.Height))
	return px >= 0 && py >= 0 && px < o.W && py < o.H && shapeCellFilled(o.Shape, px, py, o.W, o.H)
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
	obstacles []connectorObstacle
	digest    [32]byte
	ready     bool
}

func (pass *connectorRoutingPass) key(a, b shapePort, width, height int, lineWidth float64) [32]byte {
	if !pass.ready {
		data, _ := json.Marshal(pass.obstacles)
		pass.digest = connectorRouteKey(data)
		pass.ready = true
	}
	data, _ := json.Marshal([]any{a, b, pass.digest, width, height, lineWidth})
	return connectorRouteKey(data)
}

func routedConnectorPath(e Element, a, b shapePort, obstacles []connectorObstacle, width, height int) []connectorPoint {
	pass := connectorRoutingPass{obstacles: obstacles}
	return pass.path(e, a, b, width, height)
}

func (pass *connectorRoutingPass) path(e Element, a, b shapePort, width, height int) []connectorPoint {
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
	key := pass.key(a, b, width, height, lineWidth)
	cached := connectorRouteCache.get(key)
	if cached != nil {
		return cached
	}
	stub := func(p shapePort) connectorPoint {
		v := connectorPoint{p.X, p.Y}
		switch p.Side {
		case "left":
			v.X -= 4
		case "right":
			v.X += 4
		case "top":
			v.Y -= 2
		case "bottom":
			v.Y += 2
		}
		v.X = math.Max(0, math.Min(float64(width), v.X))
		v.Y = math.Max(0, math.Min(float64(height), v.Y))
		return v
	}
	u, v := stub(a), stub(b)
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
	inside := indexConnectorObstacles(projected).contains
	edgeCost := func(p, n connectorPoint) float64 {
		distance := math.Abs(p.X-n.X) + 2*math.Abs(p.Y-n.Y)
		steps := max(1, int(math.Ceil(distance*2)))
		hits := 0
		for i := 0; i <= steps; i++ {
			t := float64(i) / float64(steps)
			x, y := p.X+(n.X-p.X)*t, p.Y+(n.Y-p.Y)*t
			blocked := false
			for _, offset := range []float64{-lineWidth / 2, 0, lineWidth / 2} {
				if p.Y == n.Y {
					blocked = blocked || inside(x, y+offset/2)
				} else {
					blocked = blocked || inside(x+offset, y)
				}
			}
			if blocked {
				hits++
			}
		}
		return distance + float64(hits)/float64(steps+1)*distance*1e6
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
