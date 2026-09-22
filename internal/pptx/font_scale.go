package pptx

// This file contains the deliberately small TrueType transformation needed by
// Keynope's editable C64 export. Keynope's C64 width setting is a continuous
// horizontal scale. DrawingML has no run-level equivalent, so PPTX export
// creates a static, embedded face for every width used by a deck.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
)

// ScaleTrueTypeWidth returns an independent TrueType font whose glyph outlines
// and advance widths have been scaled horizontally. It supports the simple
// TrueType outlines used by Keynope C64. The source stays untouched.
func ScaleTrueTypeWidth(source []byte, scale float64) ([]byte, error) {
	if math.IsNaN(scale) || math.IsInf(scale, 0) || scale <= 0 || scale > 2 {
		return nil, fmt.Errorf("font width scale %.6g is outside 0..2", scale)
	}
	if math.Abs(scale-1) < 1e-9 {
		return append([]byte(nil), source...), nil
	}
	version, tables, err := copySFNTTables(source)
	if err != nil {
		return nil, err
	}
	head, hhea, hmtx := tables["head"], tables["hhea"], tables["hmtx"]
	loca, maxp, glyf := tables["loca"], tables["maxp"], tables["glyf"]
	if len(head) < 54 || len(hhea) < 36 || len(maxp) < 6 || len(loca) == 0 || len(glyf) == 0 {
		return nil, errors.New("font is missing required TrueType outline tables")
	}
	glyphCount := int(binary.BigEndian.Uint16(maxp[4:6]))
	if glyphCount < 1 {
		return nil, errors.New("font has no glyphs")
	}
	locaFormat := int16(binary.BigEndian.Uint16(head[50:52]))
	offsets, err := trueTypeLocaOffsets(loca, glyphCount, locaFormat)
	if err != nil {
		return nil, err
	}
	if offsets[len(offsets)-1] > len(glyf) {
		return nil, errors.New("font glyph locations exceed glyph table")
	}

	var rebuiltGlyphs []byte
	newOffsets := make([]int, glyphCount+1)
	for glyph := 0; glyph < glyphCount; glyph++ {
		newOffsets[glyph] = len(rebuiltGlyphs)
		start, end := offsets[glyph], offsets[glyph+1]
		if start == end {
			continue
		}
		rebuilt, err := scaleSimpleTrueTypeGlyph(glyf[start:end], scale)
		if err != nil {
			return nil, fmt.Errorf("scale glyph %d: %w", glyph, err)
		}
		rebuiltGlyphs = append(rebuiltGlyphs, rebuilt...)
		// loca offsets are measured in two-byte units for short locations and
		// all TrueType glyph data is required to begin on an even boundary.
		if len(rebuiltGlyphs)%2 != 0 {
			rebuiltGlyphs = append(rebuiltGlyphs, 0)
		}
	}
	newOffsets[glyphCount] = len(rebuiltGlyphs)

	// Keep the font's global X bounds and horizontal metrics in the same
	// coordinate system as the transformed outlines.
	if err := scaleInt16At(head, 36, scale); err != nil {
		return nil, fmt.Errorf("scale font xMin: %w", err)
	}
	if err := scaleInt16At(head, 40, scale); err != nil {
		return nil, fmt.Errorf("scale font xMax: %w", err)
	}
	if err := scaleTrueTypeHorizontalMetrics(hhea, hmtx, glyphCount, scale); err != nil {
		return nil, err
	}

	// Retain the compact short loca table whenever it still fits. A wider
	// instance can legitimately require long offsets.
	shortLoca := newOffsets[glyphCount] <= math.MaxUint16*2
	if shortLoca {
		for _, offset := range newOffsets {
			if offset%2 != 0 {
				shortLoca = false
				break
			}
		}
	}
	if shortLoca {
		loca = make([]byte, len(newOffsets)*2)
		for index, offset := range newOffsets {
			binary.BigEndian.PutUint16(loca[index*2:index*2+2], uint16(offset/2))
		}
		binary.BigEndian.PutUint16(head[50:52], 0)
	} else {
		loca = make([]byte, len(newOffsets)*4)
		for index, offset := range newOffsets {
			binary.BigEndian.PutUint32(loca[index*4:index*4+4], uint32(offset))
		}
		binary.BigEndian.PutUint16(head[50:52], 1)
	}
	tables["glyf"] = rebuiltGlyphs
	tables["loca"] = loca
	return buildSFNT(version, tables)
}

