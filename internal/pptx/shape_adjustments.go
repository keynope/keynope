package pptx

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func validAdjustmentName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}
func validateShapeAdjustments(values map[string]int64) error {
	if len(values) > 32 {
		return fmt.Errorf("too many shape adjustments")
	}
	for name, value := range values {
		if !validAdjustmentName(name) || value < -2147483648 || value > 2147483647 {
			return fmt.Errorf("invalid shape adjustment %q", name)
		}
	}
	return nil
}
func parseShapeAdjustments(geometry *node) (map[string]int64, bool) {
	list := findChild(geometry, "avLst")
	if list == nil {
		return nil, false
	}
	values := map[string]int64{}
	loss := false
	seen := map[string]bool{}
	for index, guide := range list.Children {
		if index >= 32 {
			loss = true
			break
		}
		name := attr(guide, "name")
		fields := strings.Fields(attr(guide, "fmla"))
		if guide.Name != "gd" || !validAdjustmentName(name) || len(fields) != 2 || fields[0] != "val" || seen[name] {
			loss = true
			continue
		}
		seen[name] = true
		value, err := strconv.ParseInt(fields[1], 10, 32)
		if err == nil {
			values[name] = value
		} else {
			loss = true
		}
	}
	if len(values) == 0 {
		return nil, loss
	}
	return values, loss
}
func shapeAdjustmentXML(values map[string]int64) string {
	if len(values) == 0 {
		return "<a:avLst/>"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("<a:avLst>")
	for _, key := range keys {
		fmt.Fprintf(&b, `<a:gd name="%s" fmla="val %d"/>`, escapeAttr(key), values[key])
	}
	b.WriteString("</a:avLst>")
	return b.String()
}
