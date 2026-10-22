package effects

// StrokeWidths is PhotoCraft compose/effects.rs::stroke_widths. Position is
// independent of the object's geometry and the colour/paint used for its edge.
func StrokeWidths(position string, size float64) (inside, outside float64) {
	switch position {
	case "inside":
		return size, 0
	case "outside":
		return 0, size
	default:
		return size / 2, size / 2
	}
}
