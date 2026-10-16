package main

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"sync"
	"testing"
)

func chartCacheFixture() ChartData {
	data := newChartData("column")
	data.CategoryColors = []ChartColorStyle{{Color: "#112233", ColorToken: "accent-1", LabelColor: "#ffffff"}}
	width, minimum, maximum := 90.0, -10.0, 100.0
	data.Options.FontWidth, data.Options.Minimum, data.Options.Maximum = &width, &minimum, &maximum
	return data
}

func TestChartGeometryFingerprintPublicFields(t *testing.T) {
	deck, data := Deck{Theme: builtinDeckTheme("clean-light")}, chartCacheFixture()
	inputs := deck.chartInputs(data, 1200, 700, "", "")
	key := func(data ChartData, width, height float64, inputs chartRenderInputs) [32]byte {
		t.Helper()
		result, err := chartGeometryKey(data, width, height, inputs)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	base := key(data, 1200, 700, inputs)
	// Each scalar field is tested independently, including options whose value
	// may not affect this particular chart type but is retained in public Data.
	checkFields := func(label string, value func(*ChartData) reflect.Value) {
		t.Helper()
		v := value(&data)
		for i := 0; i < v.NumField(); i++ {
			name := v.Type().Field(i).Name
			if name == "Extra" {
				continue
			}
			copy := cloneChart(data)
			field := value(&copy).Field(i)
			switch field.Kind() {
			case reflect.String:
				field.SetString(field.String() + "changed")
			case reflect.Int:
				field.SetInt(field.Int() + 1)
			case reflect.Bool:
				field.SetBool(!field.Bool())
			case reflect.Pointer:
				if field.Type() != reflect.TypeOf((*float64)(nil)) {
					t.Fatal("review new chart field", label, name)
				}
				p := 1.0
				if !field.IsNil() {
					p += field.Elem().Float()
				}
				field.Set(reflect.ValueOf(&p))
			case reflect.Slice:
				switch name {
				case "Categories", "CategoryColors", "Series", "Values":
					continue // Exercised explicitly below.
				default:
					t.Fatal("review new chart slice", label, name)
				}
			case reflect.Struct:
				if name != "Options" {
					t.Fatal("review new chart struct", label, name)
				}
				continue // Options exercised separately.
			default:
				t.Fatal("review new chart field", label, name)
			}
			if key(copy, 1200, 700, inputs) == base {
				t.Fatal("missing fingerprint dependency", label, name)
			}
		}
	}
	checkFields("chart", func(d *ChartData) reflect.Value { return reflect.ValueOf(d).Elem() })
	checkFields("options", func(d *ChartData) reflect.Value { return reflect.ValueOf(&d.Options).Elem() })
	checkFields("series", func(d *ChartData) reflect.Value { return reflect.ValueOf(&d.Series[0]).Elem() })
	checkFields("category style", func(d *ChartData) reflect.Value { return reflect.ValueOf(&d.CategoryColors[0]).Elem() })
	for name, mutate := range map[string]func(*ChartData){
		"category":               func(d *ChartData) { d.Categories[len(d.Categories)-1] += "changed" },
		"category length":        func(d *ChartData) { d.Categories = append(d.Categories, "") },
		"category styles length": func(d *ChartData) { d.CategoryColors = append(d.CategoryColors, ChartColorStyle{}) },
		"series length":          func(d *ChartData) { d.Series = append(d.Series, ChartSeries{}) },
		"value":                  func(d *ChartData) { d.Series[len(d.Series)-1].Values[len(d.Categories)-1]++ },
		"value length":           func(d *ChartData) { d.Series[0].Values = append(d.Series[0].Values, 0) },
	} {
		copy := cloneChart(data)
		mutate(&copy)
		if key(copy, 1200, 700, inputs) == base {
			t.Fatal("missing fingerprint dependency", name)
		}
	}
	if key(data, 1201, 700, inputs) == base || key(data, 1200, 701, inputs) == base {
		t.Fatal("size not fingerprinted")
	}
	for _, font := range []string{"mono", "sans", "c64"} {
		next := deck.chartInputs(data, 1200, 700, "#abcdef", font)
		if key(data, 1200, 700, next) == base {
			t.Fatal("resolved inputs not fingerprinted", font)
		}
	}
	// Length-delimited strings distinguish values that concatenate identically.
	a, b := cloneChart(data), cloneChart(data)
	a.Categories[0], a.Categories[1] = "ab", "c"
	b.Categories[0], b.Categories[1] = "a", "bc"
	if key(a, 1200, 700, inputs) == key(b, 1200, 700, inputs) {
		t.Fatal("ambiguous category boundaries")
	}
	private := &jsonExtensions{"private": json.RawMessage(`"DO NOT RETAIN"`)}
	data.Extra, data.Options.Extra, data.Series[0].Extra, data.CategoryColors[0].Extra = private, private, private, private
	if key(data, 1200, 700, inputs) != base {
		t.Fatal("private extensions affect public cache")
	}
	if _, err := chartGeometryKey(data, math.NaN(), 700, inputs); err == nil {
		t.Fatal("invalid render inputs not rejected")
	}
}

func TestChartGeometryCachedOwnershipAndColdParity(t *testing.T) {
	deck := Deck{Theme: builtinDeckTheme("clean-light")}
	for _, kind := range []string{"column", "bar", "line", "area", "pie", "doughnut", "stacked-column", "stacked-bar"} {
		t.Run(kind, func(t *testing.T) {
			data := chartCacheFixture()
			data.Type = kind
			if kind == "stacked-column" || kind == "stacked-bar" {
				data.Type = kind[8:]
				data.Options.Stacked = true
			}
			if kind == "pie" || kind == "doughnut" {
				data.Series = data.Series[:1]
			}
			data.Options.DataLabels, data.Options.Legend = true, true
			extra := &jsonExtensions{"private": json.RawMessage(`"DO NOT RETAIN"`)}
			data.Extra, data.Options.Extra, data.Series[0].Extra, data.CategoryColors[0].Extra = extra, extra, extra, extra
			bounds := sceneRect{Width: 1200, Height: 700}
			first, err := deck.projectChart(data, bounds, "")
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(first)
			if bytes.Contains(before, []byte("DO NOT RETAIN")) {
				t.Fatal("private metadata in cached data")
			}
			coldData := cloneChart(data)
			coldData.Extra, coldData.Options.Extra = nil, nil
			for i := range coldData.Series {
				coldData.Series[i].Extra = nil
			}
			for i := range coldData.CategoryColors {
				coldData.CategoryColors[i].Extra = nil
			}
			cold := layoutChart(coldData, bounds.Width, bounds.Height, deck.chartInputs(coldData, bounds.Width, bounds.Height, "", ""))
			if !reflect.DeepEqual(first, cold) {
				t.Fatal("cache changed cold geometry")
			}
			bounds.X, bounds.Y = 100, 200
			retained, err := deck.projectChart(data, bounds, "")
			if err != nil || retained != first {
				t.Fatal("translation did not retain immutable geometry", err)
			}
			data.Categories[0] = "mutated"
			data.Series[0].Values[0]++
			data.Series[0].Name = "mutated"
			data.CategoryColors[0].Color = "#abcdef"
			*data.Options.FontWidth, *data.Options.Minimum, *data.Options.Maximum = 80, -20, 120
			after, _ := json.Marshal(first)
			if !bytes.Equal(before, after) {
				t.Fatal("cache aliases source data")
			}
			changed, err := deck.projectChart(data, bounds, "")
			if err != nil || changed == first {
				t.Fatal("changed source reused stale geometry", err)
			}
			data.Series[0].Values[0] = math.NaN()
			if _, err := deck.projectChart(data, bounds, ""); err == nil {
				t.Fatal("cached path skipped validation")
			}
		})
	}
}

func TestChartGeometryCacheConcurrentBounded(t *testing.T) {
	deck, data := Deck{Theme: builtinDeckTheme("clean-light")}, chartCacheFixture()
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for j := 0; j < 40; j++ {
				bounds := sceneRect{Width: 1000 + float64(j), Height: 600 + float64(worker)}
				if _, err := deck.projectChart(data, bounds, ""); err != nil {
					t.Error(err)
					return
				}
			}
		}(i)
	}
	workers.Wait()
	sharedChartGeometry.Lock()
	defer sharedChartGeometry.Unlock()
	if sharedChartGeometry.bytes > 4<<20 || len(sharedChartGeometry.entries) > 32 {
		t.Fatal("unbounded chart cache")
	}
}
