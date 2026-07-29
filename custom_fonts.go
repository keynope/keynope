package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

const deckFontsVersion = 1
const deckFontMaxGlyphWidth = 32
const deckFigletMaxGlyphWidth = 128
const deckFigletMaxGlyphHeight = 32
const deckFontModeCells = "cells"
const deckFontModeFiglet = "figlet"

var keynopeFontsRE = regexp.MustCompile(`<!--\s*keynope-fonts\s+version=1\s+base64:([A-Za-z0-9+/=]+)\s*-->\s*`)

var (
	deckFontRegistryMu sync.RWMutex
	deckFontRegistry   = map[string]compiledDeckFont{}
)

type compiledDeckFont struct {
	normal map[rune][]string
	bold   map[rune][]string
	mode   string
	layout int
}

func cloneDeckFont(font DeckFont) DeckFont {
	out := font
	out.Normal = cloneDeckFontFace(font.Normal)
	out.Bold = cloneDeckFontFace(font.Bold)
	return out
}

func cloneDeckFontFace(face map[string][]string) map[string][]string {
	if len(face) == 0 {
		return nil
	}
	out := make(map[string][]string, len(face))
	for character, rows := range face {
		out[character] = append([]string(nil), rows...)
	}
	return out
}

func normalizeDeckFont(font DeckFont) (DeckFont, error) {
	font.ID = normalizeDeckFontID(font.ID)
	if font.ID == "" || font.ID == "default" {
		return DeckFont{}, errors.New("font needs a unique ID")
	}
	font.Name = strings.TrimSpace(font.Name)
	if font.Name == "" {
		font.Name = font.ID
	}
	font.Mode = strings.ToLower(strings.TrimSpace(font.Mode))
	if font.Mode != "" && font.Mode != deckFontModeCells && font.Mode != deckFontModeFiglet {
		return DeckFont{}, fmt.Errorf("unsupported font mode %q", font.Mode)
	}
	if font.Mode == deckFontModeFiglet {
		normal, height, err := normalizeDeckFigletFace(font.Normal, font.Height)
		if err != nil {
			return DeckFont{}, fmt.Errorf("normal face: %w", err)
		}
		boldSource := font.Bold
		if len(boldSource) == 0 {
			boldSource = normal
		}
		bold, boldHeight, err := normalizeDeckFigletFace(boldSource, height)
		if err != nil {
			return DeckFont{}, fmt.Errorf("bold face: %w", err)
		}
		if boldHeight != height {
			return DeckFont{}, errors.New("normal and bold FIGlet faces must have the same height")
		}
		font.Height, font.Normal, font.Bold = height, normal, bold
		return font, nil
	}
	normal, err := normalizeDeckFontFace(font.Normal, c64FullFont, 8, font.Mode)
	if err != nil {
		return DeckFont{}, fmt.Errorf("normal face: %w", err)
	}
	bold, err := normalizeDeckFontFace(font.Bold, c64BoldFont, 10, font.Mode)
	if err != nil {
		return DeckFont{}, fmt.Errorf("bold face: %w", err)
	}
	font.Normal, font.Bold = normal, bold
	return font, nil
}

func normalizeDeckFigletFace(face map[string][]string, requestedHeight int) (map[string][]string, int, error) {
	height := requestedHeight
	if height <= 0 {
		for _, rows := range face {
			if len(rows) > 0 {
				height = len(rows)
				break
			}
		}
	}
	if height <= 0 || height > deckFigletMaxGlyphHeight {
		return nil, 0, fmt.Errorf("height must be between 1 and %d", deckFigletMaxGlyphHeight)
	}
	fallback := face["?"]
	out := make(map[string][]string, 95)
	for code := 32; code <= 126; code++ {
		character := string(rune(code))
		rows := face[character]
		if len(rows) == 0 {
			rows = fallback
		}
		if len(rows) == 0 {
			rows = make([]string, height)
		}
		if len(rows) != height {
			return nil, 0, fmt.Errorf("%q must contain %d rows", character, height)
		}
		width := 1
		for _, row := range rows {
			if !utf8.ValidString(row) {
				return nil, 0, fmt.Errorf("%q contains invalid UTF-8", character)
			}
			width = max(width, utf8.RuneCountInString(row))
		}
		if width > deckFigletMaxGlyphWidth {
			return nil, 0, fmt.Errorf("%q is %d cells wide; maximum is %d", character, width, deckFigletMaxGlyphWidth)
		}
		normalized := make([]string, height)
		for rowIndex, row := range rows {
			var line strings.Builder
			for _, cell := range row {
				if isAllowedFigletCell(cell) {
					line.WriteRune(cell)
				} else {
					line.WriteByte(' ')
				}
			}
			normalized[rowIndex] = padRunes(line.String(), width)
		}
		out[character] = normalized
	}
	return out, height, nil
}

