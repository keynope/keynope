package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// Keynope Emoji Glyphs are modified derivatives of Noto Emoji and are
// distributed under the SIL Open Font License 1.1 stored at
// assets/emoji/OFL.txt. See assets/emoji/NOTICE.txt for provenance.
//
//go:embed assets/emoji/keynope-emoji-glyphs.bin.gz
var storedEmojiGlyphArchive []byte

//go:embed assets/emoji/emoji-test.txt
var unicodeEmojiTestData string

//go:embed LICENSE.txt
var keynopeLicenseText string

//go:embed assets/emoji/NOTICE.txt
var emojiNoticeText string

//go:embed assets/emoji/OFL.txt
var emojiOFLText string

//go:embed assets/emoji/NOTO-REGION-FLAGS-LICENSE.txt
var emojiRegionFlagsLicenseText string

//go:embed assets/emoji/UNICODE-LICENSE.txt
var unicodeLicenseText string

type emojiCatalogEntry struct {
	Emoji    string `json:"emoji"`
	Name     string `json:"name"`
	Group    string `json:"group"`
	Subgroup string `json:"subgroup"`
	assetKey string
}

type emojiTrieNode struct {
	children map[rune]*emojiTrieNode
	assetKey string
}

type emojiTextToken struct {
	text     string
	assetKey string
}

type textVisualUnit struct {
	text  string
	width int
	emoji bool
}

var emojiData = struct {
	sync.Once
	assets  map[string][]byte
	catalog []emojiCatalogEntry
	trie    *emojiTrieNode
}{}

const (
	storedEmojiGlyphWidth  = 80
	storedEmojiGlyphHeight = 40
	storedEmojiCellBytes   = 4
)

var emojiGlyphCache = struct {
	sync.RWMutex
	rows map[string][]string
}{rows: map[string][]string{}}

func bundledLicenseText() string {
	sections := []struct {
		title string
		text  string
	}{
		{title: "KEYNOPE", text: keynopeLicenseText},
		{title: "KEYNOPE EMOJI GLYPHS NOTICE", text: emojiNoticeText},
		{title: "SIL OPEN FONT LICENSE 1.1", text: emojiOFLText},
		{title: "NOTO REGIONAL FLAGS", text: emojiRegionFlagsLicenseText},
		{title: "UNICODE DATA FILES", text: unicodeLicenseText},
	}
	var out strings.Builder
	for index, section := range sections {
		if index > 0 {
			out.WriteString("\n")
		}
		out.WriteString("===== " + section.title + " =====\n\n")
		out.WriteString(strings.TrimSpace(section.text))
		out.WriteString("\n")
	}
	return out.String()
}

func ensureEmojiData() {
	emojiData.Do(func() {
		emojiData.assets = decodeStoredEmojiGlyphs(storedEmojiGlyphArchive)
		emojiData.trie = &emojiTrieNode{children: map[rune]*emojiTrieNode{}}

		group, subgroup := "Other", "Other"
		seenPicker := map[string]bool{}
		for _, rawLine := range strings.Split(unicodeEmojiTestData, "\n") {
			line := strings.TrimSpace(rawLine)
			switch {
			case strings.HasPrefix(line, "# group:"):
				group = strings.TrimSpace(strings.TrimPrefix(line, "# group:"))
				continue
			case strings.HasPrefix(line, "# subgroup:"):
				subgroup = strings.TrimSpace(strings.TrimPrefix(line, "# subgroup:"))
				continue
			case line == "" || strings.HasPrefix(line, "#"):
				continue
			}
			fields := strings.SplitN(line, "#", 2)
			if len(fields) != 2 {
				continue
			}
			left := strings.SplitN(fields[0], ";", 2)
			if len(left) != 2 {
				continue
			}
			sequence := emojiSequenceFromHexFields(strings.Fields(strings.TrimSpace(left[0])))
			if sequence == "" {
				continue
			}
			assetKey := emojiAssetKey(sequence)
			if emojiData.assets[assetKey] == nil {
				continue
			}
			addEmojiTrieSequence(sequence, assetKey)
			withoutVariation := strings.ReplaceAll(sequence, "\ufe0f", "")
			if withoutVariation != sequence {
				addEmojiTrieSequence(withoutVariation, assetKey)
			}
			status := strings.TrimSpace(left[1])
			if status != "fully-qualified" || seenPicker[assetKey] {
				continue
			}
			comment := strings.Fields(strings.TrimSpace(fields[1]))
			nameAt := 0
			for nameAt < len(comment) && !strings.HasPrefix(comment[nameAt], "E") {
				nameAt++
			}
			if nameAt < len(comment) {
				nameAt++
			}
			name := strings.Join(comment[nameAt:], " ")
			if name == "" {
				name = fmt.Sprintf("Emoji %s", assetKey)
			}
			emojiData.catalog = append(emojiData.catalog, emojiCatalogEntry{Emoji: sequence, Name: name, Group: group, Subgroup: subgroup, assetKey: assetKey})
			seenPicker[assetKey] = true
		}
	})
}

