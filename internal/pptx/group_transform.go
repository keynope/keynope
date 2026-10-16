package pptx

import (
	"fmt"
	"math"
	"strconv"
)

// Flatten the group's child coordinate system only after nested groups have
// flattened their own children. Each object here is owned by this parse.
func applyGroupTransform(group *node, objects []Object) ([]Warning, error) {
	xfrm := findChild(findChild(group, "grpSpPr"), "xfrm")
	if xfrm == nil {
		return nil, nil
	}
	read := func(node *node, key string) (float64, error) {
		if node == nil {
			return 0, nil
		}
		value, err := strconv.ParseFloat(attr(node, key), 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, fmt.Errorf("invalid group transform %s", key)
		}
		return value / 6350, nil
	}
	var values [8]float64
	for i, item := range []struct {
		node string
		key  string
	}{{"off", "x"}, {"off", "y"}, {"ext", "cx"}, {"ext", "cy"}, {"chOff", "x"}, {"chOff", "y"}, {"chExt", "cx"}, {"chExt", "cy"}} {
		value, err := read(findChild(xfrm, item.node), item.key)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	x, y, w, h, cx, cy, cw, ch := values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7]
	if w < 0 || h < 0 || cw < 0 || ch < 0 {
		return nil, fmt.Errorf("group transform extents must be non-negative")
	}
	// Each component is optional in CT_GroupTransform2D. Missing child
	// extents mean no scale; matching zero extents describe a degenerate
	// axis (for example a group of horizontal lines), not a division by zero.
	if findChild(xfrm, "chExt") == nil {
		cw, ch = w, h
	}
	if findChild(xfrm, "ext") == nil {
		w, h = cw, ch
	}
	scale := func(outer, inner float64) (float64, error) {
		if inner == 0 {
			if outer == 0 {
				return 1, nil
			}
			return 0, fmt.Errorf("group transform cannot scale a zero child extent")
		}
		return outer / inner, nil
	}
	sx, err := scale(w, cw)
	if err != nil {
		return nil, err
	}
	sy, err := scale(h, ch)
	if err != nil {
		return nil, err
	}
	rotation := 0.0
	if raw := attr(xfrm, "rot"); raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("invalid group rotation")
		}
		rotation = value / 60000
	}
	flipH, flipV := attr(xfrm, "flipH") == "1" || attr(xfrm, "flipH") == "true", attr(xfrm, "flipV") == "1" || attr(xfrm, "flipV") == "true"
	radians := rotation * math.Pi / 180
	sin, cos := math.Sin(radians), math.Cos(radians)
	var warnings []Warning
	for i := range objects {
		o := &objects[i]
		if len(o.LinePoints) > 1 {
			// Routes can represent affine transforms exactly, including skew.
			// Bake the object's transform before the enclosing group's matrix;
			// leave no flip/rotation behind to apply twice on a nested group.
			points := append([]LinePoint(nil), o.LinePoints...)
			ownAngle := o.Rotation * math.Pi / 180
			ownSin, ownCos := math.Sin(ownAngle), math.Cos(ownAngle)
			centerX, centerY := o.X+o.Width/2, o.Y+o.Height/2
			for j, p := range points {
				dx, dy := p.X-centerX, p.Y-centerY
				if o.FlipH {
					dx = -dx
				}
				if o.FlipV {
					dy = -dy
				}
				px, py := centerX+dx*ownCos-dy*ownSin, centerY+dx*ownSin+dy*ownCos
				dx, dy = x+(px-cx)*sx-(x+w/2), y+(py-cy)*sy-(y+h/2)
				if flipH {
					dx = -dx
				}
				if flipV {
					dy = -dy
				}
				points[j] = LinePoint{X: x + w/2 + dx*cos - dy*sin, Y: y + h/2 + dx*sin + dy*cos}
			}
			if err := validateLinePoints(points); err != nil {
				return nil, err
			}
			o.LinePoints = points
			_, o.X, o.Y, o.Width, o.Height = routedLineGeometry(points)
			o.Rotation, o.FlipH, o.FlipV = 0, false, false
			o.StrokeWidth *= math.Sqrt(sx * sy)
			continue
		}
		px, py := x+(o.X+o.Width/2-cx)*sx, y+(o.Y+o.Height/2-cy)*sy
		dx, dy := px-(x+w/2), py-(y+h/2)
		if flipH {
			dx = -dx
		}
		if flipV {
			dy = -dy
		}
		px, py = x+w/2+dx*cos-dy*sin, y+h/2+dx*sin+dy*cos
		r := o.Rotation * math.Pi / 180
		ux, uy := math.Cos(r)*sx, math.Sin(r)*sy
		vx, vy := -math.Sin(r)*sx, math.Cos(r)*sy
		width, height := o.Width*math.Hypot(ux, uy), o.Height*math.Hypot(vx, vy)
		if math.Abs(ux*vx+uy*vy) > 1e-8*math.Hypot(ux, uy)*math.Hypot(vx, vy) {
			warnings = append(warnings, Warning{Object: o.ID, Code: "group-skew-approximated", Message: "Non-uniform group scaling of rotated content requires skew; editable bounds and rotation were approximated."})
		}
		angle := math.Atan2(uy, ux) * 180 / math.Pi
		if flipH != flipV {
			angle = -angle
		}
		o.X, o.Y, o.Width, o.Height, o.Rotation = px-width/2, py-height/2, width, height, angle+rotation
		if flipH {
			o.FlipH = !o.FlipH
		}
		if flipV {
			o.FlipV = !o.FlipV
		}
		if (flipH || flipV) && o.Kind != "image" {
			warnings = append(warnings, Warning{Object: o.ID, Code: "group-reflection-approximated", Message: "Group reflection is retained in geometry but reflected artwork may differ in Keynope."})
		}
		for _, v := range []float64{o.X, o.Y, o.Width, o.Height, o.Rotation, sx, sy} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e12 {
				return nil, fmt.Errorf("group transform exceeds supported geometry")
			}
		}
		o.FontSize *= sy
		o.StrokeWidth *= math.Sqrt(sx * sy)
		if o.Shadow != nil {
			shadow := cloneShapeShadow(o.Shadow)
			shadow.X *= sx
			shadow.Y *= sy
			shadow.Blur *= math.Sqrt(sx * sy)
			o.Shadow = shadow
			if math.Abs(sx-sy) > 1e-8 || flipH || flipV {
				warnings = append(warnings, Warning{Object: o.ID, Code: "group-shadow-approximated", Message: "Group scaling/reflection uses approximate shadow offset and blur; shape and shadow remain editable."})
			}
		}
		for p := range o.Paragraphs {
			for r := range o.Paragraphs[p].Runs {
				o.Paragraphs[p].Runs[r].FontSize *= sy
			}
		}
		if o.Text != "" && math.Abs(sx-sy) > 1e-8 {
			warnings = append(warnings, Warning{Object: o.ID, Code: "group-text-width-approximated", Message: "Text size follows group height; non-uniform group font width is approximated."})
		}
		if o.Chart != nil {
			o.Chart.FontSize *= sy
		}
		if o.Table != nil {
			for r := range o.Table.Rows {
				o.Table.Rows[r] *= sy
			}
			for c := range o.Table.Columns {
				o.Table.Columns[c] *= sx
			}
			for r := range o.Table.Cells {
				for c := range o.Table.Cells[r] {
					cell := &o.Table.Cells[r][c]
					cell.FontSize *= sy
					cell.Padding *= math.Min(sx, sy)
					cell.BorderWidth *= math.Sqrt(sx * sy)
					for side, border := range cell.Borders {
						border.Width *= math.Sqrt(sx * sy)
						cell.Borders[side] = border
					}
					for j := range cell.Runs {
						cell.Runs[j].FontSize *= sy
					}
				}
			}
		}
	}
	return warnings, nil
}