func isAllowedFigletCell(character rune) bool {
	if character == ' ' {
		return true
	}
	if character < 0x21 || character == 0x7f || character == '\u200d' || character == '\ufe0f' {
		return false
	}
	// FIGlet art is character-cell artwork. Exclude emoji/pictograph planes while
	// retaining ASCII, box drawing, blocks, mathematical symbols and dingbats.
	switch {
	case character >= 0x1f000 && character <= 0x1faff:
		return false
	case character >= 0x2600 && character <= 0x27bf:
		return false
	}
	return true
}

func normalizeDeckFontID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	dash := false
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			out.WriteRune(character)
			dash = false
		} else if out.Len() > 0 && !dash {
			out.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(out.String(), "-")
}

func normalizeDeckFontFace(face map[string][]string, fallback map[rune][]string, fallbackWidth int, mode string) (map[string][]string, error) {
	out := make(map[string][]string, 95)
	rowCount := 8
	for code := 32; code <= 126; code++ {
		character := string(rune(code))
		rows := face[character]
		if len(rows) == 0 {
			if mode == deckFontModeCells {
				rows = builtinDeckCellFontGlyph(code, fallback, fallbackWidth)
			} else {
				rows = builtinDeckFontGlyph(code, fallback, fallbackWidth)
			}
		}
		if len(rows) != rowCount {
			return nil, fmt.Errorf("%q must contain %d rows", character, rowCount)
		}
		width := 1
		for _, row := range rows {
			width = max(width, utf8.RuneCountInString(row))
		}
		if width > deckFontMaxGlyphWidth {
			return nil, fmt.Errorf("%q is %d pixels wide; maximum is %d", character, width, deckFontMaxGlyphWidth)
		}
		normalized := make([]string, rowCount)
		for rowIndex, row := range rows {
			var line strings.Builder
			for _, pixel := range row {
				if mode == deckFontModeCells {
					switch {
					case pixel == '.' || pixel == ' ':
						line.WriteByte('.')
					case pixel == '#':
						line.WriteRune('█')
					case isDeckFontBlockCell(pixel):
						line.WriteRune(pixel)
					default:
						line.WriteByte('.')
					}
				} else if pixel == '#' || pixel == '█' {
					line.WriteByte('#')
				} else {
					line.WriteByte('.')
				}
			}
			normalized[rowIndex] = padRight(line.String(), width)
			normalized[rowIndex] = strings.ReplaceAll(normalized[rowIndex], " ", ".")
		}
		out[character] = normalized
	}
	return out, nil
}

func builtinDeckFontGlyph(code int, fallback map[rune][]string, fallbackWidth int) []string {
	glyph := fallback[rune(code)]
	if glyph == nil {
		glyph = fallback['?']
	}
	rows := make([]string, 8)
	for row := range rows {
		rows[row] = strings.Map(func(pixel rune) rune {
			if pixel == ' ' {
				return '.'
			}
			return '#'
		}, padRunes(glyph[row], fallbackWidth))
	}
	return rows
}

func builtinDeckCellFontGlyph(code int, fallback map[rune][]string, fallbackWidth int) []string {
	pixels := builtinDeckFontGlyph(code, fallback, fallbackWidth)
	rows := make([]string, 8)
	for y := range rows {
		var row strings.Builder
		for _, pixel := range pixels[y] {
			if pixel == '#' {
				row.WriteRune('█')
			} else {
				row.WriteByte('.')
			}
		}
		rows[y] = row.String()
	}
	return rows
}

