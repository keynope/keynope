package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/base64"
	"io"
	"sync"
)

// Every face is a lossless colour TrueType derivative of the bundled artwork.
// WOFF2 is only its transport compression. See assets/emoji/OFL.txt and NOTICE.
//
//go:embed assets/emoji/keynope-emoji-fonts.zip
var emojiFontArchive []byte

type exportEmojiRun struct {
	Start  int    `json:"start"`
	Length int    `json:"length"`
	Key    string `json:"key"`
	Text   string `json:"text"`
}

var emojiFonts = struct {
	sync.Once
	files map[string]*zip.File
	sync.Mutex
	encoded map[string]string
}{}

func emojiFontData(key string) string {
	emojiFonts.Do(func() {
		emojiFonts.files = map[string]*zip.File{}
		emojiFonts.encoded = map[string]string{}
		reader, err := zip.NewReader(bytes.NewReader(emojiFontArchive), int64(len(emojiFontArchive)))
		if err != nil {
			return
		}
		for _, file := range reader.File {
			emojiFonts.files[file.Name] = file
		}
	})
	emojiFonts.Lock()
	defer emojiFonts.Unlock()
	if value, ok := emojiFonts.encoded[key]; ok {
		return value
	}
	file := emojiFonts.files[key+".woff2"]
	if file == nil {
		return ""
	}
	r, err := file.Open()
	if err != nil {
		return ""
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, 2<<20))
	if err != nil {
		return ""
	}
	value := base64.StdEncoding.EncodeToString(data)
	emojiFonts.encoded[key] = value
	return value
}

func trueTypeEmojiData(text string) ([]exportEmojiRun, map[string]string) {
	var runs []exportEmojiRun
	var fonts map[string]string
	offset := 0
	for _, token := range splitEmojiText(text) {
		length := len([]rune(token.text))
		if token.assetKey != "" {
			if font := emojiFontData(token.assetKey); font != "" {
				if fonts == nil {
					fonts = map[string]string{}
				}
				fonts[token.assetKey] = font
				runs = append(runs, exportEmojiRun{Start: offset, Length: length, Key: token.assetKey, Text: token.text})
			}
		}
		offset += length
	}
	return runs, fonts
}

func exportTrueTypeElement(element Element, width, height int) *exportTrueType {
	runs, fonts := trueTypeEmojiData(element.Text)
	return &exportTrueType{Kind: element.Kind, Text: element.Text, Query: element.Query,
		Size: trueTypeSize(element), Width: width, Height: height, Emojis: runs, EmojiFonts: fonts, RichRuns: exportedRichRuns(element)}
}
