package main

import "math"

func scaleTextMask(mask [][]bool, factor float64) [][]bool {
	if len(mask) == 0 || len(mask[0]) == 0 || factor <= 0 {
		return nil
	}
	srcH, srcW := len(mask), len(mask[0])
	dstH := max(1, int(math.Round(float64(srcH)*factor)))
	dstW := max(1, int(math.Round(float64(srcW)*factor)))
	scaled := make([][]bool, dstH)
	for y := 0; y < dstH; y++ {
		sy := min(srcH-1, int(float64(y)/factor))
		scaled[y] = make([]bool, dstW)
		for x := 0; x < dstW; x++ {
			sx := min(srcW-1, int(float64(x)/factor))
			scaled[y][x] = mask[sy][sx]
		}
	}
	return scaled
}
