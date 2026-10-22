package app

import (
	"encoding/json"
	"keynope/internal/effects"
)

func transitionCurvesJS() string {
	data, err := json.Marshal(map[string][]effects.DipSample{"dip-black": effects.DipCurve(false), "dip-white": effects.DipCurve(true)})
	if err != nil {
		return ""
	}
	return "globalThis.KeynopeTransitionCurves=" + string(data) + ";\n"
}
