package app

import (
	"maps"
	"reflect"
)

// Exact value comparison, not a cached fingerprint or pointer-based ownership
// shortcut. The request still compares every table input/output property; it
// simply avoids reflect walking hundreds of cloned text/paint records.
func workspaceSceneTableEqual(a, b *sceneTable) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Header != b.Header || a.Banded != b.Banded || !sceneComparableSliceEqual(a.Rows, b.Rows) || !sceneComparableSliceEqual(a.Columns, b.Columns) || !sceneComparableSliceEqual(a.Borders, b.Borders) {
		return false
	}
	if (a.Cells == nil) != (b.Cells == nil) || len(a.Cells) != len(b.Cells) {
		return false
	}
	if len(a.Cells) > 0 && &a.Cells[0] == &b.Cells[0] {
		return true
	}
	for i, x := range a.Cells {
		y := b.Cells[i]
		if !sceneShapedTextEqual(x.Text, y.Text) || !sceneFloatPointerEqual(x.Paint.Opacity, y.Paint.Opacity) {
			return false
		}
		x.Text, y.Text = nil, nil
		x.Paint.Opacity, y.Paint.Opacity = nil, nil
		if x != y {
			return false
		}
	}
	return true
}

func sceneComparableSliceEqual[T comparable](a, b []T) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	// Preserve DeepEqual's same-backing-array rule, including malformed NaNs.
	if len(a) > 0 && &a[0] == &b[0] {
		return true
	}
	for i, x := range a {
		if x != b[i] {
			return false
		}
	}
	return true
}

func sceneFloatPointerEqual(a, b *float64) bool {
	return a == b || a != nil && b != nil && *a == *b
}

type sceneTextScalarValues struct {
	ReadOnly                                                     bool
	SeeThrough                                                   bool
	EmojiTint                                                    string
	EmojiWidthScale, ParagraphBefore, ParagraphAfter, LineHeight float64
	FontID                                                       string
	WidthScale                                                   float64
	Role                                                         string
	Level                                                        int
	Size                                                         float64
	Family, Align, Vertical                                      string
}

func sceneTextScalars(t *sceneText) sceneTextScalarValues {
	return sceneTextScalarValues{ReadOnly: t.ReadOnly, SeeThrough: t.SeeThrough, EmojiTint: t.EmojiTint, EmojiWidthScale: t.EmojiWidthScale, ParagraphBefore: t.ParagraphBefore, ParagraphAfter: t.ParagraphAfter, LineHeight: t.LineHeight, FontID: t.FontID, WidthScale: t.WidthScale, Role: t.Role, Level: t.Level, Size: t.Size, Family: t.Family, Align: t.Align, Vertical: t.Vertical}
}

func sceneStringMapEqual(a, b map[string]string) bool {
	return (a == nil) == (b == nil) && maps.Equal(a, b)
}

func sceneShapedTextEqual(a, b *sceneText) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if sceneTextScalars(a) != sceneTextScalars(b) || !reflect.DeepEqual(a.TypefaceMetrics, b.TypefaceMetrics) || !sceneStringMapEqual(a.FontRoles, b.FontRoles) || !sceneStringMapEqual(a.ColorTokens, b.ColorTokens) || !sceneStringMapEqual(a.EmojiFonts, b.EmojiFonts) || !sceneComparableSliceEqual(a.Runs, b.Runs) {
		return false
	}
	if (a.Paragraphs == nil) != (b.Paragraphs == nil) || len(a.Paragraphs) != len(b.Paragraphs) {
		return false
	}
	if len(a.Paragraphs) > 0 && &a.Paragraphs[0] == &b.Paragraphs[0] {
		return true
	}
	for i, p := range a.Paragraphs {
		if p.Bullet != b.Paragraphs[i].Bullet || !sceneComparableSliceEqual(p.Runs, b.Paragraphs[i].Runs) {
			return false
		}
	}
	return true
}
