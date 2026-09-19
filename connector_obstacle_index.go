package main

import (
	"math"
	"sort"
)

// A balanced bounding-volume tree rejects distant silhouettes before testing
// their exact sampled coverage. It borrows immutable projected obstacles for a
// single routing search, with no slide-sized bitmap or resolution quantisation.
type connectorObstacleIndex struct {
	left, top, right, bottom float64
	a, b                     *connectorObstacleIndex
	items                    []*projectedConnectorObstacle
}

func indexConnectorObstacles(obstacles []projectedConnectorObstacle) *connectorObstacleIndex {
	items := make([]*projectedConnectorObstacle, len(obstacles))
	for i := range obstacles {
		items[i] = &obstacles[i]
	}
	return buildConnectorObstacleIndex(items)
}

func buildConnectorObstacleIndex(items []*projectedConnectorObstacle) *connectorObstacleIndex {
	if len(items) == 0 {
		return nil
	}
	n := &connectorObstacleIndex{left: math.Inf(1), top: math.Inf(1), right: math.Inf(-1), bottom: math.Inf(-1)}
	for _, p := range items {
		n.left = math.Min(n.left, p.left)
		n.top = math.Min(n.top, p.top)
		n.right = math.Max(n.right, p.right)
		n.bottom = math.Max(n.bottom, p.bottom)
	}
	if len(items) <= 8 {
		n.items = items
		return n
	}
	xAxis := n.right-n.left >= n.bottom-n.top
	sort.SliceStable(items, func(i, j int) bool {
		if xAxis {
			return items[i].left+items[i].right < items[j].left+items[j].right
		}
		return items[i].top+items[i].bottom < items[j].top+items[j].bottom
	})
	mid := len(items) / 2
	n.a, n.b = buildConnectorObstacleIndex(items[:mid]), buildConnectorObstacleIndex(items[mid:])
	return n
}

func (n *connectorObstacleIndex) contains(x, y float64) bool {
	if n == nil || x < n.left || x > n.right || y < n.top || y > n.bottom {
		return false
	}
	for _, p := range n.items {
		if p.contains(x, y) {
			return true
		}
	}
	return n.a.contains(x, y) || n.b.contains(x, y)
}