func scaleSimpleTrueTypeGlyph(source []byte, scale float64) ([]byte, error) {
	if len(source) < 10 {
		return nil, errors.New("truncated glyph")
	}
	contours := int(int16(binary.BigEndian.Uint16(source[0:2])))
	if contours < 0 {
		return nil, errors.New("compound glyphs are unsupported")
	}
	if contours == 0 {
		copy := append([]byte(nil), source...)
		if err := scaleInt16At(copy, 2, scale); err != nil {
			return nil, err
		}
		if err := scaleInt16At(copy, 6, scale); err != nil {
			return nil, err
		}
		return copy, nil
	}
	position := 10
	if position+contours*2+2 > len(source) {
		return nil, errors.New("truncated contour endpoints")
	}
	endPoints := append([]byte(nil), source[position:position+contours*2]...)
	pointCount := int(binary.BigEndian.Uint16(endPoints[len(endPoints)-2:])) + 1
	position += contours * 2
	instructionLength := int(binary.BigEndian.Uint16(source[position : position+2]))
	position += 2
	if instructionLength < 0 || position+instructionLength > len(source) {
		return nil, errors.New("truncated glyph instructions")
	}
	instructions := append([]byte(nil), source[position:position+instructionLength]...)
	position += instructionLength
	flags, next, err := trueTypeGlyphFlags(source, position, pointCount)
	if err != nil {
		return nil, err
	}
	x, next, err := trueTypeCoordinates(source, next, flags, true)
	if err != nil {
		return nil, err
	}
	y, _, err := trueTypeCoordinates(source, next, flags, false)
	if err != nil {
		return nil, err
	}
	for index := range x {
		scaled, err := scaleInt16(x[index], scale)
		if err != nil {
			return nil, err
		}
		x[index] = scaled
	}
	return encodeSimpleTrueTypeGlyph(source[:10], endPoints, instructions, flags, x, y)
}

func trueTypeGlyphFlags(source []byte, position, count int) ([]byte, int, error) {
	flags := make([]byte, 0, count)
	for len(flags) < count {
		if position >= len(source) {
			return nil, 0, errors.New("truncated glyph flags")
		}
		flag := source[position]
		position++
		flags = append(flags, flag)
		if flag&0x08 == 0 {
			continue
		}
		if position >= len(source) {
			return nil, 0, errors.New("truncated glyph flag repeat")
		}
		repeat := int(source[position])
		position++
		if len(flags)+repeat > count {
			return nil, 0, errors.New("glyph flag repeat exceeds point count")
		}
		for range repeat {
			flags = append(flags, flag)
		}
	}
	return flags, position, nil
}

func trueTypeCoordinates(source []byte, position int, flags []byte, horizontal bool) ([]int16, int, error) {
	values := make([]int16, len(flags))
	previous := int16(0)
	shortBit, sameBit := byte(0x04), byte(0x20)
	if horizontal {
		shortBit, sameBit = 0x02, 0x10
	}
	for index, flag := range flags {
		delta := int16(0)
		switch {
		case flag&shortBit != 0:
			if position >= len(source) {
				return nil, 0, errors.New("truncated short coordinate")
			}
			delta = int16(source[position])
			position++
			if flag&sameBit == 0 {
				delta = -delta
			}
		case flag&sameBit == 0:
			if position+2 > len(source) {
				return nil, 0, errors.New("truncated long coordinate")
			}
			delta = int16(binary.BigEndian.Uint16(source[position : position+2]))
			position += 2
		}
		value := int32(previous) + int32(delta)
		if value < math.MinInt16 || value > math.MaxInt16 {
			return nil, 0, errors.New("coordinate overflows int16")
		}
		previous = int16(value)
		values[index] = previous
	}
	return values, position, nil
}