func isDeckFontBlockCell(character rune) bool {
	return character >= '\u2580' && character <= '\u259f'
}

func defaultEditableDeckFont() DeckFont {
	font := DeckFont{ID: "default", Name: "Keynope Default", Normal: map[string][]string{}, Bold: map[string][]string{}}
	for code := 32; code <= 126; code++ {
		character := string(rune(code))
		font.Normal[character] = builtinDeckFontGlyph(code, c64FullFont, 8)
		font.Bold[character] = builtinDeckFontGlyph(code, c64BoldFont, 10)
	}
	return font
}

func defaultEditableDeckCellFont() DeckFont {
	font := DeckFont{ID: "default", Name: "Keynope Default", Mode: deckFontModeCells, Normal: map[string][]string{}, Bold: map[string][]string{}}
	for code := 32; code <= 126; code++ {
		character := string(rune(code))
		font.Normal[character] = builtinDeckCellFontGlyph(code, c64FullFont, 8)
		font.Bold[character] = builtinDeckCellFontGlyph(code, c64BoldFont, 10)
	}
	return font
}

func removeDeckFontReferences(deck *Deck, id string) {
	if deck == nil || id == "" {
		return
	}
	updateSlide := func(slide *Slide) {
		for index := range slide.Elements {
			values, _ := url.ParseQuery(slide.Elements[index].Query)
			if normalizeDeckFontID(values.Get("font")) == id {
				values.Del("font")
				slide.Elements[index].Query = values.Encode()
			}
		}
	}
	for index := range deck.Slides {
		updateSlide(&deck.Slides[index])
	}
	updateSlide(&deck.Masters.Base.Slide)
	for index := range deck.Masters.Layouts {
		updateSlide(&deck.Masters.Layouts[index].Slide)
	}
}

func pruneUnusedDeckFonts(deck *Deck) {
	if deck == nil || len(deck.Fonts) == 0 {
		return
	}
	used := map[string]bool{}
	visitSlide := func(slide *Slide) {
		for _, element := range slide.Elements {
			values, _ := url.ParseQuery(element.Query)
			if id := normalizeDeckFontID(values.Get("font")); id != "" {
				used[id] = true
			}
		}
	}
	for index := range deck.Slides {
		visitSlide(&deck.Slides[index])
	}
	visitSlide(&deck.Masters.Base.Slide)
	for index := range deck.Masters.Layouts {
		visitSlide(&deck.Masters.Layouts[index].Slide)
	}
	for id := range deck.Fonts {
		if !used[id] {
			delete(deck.Fonts, id)
		}
	}
}