func decodeStoredEmojiGlyphs(data []byte) map[string][]byte {
	assets := map[string][]byte{}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return assets
	}
	defer reader.Close()
	magic := make([]byte, 5)
	if _, err := io.ReadFull(reader, magic); err != nil || string(magic) != "KNEG1" {
		return assets
	}
	var width, height uint16
	var count uint32
	if binary.Read(reader, binary.BigEndian, &width) != nil || binary.Read(reader, binary.BigEndian, &height) != nil || binary.Read(reader, binary.BigEndian, &count) != nil || width != storedEmojiGlyphWidth || height != storedEmojiGlyphHeight {
		return assets
	}
	cellCount := int(width) * int(height) * storedEmojiCellBytes
	for range count {
		var keyLength uint16
		if binary.Read(reader, binary.BigEndian, &keyLength) != nil || keyLength == 0 {
			return map[string][]byte{}
		}
		key, cells := make([]byte, keyLength), make([]byte, cellCount)
		if _, err := io.ReadFull(reader, key); err != nil {
			return map[string][]byte{}
		}
		if _, err := io.ReadFull(reader, cells); err != nil {
			return map[string][]byte{}
		}
		assets[string(key)] = cells
	}
	return assets
}

func emojiFlagAssetKey(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	if len(name) == 2 && name[0] >= 'A' && name[0] <= 'Z' && name[1] >= 'A' && name[1] <= 'Z' {
		first := rune(0x1f1e6 + int(name[0]-'A'))
		second := rune(0x1f1e6 + int(name[1]-'A'))
		return emojiAssetKey(string([]rune{first, second}))
	}
	tag := ""
	switch name {
	case "GB-ENG":
		tag = "gbeng"
	case "GB-SCT":
		tag = "gbsct"
	case "GB-WLS":
		tag = "gbwls"
	}
	if tag == "" {
		return ""
	}
	sequence := []rune{0x1f3f4}
	for _, r := range tag {
		sequence = append(sequence, 0xe0000+r)
	}
	sequence = append(sequence, 0xe007f)
	return emojiAssetKey(string(sequence))
}

func emojiSequenceFromHexFields(fields []string) string {
	var out strings.Builder
	for _, field := range fields {
		value, err := strconv.ParseInt(field, 16, 32)
		if err != nil || value <= 0 {
			return ""
		}
		out.WriteRune(rune(value))
	}
	return out.String()
}

func emojiAssetKey(sequence string) string {
	parts := make([]string, 0, len([]rune(sequence)))
	for _, r := range sequence {
		if r == '\ufe0f' {
			continue
		}
		parts = append(parts, fmt.Sprintf("%04x", r))
	}
	return strings.Join(parts, "_")
}

func addEmojiTrieSequence(sequence, assetKey string) {
	node := emojiData.trie
	for _, r := range sequence {
		if node.children == nil {
			node.children = map[rune]*emojiTrieNode{}
		}
		if node.children[r] == nil {
			node.children[r] = &emojiTrieNode{children: map[rune]*emojiTrieNode{}}
		}
		node = node.children[r]
	}
	node.assetKey = assetKey
}

