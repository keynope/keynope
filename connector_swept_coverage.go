package main

import "math"

// A swept stroke is a rectangle in world cells. Test the complete rectangle
// against the shared flattened silhouette and physical round-joined outline.
// Sampling separate centre/edge rails can step over thin or concave geometry.
func (n *connectorObstacleIndex) intersectsRect(left, top, right, bottom float64) bool {
	if n == nil || right < n.left || left > n.right || bottom < n.top || top > n.bottom {
		return false
	}
	for _, p := range n.items {
		if p.intersectsRect(left, top, right, bottom) {
			return true
		}
	}
	return n.a.intersectsRect(left, top, right, bottom) || n.b.intersectsRect(left, top, right, bottom)
}

func (p *projectedConnectorObstacle) intersectsRect(left, top, right, bottom float64) bool {
	if right < p.left || left > p.right || bottom < p.top || top > p.bottom {
		return false
	}
	if p.contains((left+right)/2, (top+bottom)/2) {
		return true
	}
	if p.sweepSegments == nil {
		local := p.strokeSegments
		if local == nil {
			local = shapeBoundarySegments(p.geometry, p.object.Width*p.sx, p.object.Height*p.sy)
		}
		p.sweepSegments = make([]shapeBoundarySegment, len(local))
		transform := func(v connectorPoint) connectorPoint {
			dx, dy := v.X-p.object.Width*p.sx/2, v.Y-p.object.Height*p.sy/2
			return connectorPoint{p.cx*p.sx + dx*p.c - dy*p.s, p.cy*p.sy + dx*p.s + dy*p.c}
		}
		for i, segment := range local {
			p.sweepSegments[i] = shapeBoundarySegment{transform(segment.a), transform(segment.b)}
		}
	}
	left, top, right, bottom = left*p.sx, top*p.sy, right*p.sx, bottom*p.sy
	radius := p.object.StrokeWidth / 2
	corners := [4]connectorPoint{{left, top}, {right, top}, {right, bottom}, {left, bottom}}
	for _, segment := range p.sweepSegments {
		if math.Max(segment.a.X, segment.b.X)+radius < left || math.Min(segment.a.X, segment.b.X)-radius > right || math.Max(segment.a.Y, segment.b.Y)+radius < top || math.Min(segment.a.Y, segment.b.Y)-radius > bottom {
			continue
		}
		if segmentIntersectsConnectorRect(segment, left, top, right, bottom) {
			return true
		}
		if radius <= 0 {
			continue
		}
		for _, point := range [2]connectorPoint{segment.a, segment.b} {
			dx, dy := point.X-clampFloat(point.X, left, right), point.Y-clampFloat(point.Y, top, bottom)
			if dx*dx+dy*dy <= radius*radius {
				return true
			}
		}
		for _, point := range corners {
			if shapeSegmentDistanceSquared(point, segment) <= radius*radius {
				return true
			}
		}
	}
	return false
}

func segmentIntersectsConnectorRect(s shapeBoundarySegment, left, top, right, bottom float64) bool {
	lo, hi := 0.0, 1.0
	for _, axis := range [2][4]float64{{s.a.X, s.b.X - s.a.X, left, right}, {s.a.Y, s.b.Y - s.a.Y, top, bottom}} {
		origin, delta := axis[0], axis[1]
		if delta == 0 {
			if origin < axis[2] || origin > axis[3] {
				return false
			}
			continue
		}
		a, b := (axis[2]-origin)/delta, (axis[3]-origin)/delta
		if a > b {
			a, b = b, a
		}
		lo, hi = math.Max(lo, a), math.Min(hi, b)
		if lo > hi {
			return false
		}
	}
	return true
}