func decodeDeckFonts(text string) (map[string]DeckFont, string, error) {
	match := keynopeFontsRE.FindStringSubmatch(text)
	if match == nil {
		return nil, text, nil
	}
	compressed, err := base64.StdEncoding.DecodeString(match[1])
	if err != nil {
		return nil, text, fmt.Errorf("decode embedded fonts: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, text, fmt.Errorf("open embedded fonts: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(reader, 8<<20))
	closeErr := reader.Close()
	if err != nil || closeErr != nil {
		return nil, text, fmt.Errorf("read embedded fonts: %w", errors.Join(err, closeErr))
	}
	var encoded map[string]DeckFont
	if err := json.Unmarshal(data, &encoded); err != nil {
		return nil, text, fmt.Errorf("parse embedded fonts: %w", err)
	}
	fonts := make(map[string]DeckFont, len(encoded))
	for id, font := range encoded {
		if font.ID == "" {
			font.ID = id
		}
		normalized, err := normalizeDeckFont(font)
		if err != nil {
			return nil, text, fmt.Errorf("font %q: %w", id, err)
		}
		fonts[normalized.ID] = normalized
	}
	return fonts, keynopeFontsRE.ReplaceAllString(text, ""), nil
}

func encodeDeckFonts(fonts map[string]DeckFont) (string, error) {
	if len(fonts) == 0 {
		return "", nil
	}
	normalized := make(map[string]DeckFont, len(fonts))
	for id, font := range fonts {
		if font.ID == "" {
			font.ID = id
		}
		value, err := normalizeDeckFont(font)
		if err != nil {
			return "", fmt.Errorf("font %q: %w", id, err)
		}
		normalized[value.ID] = value
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return "<!-- keynope-fonts version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->", nil
}

func registerDeckFonts(fonts map[string]DeckFont) {
	available := builtinDeckFontLibrary()
	for id, font := range fonts {
		available[id] = font
	}
	compiled := make(map[string]compiledDeckFont, len(available))
	for _, font := range available {
		normalized, err := normalizeDeckFont(font)
		if err != nil {
			continue
		}
		compiled[normalized.ID] = compiledDeckFont{
			normal: compileDeckFontFace(normalized.Normal, normalized.Mode),
			bold:   compileDeckFontFace(normalized.Bold, normalized.Mode),
			mode:   normalized.Mode,
			layout: normalized.FigletLayout,
		}
	}
	deckFontRegistryMu.Lock()
	deckFontRegistry = compiled
	deckFontRegistryMu.Unlock()
}

func fontLibraryDirectory() string {
	if os.Getenv("APP_SANDBOX_CONTAINER_ID") != "" {
		if root, err := os.UserConfigDir(); err == nil && root != "" {
			return filepath.Join(root, "Keynope", "fonts")
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".keynope", "fonts")
	}
	return filepath.Join(".keynope", "fonts")
}

func storeDeckFontInLibrary(font DeckFont) error {
	if runtime.GOOS == "js" {
		return nil
	}
	normalized, err := normalizeDeckFont(font)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(fontLibraryDirectory(), 0o755); err != nil {
		return err
	}
	var content bytes.Buffer
	writer := gzip.NewWriter(&content)
	if err := json.NewEncoder(writer).Encode(normalized); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(fontLibraryDirectory(), normalized.ID+".json.gz"), content.Bytes(), 0o644)
}

func storeDeckFontsInLibrary(fonts map[string]DeckFont) {
	for _, font := range fonts {
		_ = storeDeckFontInLibrary(font)
	}
}

func loadDeckFontLibrary() map[string]DeckFont {
	fonts := builtinDeckFontLibrary()
	if runtime.GOOS == "js" {
		return fonts
	}
	entries, err := os.ReadDir(fontLibraryDirectory())
	if err != nil {
		return fonts
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json.gz") {
			continue
		}
		file, err := os.Open(filepath.Join(fontLibraryDirectory(), entry.Name()))
		if err != nil {
			continue
		}
		reader, err := gzip.NewReader(file)
		if err != nil {
			_ = file.Close()
			continue
		}
		var font DeckFont
		err = json.NewDecoder(io.LimitReader(reader, 2<<20)).Decode(&font)
		closeErr := reader.Close()
		_ = file.Close()
		if err != nil || closeErr != nil {
			continue
		}
		normalized, err := normalizeDeckFont(font)
		if err == nil {
			fonts[normalized.ID] = normalized
		}
	}
	return fonts
}

