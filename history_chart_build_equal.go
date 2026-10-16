package main

func historyComparableSliceEqual[T comparable](a, b []T) bool {
	if !historySliceShapeEqual(a, b) {
		return false
	}
	if historySameSlice(a, b) {
		return true
	}
	for i, value := range a {
		if value != b[i] {
			return false
		}
	}
	return true
}

func historyChartEqual(a, b ChartData) bool {
	if a.Version != b.Version || a.Type != b.Type || a.Title != b.Title || !historyExtraEqual(a.Extra, b.Extra) || !historyComparableSliceEqual(a.Categories, b.Categories) || !historyChartOptionsEqual(a.Options, b.Options) || !historySliceShapeEqual(a.Series, b.Series) || !historySliceShapeEqual(a.CategoryColors, b.CategoryColors) {
		return false
	}
	if !historySameSlice(a.CategoryColors, b.CategoryColors) {
		for i, color := range a.CategoryColors {
			other := b.CategoryColors[i]
			if !historyExtraEqual(color.Extra, other.Extra) {
				return false
			}
			color.Extra, other.Extra = nil, nil
			if color != other {
				return false
			}
		}
	}
	if !historySameSlice(a.Series, b.Series) {
		for i, series := range a.Series {
			other := b.Series[i]
			if series.LabelColor != other.LabelColor || series.Name != other.Name || series.Color != other.Color || series.ColorToken != other.ColorToken || !historyExtraEqual(series.Extra, other.Extra) || !historyComparableSliceEqual(series.Values, other.Values) {
				return false
			}
		}
	}
	return true
}

func historyChartOptionsEqual(a, b ChartOptions) bool {
	if !historyExtraEqual(a.Extra, b.Extra) || !historyFloatEqual(a.Minimum, b.Minimum) || !historyFloatEqual(a.Maximum, b.Maximum) || !historyFloatEqual(a.FontWidth, b.FontWidth) {
		return false
	}
	a.Extra, b.Extra = nil, nil
	a.Minimum, b.Minimum, a.Maximum, b.Maximum, a.FontWidth, b.FontWidth = nil, nil, nil, nil, nil, nil
	return a == b
}

func historyBuildEqual(a, b SlideBuild) bool {
	if a.Version != b.Version || !historyExtraEqual(a.Extra, b.Extra) || !historyExtraEqual(a.Transition.Extra, b.Transition.Extra) || !historySliceShapeEqual(a.Steps, b.Steps) {
		return false
	}
	a.Transition.Extra, b.Transition.Extra = nil, nil
	if a.Transition != b.Transition {
		return false
	}
	if historySameSlice(a.Steps, b.Steps) {
		return true
	}
	for i, step := range a.Steps {
		other := b.Steps[i]
		if step.Effect != other.Effect || step.Direction != other.Direction || step.DurationMS != other.DurationMS || step.Advance != other.Advance || step.DelayMS != other.DelayMS || !historyExtraEqual(step.Extra, other.Extra) || !historyComparableSliceEqual(step.TargetIDs, other.TargetIDs) {
			return false
		}
	}
	return true
}