func splitEmojiText(text string) []emojiTextToken {
	ensureEmojiData()
	runes := []rune(text)
	var tokens []emojiTextToken
	var plain strings.Builder
	flushPlain := func() {
		if plain.Len() > 0 {
			tokens = append(tokens, emojiTextToken{text: plain.String()})
			plain.Reset()
		}
	}
	for index := 0; index < len(runes); {
		node := emojiData.trie
		matchedEnd, matchedKey := -1, ""
		for cursor := index; cursor < len(runes); cursor++ {
			node = node.children[runes[cursor]]
			if node == nil {
				break
			}
			if node.assetKey != "" {
				matchedEnd, matchedKey = cursor+1, node.assetKey
			}
		}
		if matchedEnd > index {
			flushPlain()
			tokens = append(tokens, emojiTextToken{text: string(runes[index:matchedEnd]), assetKey: matchedKey})
			index = matchedEnd
			continue
		}
		plain.WriteRune(runes[index])
		index++
	}
	flushPlain()
	return tokens
}

func hasEmoji(text string) bool {
	for _, token := range splitEmojiText(text) {
		if token.assetKey != "" {
			return true
		}
	}
	return false
}

func emojiStartsAtRuneCursor(text string, cursor int) bool {
	_, ok := emojiRuneLengthAtCursor(text, cursor)
	return ok
}

func emojiRuneLengthAtCursor(text string, cursor int) (int, bool) {
	cursor = max(0, cursor)
	position := 0
	for _, token := range splitEmojiText(text) {
		if position == cursor {
			if token.assetKey == "" {
				return 0, false
			}
			return len([]rune(token.text)), true
		}
		position += len([]rune(token.text))
		if position > cursor {
			return 0, false
		}
	}
	return 0, false
}

func textVisualUnits(text string) []textVisualUnit {
	var units []textVisualUnit
	for _, token := range splitEmojiText(text) {
		if token.assetKey != "" {
			units = append(units, textVisualUnit{text: token.text, width: 2, emoji: true})
			continue
		}
		for _, r := range token.text {
			units = append(units, textVisualUnit{text: string(r), width: 1})
		}
	}
	return units
}

func textVisualUnitWidth(text string) int {
	total := 0
	for _, unit := range textVisualUnits(text) {
		total += unit.width
	}
	return total
}

func textSpacedVisualUnitWidth(text string) int {
	total := 0
	for _, unit := range textVisualUnits(text) {
		total += unit.width
		if unit.emoji {
			total++
		}
	}
	return total
}

func splitTextVisualUnits(text string, width int) []string {
	width = max(1, width)
	units := textVisualUnits(text)
	if len(units) == 0 {
		return []string{""}
	}
	var chunks []string
	var chunk strings.Builder
	used := 0
	for _, unit := range units {
		unitWidth := unit.width
		if unit.emoji {
			unitWidth++
		}
		if used > 0 && used+unitWidth > width {
			chunks = append(chunks, chunk.String())
			chunk.Reset()
			used = 0
		}
		chunk.WriteString(unit.text)
		used += unitWidth
	}
	if chunk.Len() > 0 {
		chunks = append(chunks, chunk.String())
	}
	return chunks
}

func renderEmojiGlyph(assetKey string, height int) []string {
	ensureEmojiData()
	height = max(1, height)
	cacheKey := assetKey + ":" + strconv.Itoa(height)
	emojiGlyphCache.RLock()
	rows := emojiGlyphCache.rows[cacheKey]
	emojiGlyphCache.RUnlock()
	if rows != nil {
		return append([]string(nil), rows...)
	}
	cells := emojiData.assets[assetKey]
	if cells == nil {
		return nil
	}
	rows = resizeStoredEmojiGlyph(cells, height)
	emojiGlyphCache.Lock()
	emojiGlyphCache.rows[cacheKey] = append([]string(nil), rows...)
	emojiGlyphCache.Unlock()
	return rows
}

