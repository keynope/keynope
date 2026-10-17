package app

import (
	"fmt"
	"reflect"
	"testing"
)

func TestShapeLabelFittingIsStableAcrossProjectionPasses(t *testing.T) {
	for _, canvas := range [][2]int{{245, 56}, {160, 90}} {
		for _, font := range []string{"c64", "sans", "mono"} {
			for _, definition := range shapeDefinitions {
				data, err := encodeShapeLabel(shapeLabelData{Text: "Wide label\nSecond line", Query: "render=truetype&modern-font=" + font + "&modern-size=64"})
				if err != nil {
					t.Fatal(err)
				}
				query := fmt.Sprintf("shape=%s&left=10&top=5&width=3&height=2", definition.ID)
				query = setQueryValue(query, "shape-label", data)
				if definition.ID == "rounded" {
					query = setQueryValue(query, "shape-radius", "0.5")
				}
				first := Element{ID: "owned", Kind: "shape", Query: query}
				fitShapeLabelUncached(&first, canvas[0], canvas[1])
				second := first
				fitShapeLabelUncached(&second, canvas[0], canvas[1])
				if !reflect.DeepEqual(first, second) {
					t.Fatalf("%s/%s/%v grew on another projection pass", definition.ID, font, canvas)
				}
			}
		}
	}
}
