package app

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math"
)

// Fingerprint public render inputs without cloning or JSON-encoding numeric
// datasets on a cache hit. Length delimiters prevent ambiguous concatenations;
// a fixed digest also avoids retaining a second copy of the dataset as a key.
// Callers still validate the entire dataset before consulting the cache.
func chartGeometryKey(data ChartData, width, height float64, inputs chartRenderInputs) ([32]byte, error) {
	options := data.Options
	options.Extra = nil
	header, err := json.Marshal(struct {
		Version       int
		Type, Title   string
		Width, Height float64
		Options       ChartOptions
		Inputs        chartRenderInputs
	}{data.Version, data.Type, data.Title, width, height, options, inputs})
	if err != nil {
		return [32]byte{}, err
	}
	digest := sha256.New()
	h := bufio.NewWriterSize(digest, 4096)
	var number [8]byte
	writeNumber := func(value uint64) {
		binary.LittleEndian.PutUint64(number[:], value)
		h.Write(number[:])
	}
	writeString := func(value string) {
		writeNumber(uint64(len(value)))
		h.WriteString(value)
	}
	writeNumber(uint64(len(header)))
	h.Write(header)
	writeNumber(uint64(len(data.Categories)))
	for _, category := range data.Categories {
		writeString(category)
	}
	writeNumber(uint64(len(data.CategoryColors)))
	for _, style := range data.CategoryColors {
		writeString(style.LabelColor)
		writeString(style.Color)
		writeString(style.ColorToken)
	}
	writeNumber(uint64(len(data.Series)))
	for _, series := range data.Series {
		writeString(series.LabelColor)
		writeString(series.Name)
		writeString(series.Color)
		writeString(series.ColorToken)
		writeNumber(uint64(len(series.Values)))
		for _, value := range series.Values {
			writeNumber(math.Float64bits(value))
		}
	}
	h.Flush() // sha256 writes cannot fail.
	var key [32]byte
	digest.Sum(key[:0])
	return key, nil
}

// Only misses need an owned dataset. Do not even clone private extensions:
// they are not render inputs and must never enter the shared public cache.
func clonePublicChart(data ChartData) ChartData {
	data.Extra = nil
	data.Options.Extra = nil
	data.Categories = append([]string(nil), data.Categories...)
	if data.CategoryColors != nil {
		data.CategoryColors = append([]ChartColorStyle{}, data.CategoryColors...)
		for i := range data.CategoryColors {
			data.CategoryColors[i].Extra = nil
		}
	}
	data.Series = append([]ChartSeries(nil), data.Series...)
	for i := range data.Series {
		data.Series[i].Extra = nil
		data.Series[i].Values = append([]float64(nil), data.Series[i].Values...)
	}
	for _, field := range []**float64{&data.Options.FontWidth, &data.Options.Minimum, &data.Options.Maximum} {
		if *field != nil {
			value := **field
			*field = &value
		}
	}
	return data
}
