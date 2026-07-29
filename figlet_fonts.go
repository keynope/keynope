package main

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

func builtinDeckFontLibrary() map[string]DeckFont {
	return map[string]DeckFont{}
}

func parseFIGletFont(name, source string) (DeckFont, error) {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.TrimPrefix(source, "\ufeff")
	scanner := bufio.NewScanner(strings.NewReader(source))
	scanner.Buffer(make([]byte, 1024), 4<<20)
	if !scanner.Scan() {
		return DeckFont{}, errors.New("empty FIGlet font")
	}
	header := scanner.Text()
	fields := strings.Fields(header)
	if len(fields) < 6 || !strings.HasPrefix(fields[0], "flf2a") {
		return DeckFont{}, errors.New("not a FIGlet 2 font")
	}
	signature := []rune(fields[0])
	if len(signature) != 6 {
		return DeckFont{}, errors.New("invalid FIGlet signature")
	}
	hardblank := signature[5]
	height, err := strconv.Atoi(fields[1])
	if err != nil || height < 1 || height > deckFigletMaxGlyphHeight {
		return DeckFont{}, fmt.Errorf("invalid FIGlet height %q", fields[1])
	}
	commentLines, err := strconv.Atoi(fields[5])
	if err != nil || commentLines < 0 {
		return DeckFont{}, fmt.Errorf("invalid FIGlet comment count %q", fields[5])
	}
	layout := 0
	if len(fields) >= 8 {
		layout, _ = strconv.Atoi(fields[7])
	} else if len(fields) >= 5 {
		layout, _ = strconv.Atoi(fields[4])
	}
	for range commentLines {
		if !scanner.Scan() {
			return DeckFont{}, errors.New("FIGlet comments end before the glyph data")
		}
	}
	face := make(map[string][]string, 95)
	var endmark rune
	for code := 32; code <= 126; code++ {
		rows := make([]string, height)
		for row := 0; row < height; row++ {
			if !scanner.Scan() {
				return DeckFont{}, fmt.Errorf("FIGlet font ends inside glyph %q", rune(code))
			}
			line := scanner.Text()
			if code == 32 && row == 0 {
				runes := []rune(line)
				if len(runes) == 0 {
					return DeckFont{}, errors.New("FIGlet glyph data has no endmark")
				}
				endmark = runes[len(runes)-1]
			}
			line, err = stripFIGletEndmark(line, endmark)
			if err != nil {
				return DeckFont{}, fmt.Errorf("glyph %q row %d: %w", rune(code), row+1, err)
			}
			line = strings.Map(func(character rune) rune {
				if character == hardblank {
					return ' '
				}
				return character
			}, line)
			rows[row] = line
		}
		face[string(rune(code))] = rows
	}
	font := DeckFont{
		ID:           normalizeDeckFontID(name),
		Name:         strings.TrimSpace(name),
		Mode:         deckFontModeFiglet,
		Height:       height,
		FigletLayout: layout,
		Normal:       face,
		Bold:         cloneDeckFontFace(face),
	}
	if font.Name == "" {
		font.Name = "Imported FIGlet"
		font.ID = "imported-figlet"
	}
	return normalizeDeckFont(font)
}

func stripFIGletEndmark(line string, endmark rune) (string, error) {
	if endmark == utf8.RuneError {
		return "", errors.New("invalid endmark")
	}
	runes := []rune(line)
	if len(runes) == 0 || runes[len(runes)-1] != endmark {
		return "", errors.New("missing endmark")
	}
	for len(runes) > 0 && runes[len(runes)-1] == endmark {
		runes = runes[:len(runes)-1]
	}
	return string(runes), nil
}
