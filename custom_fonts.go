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

var keynopeFontsRE = regexp.MustCompile(`<!--\s*keynope-fonts\s+version=1\s+base64:([A-Za-z0-9+/=]+)\s*-->\s*`)

var (
	deckFontRegistryMu sync.RWMutex
	deckFontRegistry   = map[string]compiledDeckFont{}
)

type compiledDeckFont struct {
	normal map[rune][]string
	bold   map[rune][]string
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
	normal, err := normalizeDeckFontFace(font.Normal, c64FullFont, 8)
	if err != nil {
		return DeckFont{}, fmt.Errorf("normal face: %w", err)
	}
	bold, err := normalizeDeckFontFace(font.Bold, c64BoldFont, 10)
	if err != nil {
		return DeckFont{}, fmt.Errorf("bold face: %w", err)
	}
	font.Normal, font.Bold = normal, bold
	return font, nil
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

func normalizeDeckFontFace(face map[string][]string, fallback map[rune][]string, fallbackWidth int) (map[string][]string, error) {
	out := make(map[string][]string, 95)
	for code := 32; code <= 126; code++ {
		character := string(rune(code))
		rows := face[character]
		if len(rows) == 0 {
			rows = builtinDeckFontGlyph(code, fallback, fallbackWidth)
		}
		if len(rows) != 8 {
			return nil, fmt.Errorf("%q must contain 8 rows", character)
		}
		width := 1
		for _, row := range rows {
			width = max(width, utf8.RuneCountInString(row))
		}
		if width > deckFontMaxGlyphWidth {
			return nil, fmt.Errorf("%q is %d pixels wide; maximum is %d", character, width, deckFontMaxGlyphWidth)
		}
		normalized := make([]string, 8)
		for rowIndex, row := range rows {
			var line strings.Builder
			for _, pixel := range row {
				if pixel == '#' || pixel == '█' {
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

func defaultEditableDeckFont() DeckFont {
	font := DeckFont{ID: "default", Name: "Keynope Default", Normal: map[string][]string{}, Bold: map[string][]string{}}
	for code := 32; code <= 126; code++ {
		character := string(rune(code))
		font.Normal[character] = builtinDeckFontGlyph(code, c64FullFont, 8)
		font.Bold[character] = builtinDeckFontGlyph(code, c64BoldFont, 10)
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
	compiled := make(map[string]compiledDeckFont, len(fonts))
	for _, font := range fonts {
		normalized, err := normalizeDeckFont(font)
		if err != nil {
			continue
		}
		compiled[normalized.ID] = compiledDeckFont{
			normal: compileDeckFontFace(normalized.Normal),
			bold:   compileDeckFontFace(normalized.Bold),
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
	fonts := map[string]DeckFont{}
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

func compileDeckFontFace(face map[string][]string) map[rune][]string {
	out := make(map[rune][]string, len(face))
	for character, rows := range face {
		r, _ := utf8.DecodeRuneInString(character)
		compiledRows := make([]string, len(rows))
		for row, line := range rows {
			compiledRows[row] = strings.Map(func(pixel rune) rune {
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
	values, _ := url.ParseQuery(element.Query)
	id := normalizeDeckFontID(values.Get("font"))
	if id == "" || id == "default" {
		return nil, false
	}
	deckFontRegistryMu.RLock()
	font, ok := deckFontRegistry[id]
	deckFontRegistryMu.RUnlock()
	if !ok {
		return nil, false
	}
	if bold {
		return font.bold, true
	}
	return font.normal, true
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
	rows := make([]string, 8)
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
