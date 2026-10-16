package pptx

import "testing"

func TestChartLabelScopesDoNotEnableOfficeDefaults(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut"} {
		for _, enabled := range []bool{false, true} {
			c := chartFixture(kind)
			c.Options.DataLabels = enabled
			root, err := parseXML([]byte(chartXML(c)), 128)
			if err != nil {
				t.Fatal(err)
			}
			blocks := descendants(root, "dLbls")
			blocks = append(blocks, descendants(root, "dLbl")...)
			if len(blocks) == 0 {
				t.Fatal("fixture did not exercise formatted labels", kind)
			}
			circular := kind == "pie" || kind == "doughnut"
			for _, block := range blocks {
				for _, flag := range []string{"showLegendKey", "showCatName", "showSerName", "showBubbleSize"} {
					if attr(findChild(block, flag), "val") != "0" {
						t.Fatalf("%s %s omitted explicit %s", kind, block.Name, flag)
					}
				}
				for flag, want := range map[string]int{"showVal": boolNumber(enabled && !circular), "showPercent": boolNumber(enabled && circular)} {
					got := attr(findChild(block, flag), "val")
					if got != string(rune('0'+want)) {
						t.Fatalf("%s %s %s=%s, want %d", kind, block.Name, flag, got, want)
					}
				}
				if circular && attr(findChild(block, "numFmt"), "formatCode") != "0%" {
					t.Fatal("circular labels must be shares", kind)
				}
				if kind == "pie" && attr(findChild(block, "dLblPos"), "val") != "ctr" {
					t.Fatal("pie labels must be centred", kind)
				}
				if kind != "pie" && findChild(block, "dLblPos") != nil {
					t.Fatal("position override is not valid for this chart", kind)
				}
			}
			for _, series := range descendants(root, "ser") {
				invert := findChild(series, "invertIfNegative")
				if kind == "column" || kind == "bar" {
					if attr(invert, "val") != "0" {
						t.Fatal("negative bars can invert their authored colour", kind)
					}
				} else if invert != nil {
					t.Fatal("bar-only property emitted for", kind)
				}
			}
		}
	}
}