func removeDeckFontFromLibrary(id string) error {
	if runtime.GOOS == "js" {
		return nil
	}
	id = normalizeDeckFontID(id)
	if id == "" {
		return errors.New("font needs a unique ID")
	}
	err := os.Remove(filepath.Join(fontLibraryDirectory(), id+".json.gz"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func compileDeckFontFace(face map[string][]string, mode string) map[rune][]string {
	out := make(map[rune][]string, len(face))
	for character, rows := range face {
		r, _ := utf8.DecodeRuneInString(character)
		compiledRows := make([]string, len(rows))
		for row, line := range rows {
			compiledRows[row] = strings.Map(func(pixel rune) rune {
				if mode == deckFontModeFiglet {
					if isAllowedFigletCell(pixel) {
						return pixel
					}
					return ' '
				}
				if mode == deckFontModeCells {
					if pixel == '.' || pixel == ' ' {
						return ' '
					}
					if pixel == '#' {
						return '█'
					}
					if isDeckFontBlockCell(pixel) {
						return pixel
					}
					return ' '
				}
				if pixel == '#' || pixel == '█' {
					return '█'
				}
				return ' '
			}, line)
		}
		out[r] = compiledRows
	}
	return out
}

func elementDeckFont(element Element, bold bool) (map[rune][]string, bool) {
	face, _, ok := elementDeckFontDetails(element, bold)
	return face, ok
}

func elementDeckFontDetails(element Element, bold bool) (map[rune][]string, bool, bool) {
	values, _ := url.ParseQuery(element.Query)
	id := normalizeDeckFontID(values.Get("font"))
	if id == "" || id == "default" {
		return nil, false, false
	}
	deckFontRegistryMu.RLock()
	font, ok := deckFontRegistry[id]
	deckFontRegistryMu.RUnlock()
	if !ok {
		return nil, false, false
	}
	if bold {
		return font.bold, font.mode == deckFontModeCells, true
	}
	return font.normal, font.mode == deckFontModeCells, true
}

func elementDeckFontMode(element Element, bold bool) (map[rune][]string, string, bool) {
	face, mode, _, ok := elementDeckFontRenderDetails(element, bold)
	return face, mode, ok
}

func elementDeckFontRenderDetails(element Element, bold bool) (map[rune][]string, string, int, bool) {
	values, _ := url.ParseQuery(element.Query)
	id := normalizeDeckFontID(values.Get("font"))
	if id == "" || id == "default" {
		return nil, "", 0, false
	}
	deckFontRegistryMu.RLock()
	font, ok := deckFontRegistry[id]
	deckFontRegistryMu.RUnlock()
	if !ok {
		return nil, "", 0, false
	}
	if bold {
		return font.bold, font.mode, font.layout, true
	}
	return font.normal, font.mode, font.layout, true
}

func deckFontGlyphWidth(face map[rune][]string, character rune) int {
	rows := face[character]
	if rows == nil {
		rows = face['?']
	}
	width := 1
	for _, row := range rows {
		width = max(width, utf8.RuneCountInString(row))
	}
	return width
}

func deckFontMaxWidth(face map[rune][]string) int {
	width := 1
	for character := range face {
		width = max(width, deckFontGlyphWidth(face, character))
	}
	return width
}

func renderDeckFontRaw(text string, face map[rune][]string) []string {
	height := 0
	for _, glyph := range face {
		height = max(height, len(glyph))
	}
	rows := make([]string, max(1, height))
	for _, character := range text {
		glyph := face[character]
		if glyph == nil {
			glyph = face['?']
		}
		width := deckFontGlyphWidth(face, character)
		for row := range rows {
			line := ""
			if row < len(glyph) {
				line = glyph[row]
			}
			rows[row] += padRunes(line, width)
		}
	}
	return rows
}

func renderScaledDeckFigletFont(text string, factor float64, face map[rune][]string, layout int) []string {
	raw := renderDeckFigletRaw(text, face, layout)
	if len(raw) == 0 {
		return nil
	}
	factor = math.Max(0.25, factor)
	if math.Abs(factor-1) < 0.0001 {
		return trimBlank(raw)
	}
	sourceHeight, sourceWidth := len(raw), maxLineDisplayWidth(raw)
	if sourceWidth == 0 {
		return raw
	}
	source := make([][]rune, sourceHeight)
	for row := range raw {
		source[row] = []rune(padRunes(raw[row], sourceWidth))
	}
	targetHeight := max(1, int(math.Round(float64(sourceHeight)*factor)))
	targetWidth := max(1, int(math.Round(float64(sourceWidth)*factor)))
	out := make([]string, targetHeight)
	for y := range out {
		sourceY := min(sourceHeight-1, int(float64(y)/factor))
		var row strings.Builder
		for x := 0; x < targetWidth; x++ {
			sourceX := min(sourceWidth-1, int(float64(x)/factor))
			row.WriteRune(source[sourceY][sourceX])
		}
		out[y] = row.String()
	}
	return trimBlank(out)
}

func scaledDeckFontGlyphWidth(element Element, bold bool, scale float64) (int, bool) {
	font, mode, _, ok := elementDeckFontRenderDetails(element, bold)
	if !ok {
		return 0, false
	}
	width := float64(deckFontMaxWidth(font)) * scale
	if mode != deckFontModeFiglet {
		width /= 2
	}
	return max(1, int(math.Ceil(width))), true
}

func renderDeckFigletRaw(text string, face map[rune][]string, layout int) []string {
	height := 0
	for _, glyph := range face {
		height = max(height, len(glyph))
	}
	rows := make([]string, max(1, height))
	for _, character := range text {
		glyph := face[character]
		if glyph == nil {
			glyph = face['?']
		}
		width := deckFontGlyphWidth(face, character)
		padded := make([]string, len(rows))
		for row := range padded {
			if row < len(glyph) {
				padded[row] = padRunes(glyph[row], width)
			} else {
				padded[row] = strings.Repeat(" ", width)
			}
		}
		overlap := 0
		if layout&64 != 0 && len(strings.TrimSpace(strings.Join(rows, ""))) > 0 {
			overlap = width
			for row := range rows {
				trailing := utf8.RuneCountInString(rows[row]) - utf8.RuneCountInString(strings.TrimRight(rows[row], " "))
				leading := utf8.RuneCountInString(padded[row]) - utf8.RuneCountInString(strings.TrimLeft(padded[row], " "))
				overlap = min(overlap, trailing+leading)
			}
		}
		for row := range rows {
			current, incoming := []rune(rows[row]), []rune(padded[row])
			rowOverlap := min(overlap, len(current))
			start := len(current) - rowOverlap
			merged := make([]rune, max(len(current), start+len(incoming)))
			copy(merged, current)
			for index, cell := range incoming {
				target := start + index
				if target >= len(current) || cell != ' ' {
					merged[target] = cell
				}
			}
			rows[row] = string(merged)
		}
	}
	return rows
}

func deckFontBlockCellPattern(character rune) [64]bool {
	var pattern [64]bool
	set := func(x, y int) {
		if x >= 0 && x < 8 && y >= 0 && y < 8 {
			pattern[y*8+x] = true
		}
	}
	fill := func(x0, y0, x1, y1 int) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				set(x, y)
			}
		}
	}
	switch {
	case character == '█' || character == '#':
		fill(0, 0, 8, 8)
	case character == '▀':
		fill(0, 0, 8, 4)
	case character >= '▁' && character <= '▇':
		rows := int(character - '▁' + 1)
		fill(0, 8-rows, 8, 8)
	case character >= '▉' && character <= '▏':
		columns := 8 - int(character-'▉')
		fill(0, 0, columns, 8)
	case character == '▐':
		fill(4, 0, 8, 8)
	case character == '▔':
		fill(0, 0, 8, 1)
	case character == '▕':
		fill(7, 0, 8, 8)
	case character >= '░' && character <= '▓':
		threshold := map[rune]int{'░': 4, '▒': 8, '▓': 12}[character]
		bayer := [16]int{0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5}
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				if bayer[(y%4)*4+x%4] < threshold {
					set(x, y)
				}
			}
		}
	case character >= '▖' && character <= '▟':
		quadrants := map[rune][4]bool{
			'▖': {false, false, true, false},
			'▗': {false, false, false, true},
			'▘': {true, false, false, false},
			'▙': {true, false, true, true},
			'▚': {true, false, false, true},
			'▛': {true, true, true, false},
			'▜': {true, true, false, true},
			'▝': {false, true, false, false},
			'▞': {false, true, true, false},
			'▟': {false, true, true, true},
		}[character]
		if quadrants[0] {
			fill(0, 0, 4, 4)
		}
		if quadrants[1] {
			fill(4, 0, 8, 4)
		}
		if quadrants[2] {
			fill(0, 4, 4, 8)
		}
		if quadrants[3] {
			fill(4, 4, 8, 8)
		}
	}
	return pattern
}

