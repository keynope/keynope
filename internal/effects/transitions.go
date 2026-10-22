package effects

import "math"

// DipSample ports FilmCraft render/transitions/legacy.rs dip_to_black/white:
// fade A over an opaque solid, switch at halfway, fade B up from that solid.
// Gains are interpolated by the renderer; colour arithmetic stays linear RGB.
type DipSample struct {
	Offset float64 `json:"offset"`
	Gain   float64 `json:"gain"`
	Bias   float64 `json:"bias"`
}

func DipCurve(white bool) []DipSample {
	result := make([]DipSample, 61)
	for i := range result {
		p := float64(i) / 60
		gain := math.Abs(1 - 2*p)
		bias := 0.0
		if white {
			bias = 1 - gain
		}
		result[i] = DipSample{p, gain, bias}
	}
	return result
}
