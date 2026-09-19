package main

// The existing compositor uses the last mask covering each cell. Retaining
// that mask's source-line index answers every suffix query without rescanning
// the remaining slide for each exported row. This is local to one export.
type exportMaskCell struct {
	line   int
	colour string
}
type exportTransparencyIndex map[int]map[int]exportMaskCell

func indexExportTransparency(lines []Line, width, height int, slide Slide) exportTransparencyIndex {
	index := exportTransparencyIndex{}
	for i, line := range lines {
		if !transparencyMaskLine(line, slide) || line.Row < 0 || line.Row >= height {
			continue
		}
		cells := transparentShapeCellsFrom(lines[i:i+1], 0, width, height, slide)
		for row, columns := range cells {
			if index[row] == nil {
				index[row] = map[int]exportMaskCell{}
			}
			for col, colour := range columns {
				index[row][col] = exportMaskCell{i, colour}
			}
		}
	}
	return index
}

func (index exportTransparencyIndex) after(line, row int) map[int]map[int]string {
	var cells map[int]string
	for col, mask := range index[row] {
		if mask.line <= line {
			continue
		}
		if cells == nil {
			cells = map[int]string{}
		}
		cells[col] = mask.colour
	}
	if cells == nil {
		return nil
	}
	return map[int]map[int]string{row: cells}
}