func encodeSimpleTrueTypeGlyph(header, endPoints, instructions, originalFlags []byte, x, y []int16) ([]byte, error) {
	if len(x) != len(y) || len(x) != len(originalFlags) {
		return nil, errors.New("inconsistent glyph point data")
	}
	flags := make([]byte, len(x))
	xData, yData := make([]byte, 0, len(x)*2), make([]byte, 0, len(y)*2)
	previousX, previousY := int16(0), int16(0)
	for index := range x {
		// Preserve on-curve and overlap-simple flags. Coordinate encodings are
		// rebuilt because scaling can turn a one-byte delta into a two-byte one.
		flag := originalFlags[index] & 0x41
		dx, dy := int32(x[index])-int32(previousX), int32(y[index])-int32(previousY)
		var err error
		flag, xData, err = appendTrueTypeCoordinate(flag, xData, dx, true)
		if err != nil {
			return nil, err
		}
		flag, yData, err = appendTrueTypeCoordinate(flag, yData, dy, false)
		if err != nil {
			return nil, err
		}
		flags[index] = flag
		previousX, previousY = x[index], y[index]
	}

	output := make([]byte, 0, len(header)+len(endPoints)+len(instructions)+len(flags)+len(xData)+len(yData)+8)
	output = append(output, header...)
	if len(x) > 0 {
		minX, maxX, minY, maxY := x[0], x[0], y[0], y[0]
		for index := range x {
			minX, maxX = minInt16(minX, x[index]), maxInt16(maxX, x[index])
			minY, maxY = minInt16(minY, y[index]), maxInt16(maxY, y[index])
		}
		binary.BigEndian.PutUint16(output[2:4], uint16(minX))
		binary.BigEndian.PutUint16(output[4:6], uint16(minY))
		binary.BigEndian.PutUint16(output[6:8], uint16(maxX))
		binary.BigEndian.PutUint16(output[8:10], uint16(maxY))
	}
	output = append(output, endPoints...)
	var instructionSize [2]byte
	binary.BigEndian.PutUint16(instructionSize[:], uint16(len(instructions)))
	output = append(output, instructionSize[:]...)
	output = append(output, instructions...)
	for index := 0; index < len(flags); {
		flag := flags[index]
		run := 1
		for index+run < len(flags) && flags[index+run] == flag && run < 256 {
			run++
		}
		if run > 1 {
			output = append(output, flag|0x08, byte(run-1))
		} else {
			output = append(output, flag)
		}
		index += run
	}
	output = append(output, xData...)
	output = append(output, yData...)
	return output, nil
}

func appendTrueTypeCoordinate(flag byte, data []byte, delta int32, horizontal bool) (byte, []byte, error) {
	shortBit, sameBit := byte(0x04), byte(0x20)
	if horizontal {
		shortBit, sameBit = 0x02, 0x10
	}
	if delta == 0 {
		return flag | sameBit, data, nil
	}
	absolute := delta
	if absolute < 0 {
		absolute = -absolute
	}
	if absolute <= math.MaxUint8 {
		flag |= shortBit
		if delta > 0 {
			flag |= sameBit
		}
		return flag, append(data, byte(absolute)), nil
	}
	if delta < math.MinInt16 || delta > math.MaxInt16 {
		return 0, nil, errors.New("scaled coordinate delta overflows int16")
	}
	var value [2]byte
	binary.BigEndian.PutUint16(value[:], uint16(int16(delta)))
	return flag, append(data, value[:]...), nil
}

func scaleTrueTypeHorizontalMetrics(hhea, hmtx []byte, glyphCount int, scale float64) error {
	metricCount := int(binary.BigEndian.Uint16(hhea[34:36]))
	if metricCount < 1 || metricCount > glyphCount || len(hmtx) < metricCount*4+(glyphCount-metricCount)*2 {
		return errors.New("invalid horizontal metrics table")
	}
	for index := 0; index < metricCount; index++ {
		advance := int(binary.BigEndian.Uint16(hmtx[index*4 : index*4+2]))
		scaled := int(math.Round(float64(advance) * scale))
		if scaled < 1 || scaled > math.MaxUint16 {
			return errors.New("scaled advance width overflows uint16")
		}
		binary.BigEndian.PutUint16(hmtx[index*4:index*4+2], uint16(scaled))
		if err := scaleInt16At(hmtx, index*4+2, scale); err != nil {
			return err
		}
	}
	for index := metricCount; index < glyphCount; index++ {
		if err := scaleInt16At(hmtx, metricCount*4+(index-metricCount)*2, scale); err != nil {
			return err
		}
	}
	return nil
}

func trueTypeLocaOffsets(loca []byte, glyphCount int, format int16) ([]int, error) {
	offsets := make([]int, glyphCount+1)
	switch format {
	case 0:
		if len(loca) < len(offsets)*2 {
			return nil, errors.New("truncated short glyph locations")
		}
		for index := range offsets {
			offsets[index] = int(binary.BigEndian.Uint16(loca[index*2:index*2+2])) * 2
		}
	case 1:
		if len(loca) < len(offsets)*4 {
			return nil, errors.New("truncated long glyph locations")
		}
		for index := range offsets {
			value := binary.BigEndian.Uint32(loca[index*4 : index*4+4])
			if uint64(value) > uint64(math.MaxInt) {
				return nil, errors.New("glyph location overflows int")
			}
			offsets[index] = int(value)
		}
	default:
		return nil, errors.New("unsupported glyph location format")
	}
	for index := 1; index < len(offsets); index++ {
		if offsets[index] < offsets[index-1] {
			return nil, errors.New("glyph locations are not sorted")
		}
	}
	return offsets, nil
}