func resizeStoredEmojiGlyph(cells []byte, height int) []string {
	height = max(1, min(storedEmojiGlyphHeight, height))
	width := height * 2
	rows := make([]string, height)
	for targetY := 0; targetY < height; targetY++ {
		var row strings.Builder
		for targetX := 0; targetX < width; targetX++ {
			mask, red, green, blue, colours := byte(0), 0, 0, 0, 0
			for quadrantY := 0; quadrantY < 2; quadrantY++ {
				for quadrantX := 0; quadrantX < 2; quadrantX++ {
					targetSubX, targetSubY := targetX*2+quadrantX, targetY*2+quadrantY
					sourceStartX := targetSubX * (storedEmojiGlyphWidth * 2) / (width * 2)
					sourceEndX := max(sourceStartX+1, ((targetSubX+1)*(storedEmojiGlyphWidth*2)+(width*2)-1)/(width*2))
					sourceStartY := targetSubY * (storedEmojiGlyphHeight * 2) / (height * 2)
					sourceEndY := max(sourceStartY+1, ((targetSubY+1)*(storedEmojiGlyphHeight*2)+(height*2)-1)/(height*2))
					quadrantRed, quadrantGreen, quadrantBlue, quadrantColours := 0, 0, 0, 0
					for sourceSubY := sourceStartY; sourceSubY < sourceEndY; sourceSubY++ {
						for sourceSubX := sourceStartX; sourceSubX < sourceEndX; sourceSubX++ {
							sourceX, sourceY := sourceSubX/2, sourceSubY/2
							sourceBit := byte(1 << (sourceSubY%2*2 + sourceSubX%2))
							at := (sourceY*storedEmojiGlyphWidth + sourceX) * storedEmojiCellBytes
							if at+3 >= len(cells) || cells[at]&sourceBit == 0 {
								continue
							}
							quadrantRed += int(cells[at+1])
							quadrantGreen += int(cells[at+2])
							quadrantBlue += int(cells[at+3])
							quadrantColours++
						}
					}
					if quadrantColours == 0 {
						continue
					}
					mask |= byte(1 << (quadrantY*2 + quadrantX))
					red += quadrantRed / quadrantColours
					green += quadrantGreen / quadrantColours
					blue += quadrantBlue / quadrantColours
					colours++
				}
			}
			if mask == 0 || colours == 0 {
				row.WriteByte(' ')
				continue
			}
			char := quadrantRune(mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0)
			writeRGBSequence(&row, "38", uint8(red/colours), uint8(green/colours), uint8(blue/colours), char)
		}
		row.WriteString("\033[0m")
		rows[targetY] = row.String()
	}
	return rows
}

func renderTextWithEmoji(text string, height int, renderPlain func(string) []string) []string {
	tokens := splitEmojiText(text)
	containsEmoji := false
	for _, token := range tokens {
		containsEmoji = containsEmoji || token.assetKey != ""
	}
	if !containsEmoji {
		return renderPlain(text)
	}
	height = max(1, height)
	rows := make([]string, height)
	plainWidth := max(1, maxLineDisplayWidth(renderPlain("M")))
	for _, token := range tokens {
		chunk := renderPlain(token.text)
		if token.assetKey != "" {
			chunk = renderEmojiGlyph(token.assetKey, height)
		}
		chunkWidth := max(1, maxLineDisplayWidth(chunk))
		leftPad, rightPad := 0, 0
		if token.assetKey != "" {
			innerWidth := max(chunkWidth, plainWidth*2)
			leftPad = 1 + (innerWidth-chunkWidth)/2
			rightPad = 1 + innerWidth - chunkWidth - (innerWidth-chunkWidth)/2
			chunkWidth += leftPad + rightPad
		}
		offset := max(0, height-len(chunk))
		for row := 0; row < height; row++ {
			if row >= offset && row-offset < len(chunk) {
				value := chunk[row-offset]
				rows[row] += strings.Repeat(" ", leftPad) + value + strings.Repeat(" ", max(0, chunkWidth-leftPad-displayWidth(stripANSI(value))))
			} else {
				rows[row] += strings.Repeat(" ", chunkWidth)
			}
		}
	}
	return rows
}

func emojiCatalogSearch(query, group string, offset, limit int) []emojiCatalogEntry {
	ensureEmojiData()
	query, group = strings.ToLower(strings.TrimSpace(query)), strings.TrimSpace(group)
	if offset < 0 {
		offset = 0
	}
	limit = max(1, min(100, limit))
	results := make([]emojiCatalogEntry, 0, limit)
	matched := 0
	for _, entry := range emojiData.catalog {
		if group != "" && entry.Group != group {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Name+" "+entry.Group+" "+entry.Subgroup), query) {
			continue
		}
		if matched < offset {
			matched++
			continue
		}
		results = append(results, entry)
		if len(results) >= limit {
			break
		}
	}
	return results
}

func emojiCatalogGroups() []string {
	ensureEmojiData()
	seen := map[string]bool{}
	groups := make([]string, 0, 12)
	for _, entry := range emojiData.catalog {
		if entry.Group != "" && !seen[entry.Group] {
			seen[entry.Group] = true
			groups = append(groups, entry.Group)
		}
	}
	return groups
}