func nearestDeckFontBlockCell(pattern [64]bool) rune {
	best, bestDistance := ' ', 65
	candidates := append([]rune{' '}, []rune("▀▁▂▃▄▅▆▇█▉▊▋▌▍▎▏▐░▒▓▔▕▖▗▘▙▚▛▜▝▞▟")...)
	for _, candidate := range candidates {
		value := deckFontBlockCellPattern(candidate)
		distance := 0
		for index := range pattern {
			if pattern[index] != value[index] {
				distance++
			}
		}
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best
}

func renderScaledDeckCellFont(text string, factor float64, face map[rune][]string) []string {
	raw := renderDeckFontRaw(text, face)
	if len(raw) == 0 {
		return nil
	}
	sourceHeight, sourceWidth := len(raw), maxLineDisplayWidth(raw)
	if sourceWidth == 0 {
		return raw
	}
	if math.Abs(factor-1) < 0.0001 {
		return raw
	}
	source := make([][]rune, sourceHeight)
	for row := range raw {
		source[row] = []rune(padRunes(raw[row], sourceWidth))
	}
	targetHeight := max(1, int(math.Round(float64(sourceHeight)*factor)))
	targetWidth := max(1, int(math.Round(float64(sourceWidth)*factor)))
	out := make([]string, targetHeight)
	for targetY := 0; targetY < targetHeight; targetY++ {
		var row strings.Builder
		for targetX := 0; targetX < targetWidth; targetX++ {
			var sampled [64]bool
			for microY := 0; microY < 8; microY++ {
				sourceMicroY := min(sourceHeight*8-1, (targetY*8+microY)*sourceHeight/targetHeight)
				sourceCellY, sourceCellMicroY := sourceMicroY/8, sourceMicroY%8
				for microX := 0; microX < 8; microX++ {
					sourceMicroX := min(sourceWidth*8-1, (targetX*8+microX)*sourceWidth/targetWidth)
					sourceCellX, sourceCellMicroX := sourceMicroX/8, sourceMicroX%8
					sourcePattern := deckFontBlockCellPattern(source[sourceCellY][sourceCellX])
					sampled[microY*8+microX] = sourcePattern[sourceCellMicroY*8+sourceCellMicroX]
				}
			}
			row.WriteRune(nearestDeckFontBlockCell(sampled))
		}
		out[targetY] = row.String()
	}
	return out
}

func deckFontTextMask(text string, face map[rune][]string) [][]bool {
	width := 0
	for _, character := range text {
		width += deckFontGlyphWidth(face, character)
	}
	rows := make([][]bool, 8)
	for row := range rows {
		rows[row] = make([]bool, 0, width)
	}
	for _, character := range text {
		glyph := face[character]
		if glyph == nil {
			glyph = face['?']
		}
		glyphWidth := deckFontGlyphWidth(face, character)
		for row := range rows {
			line := []rune("")
			if row < len(glyph) {
				line = []rune(padRunes(glyph[row], glyphWidth))
			}
			for column := 0; column < glyphWidth; column++ {
				rows[row] = append(rows[row], column < len(line) && line[column] != ' ')
			}
		}
	}
	return rows
}

func scaledDeckFontTextMask(text string, factor float64, face map[rune][]string) [][]bool {
	return scaleTextMask(deckFontTextMask(text, face), factor)
}

func scaleTextMask(mask [][]bool, factor float64) [][]bool {
	if len(mask) == 0 || len(mask[0]) == 0 || factor <= 0 {
		return nil
	}
	srcH, srcW := len(mask), len(mask[0])
	dstH := max(1, int(math.Round(float64(srcH)*factor)))
	dstW := max(1, int(math.Round(float64(srcW)*factor)))
	scaled := make([][]bool, dstH)
	for y := 0; y < dstH; y++ {
		sy := min(srcH-1, int(float64(y)/factor))
		scaled[y] = make([]bool, dstW)
		for x := 0; x < dstW; x++ {
			sx := min(srcW-1, int(float64(x)/factor))
			scaled[y][x] = mask[sy][sx]
		}
	}
	return scaled
}

func deckFontIDs(fonts map[string]DeckFont) []string {
	ids := make([]string, 0, len(fonts))
	for id := range fonts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := fonts[ids[i]].Name, fonts[ids[j]].Name
		if left == right {
			return ids[i] < ids[j]
		}
		return strings.ToLower(left) < strings.ToLower(right)
	})
	return ids
}