func scaleInt16At(data []byte, offset int, scale float64) error {
	if offset < 0 || offset+2 > len(data) {
		return errors.New("truncated int16 field")
	}
	scaled, err := scaleInt16(int16(binary.BigEndian.Uint16(data[offset:offset+2])), scale)
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint16(data[offset:offset+2], uint16(scaled))
	return nil
}

func scaleInt16(value int16, scale float64) (int16, error) {
	scaled := math.Round(float64(value) * scale)
	if scaled < math.MinInt16 || scaled > math.MaxInt16 {
		return 0, errors.New("scaled int16 overflows")
	}
	return int16(scaled), nil
}

func minInt16(a, b int16) int16 {
	if a < b {
		return a
	}
	return b
}
func maxInt16(a, b int16) int16 {
	if a > b {
		return a
	}
	return b
}

func copySFNTTables(source []byte) (uint32, map[string][]byte, error) {
	if len(source) < 12 {
		return 0, nil, errors.New("invalid sfnt header")
	}
	count := int(binary.BigEndian.Uint16(source[4:6]))
	if count < 1 || 12+count*16 > len(source) {
		return 0, nil, errors.New("invalid sfnt table directory")
	}
	tables := make(map[string][]byte, count)
	for index := 0; index < count; index++ {
		offset := 12 + index*16
		name := string(source[offset : offset+4])
		start := int(binary.BigEndian.Uint32(source[offset+8 : offset+12]))
		length := int(binary.BigEndian.Uint32(source[offset+12 : offset+16]))
		if start < 0 || length < 0 || start > len(source) || length > len(source)-start || tables[name] != nil {
			return 0, nil, errors.New("invalid sfnt table bounds")
		}
		tables[name] = append([]byte(nil), source[start:start+length]...)
	}
	return binary.BigEndian.Uint32(source[:4]), tables, nil
}

func buildSFNT(version uint32, tables map[string][]byte) ([]byte, error) {
	if len(tables) < 1 || len(tables) > math.MaxUint16 {
		return nil, errors.New("invalid sfnt table count")
	}
	tags := make([]string, 0, len(tables))
	for tag, data := range tables {
		if len(tag) != 4 || len(data) == 0 {
			return nil, errors.New("invalid sfnt table")
		}
		tags = append(tags, tag)
	}
	head, ok := tables["head"]
	if !ok || len(head) < 12 {
		return nil, errors.New("font has no usable head table")
	}
	// The head table's own checksum is calculated with this field cleared.
	binary.BigEndian.PutUint32(head[8:12], 0)
	sort.Strings(tags)
	maxPower, selector := 1, 0
	for maxPower*2 <= len(tags) {
		maxPower *= 2
		selector++
	}
	headerSize := 12 + len(tags)*16
	offsets := make(map[string]int, len(tags))
	offset := headerSize
	for _, tag := range tags {
		offsets[tag] = offset
		offset += len(tables[tag])
		if remainder := offset % 4; remainder != 0 {
			offset += 4 - remainder
		}
	}
	output := make([]byte, offset)
	binary.BigEndian.PutUint32(output[:4], version)
	binary.BigEndian.PutUint16(output[4:6], uint16(len(tags)))
	binary.BigEndian.PutUint16(output[6:8], uint16(maxPower*16))
	binary.BigEndian.PutUint16(output[8:10], uint16(selector))
	binary.BigEndian.PutUint16(output[10:12], uint16(len(tags)*16-maxPower*16))
	for index, tag := range tags {
		record := 12 + index*16
		copy(output[record:record+4], tag)
		binary.BigEndian.PutUint32(output[record+4:record+8], sfntChecksum(tables[tag]))
		binary.BigEndian.PutUint32(output[record+8:record+12], uint32(offsets[tag]))
		binary.BigEndian.PutUint32(output[record+12:record+16], uint32(len(tables[tag])))
		copy(output[offsets[tag]:], tables[tag])
	}
	headOffset := offsets["head"]
	// Table checksums are calculated with checkSumAdjustment cleared.
	binary.BigEndian.PutUint32(output[headOffset+8:headOffset+12], 0)
	adjustment := uint32(0xB1B0AFBA - sfntChecksum(output))
	binary.BigEndian.PutUint32(output[headOffset+8:headOffset+12], adjustment)
	return output, nil
}

func sfntChecksum(data []byte) uint32 {
	var sum uint32
	for offset := 0; offset < len(data); offset += 4 {
		var word [4]byte
		copy(word[:], data[offset:minInt(offset+4, len(data))])
		sum += binary.BigEndian.Uint32(word[:])
	}
	return sum
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
