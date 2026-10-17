package app

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

type connectorSelectionControl struct {
	key                string
	min, max, fallback float64
	choices            string
}

var connectorSelectionControls = map[string]connectorSelectionControl{
	"line.width":      {key: "connector-width", min: 1, max: 8, fallback: 1},
	"line.arrowWidth": {key: "connector-arrow-width", min: .5, max: 20, fallback: 6},
	"line.arrows":     {key: "connector-arrows", choices: "none start end both"},
	"line.path":       {key: "connector-mode", choices: "straight elbow"},
	"line.routing":    {key: "connector-route", choices: "automatic"},
}

func connectorSelectionValue(authored Element, q, rq url.Values, control connectorSelectionControl) (interface{}, bool, bool) {
	if authored.Kind != "connector" {
		return nil, false, false
	}
	explicit := q.Has(control.key)
	for _, clear := range strings.Split(q.Get("master-clear"), ",") {
		explicit = explicit || strings.TrimSpace(clear) == control.key
	}
	if control.key == "connector-route" {
		if rq.Get("connector-mode") == "elbow" && len(decodeConnectorRoute(rq.Get(control.key))) >= 4 {
			return "manual", explicit, true
		}
		return "automatic", explicit || q.Has("connector-mode"), true
	}
	if control.choices != "" {
		for _, value := range strings.Fields(control.choices) {
			if value == rq.Get(control.key) {
				return value, explicit, true
			}
		}
		return strings.Fields(control.choices)[0], explicit, true
	}
	return connectorNumber(rq, control.key, control.fallback, control.max), explicit, true
}

func applyConnectorSelection(slide *Slide, ids []string, control connectorSelectionControl, raw json.RawMessage) (bool, error) {
	if strings.TrimSpace(string(raw)) == "null" {
		return false, fmt.Errorf("connector %s requires a value", control.key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		valid := value == ""
		for _, choice := range strings.Fields(control.choices) {
			valid = valid || value == choice
		}
		if !valid {
			return false, fmt.Errorf("unsupported connector %s value", control.key)
		}
	} else {
		var number float64
		if control.choices != "" || json.Unmarshal(raw, &number) != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < control.min || number > control.max {
			return false, fmt.Errorf("connector %s must be between %g and %g", control.key, control.min, control.max)
		}
		value = strconv.FormatFloat(number, 'f', -1, 64)
	}
	selected := map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	changed := false
	for i, element := range slide.Elements {
		if !selected[element.ID] || element.Kind != "connector" {
			continue
		}
		q := mustQuery(element.Query)
		before := q.Encode()
		if control.key == "connector-route" {
			q.Del(control.key)
			q.Del("connector-mode")
			if element.MasterSlotID != "" {
				setMasterQueryClears(q, []string{control.key, "connector-mode"}, nil)
				if value != "" {
					setMasterQueryClears(q, nil, []string{control.key})
				}
			}
			if value == "automatic" {
				q.Set("connector-mode", "elbow")
				if element.MasterSlotID != "" {
					setMasterQueryClears(q, []string{"connector-mode"}, nil)
				}
			}
			if next := q.Encode(); next != before {
				slide.Elements[i].Query = next
				changed = true
			}
			continue
		}
		pathChanged := control.key == "connector-mode" && q.Get(control.key) != value
		if value == "" {
			q.Del(control.key)
		} else {
			q.Set(control.key, value)
		}
		if element.MasterSlotID != "" {
			setMasterQueryClears(q, []string{control.key}, nil)
		}
		if pathChanged {
			q.Del("connector-route")
			if element.MasterSlotID != "" {
				setMasterQueryClears(q, []string{"connector-route"}, nil)
				if value != "" {
					setMasterQueryClears(q, nil, []string{"connector-route"})
				}
			}
		}
		if next := q.Encode(); next != before {
			slide.Elements[i].Query = next
			changed = true
		}
	}
	return changed, nil
}
