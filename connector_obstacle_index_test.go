package main

import (
	"math/rand"
	"reflect"
	"testing"
)

func obstacleIndexFixture() []projectedConnectorObstacle {
	var result []projectedConnectorObstacle
	for i := 0; i < 400; i++ {
		result = append(result, projectConnectorObstacle(connectorObstacle{X: float64(i%20)*13 - 5, Y: float64(i/20)*3 - 2, W: 12 + i%13, H: 4 + i%7, Shape: []string{"square", "circle", "triangle", "diamond"}[i%4], Rotation: float64(i%7) * 37.5}, 245, 56))
	}
	return result
}

func TestConnectorObstacleIndexMatchesSilhouettes(t *testing.T) {
	obstacles := obstacleIndexFixture()
	before := append([]projectedConnectorObstacle(nil), obstacles...)
	index := indexConnectorObstacles(obstacles)
	check := func(x, y float64) {
		t.Helper()
		want := false
		for i := range obstacles {
			if obstacles[i].contains(x, y) {
				want = true
				break
			}
		}
		if index.contains(x, y) != want {
			t.Fatalf("coverage differs at %g,%g", x, y)
		}
	}
	random := rand.New(rand.NewSource(2026))
	for i := 0; i < 10000; i++ {
		check(random.Float64()*280-15, random.Float64()*85-15)
	}
	for _, p := range obstacles {
		check(p.left, p.top)
		check(p.right, p.bottom)
		check(p.cx, p.cy)
	}
	if !reflect.DeepEqual(obstacles, before) {
		t.Fatal("index reordered or changed source obstacles")
	}
	if indexConnectorObstacles(nil).contains(0, 0) {
		t.Fatal("empty index is occupied")
	}
}

func BenchmarkConnectorObstacleQueries(b *testing.B) {
	obstacles := obstacleIndexFixture()
	index := indexConnectorObstacles(obstacles)
	for _, indexed := range []bool{false, true} {
		name := "scan"
		if indexed {
			name = "index"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			hits := 0
			for i := 0; i < b.N; i++ {
				x, y := float64(i%280)-15, float64(i%85)-15
				inside := false
				if indexed {
					inside = index.contains(x, y)
				} else {
					for j := range obstacles {
						if obstacles[j].contains(x, y) {
							inside = true
							break
						}
					}
				}
				if inside {
					hits++
				}
			}
			b.ReportMetric(float64(hits)/float64(b.N), "hit-ratio")
		})
	}
}
