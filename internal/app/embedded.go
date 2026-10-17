package app

import resources "keynope"

var embeddedFiles = resources.Resources

func embeddedBytes(name string) []byte {
	switch name {
	case "assets/emoji/keynope-emoji-fonts.zip":
		return resources.EmojiFontArchive
	case "assets/emoji/keynope-emoji-glyphs.bin.gz":
		return resources.EmojiGlyphArchive
	}
	data, err := embeddedFiles.ReadFile(name)
	if err != nil {
		panic("missing bundled Keynope resource: " + name + ": " + err.Error())
	}
	return data
}

func embeddedString(name string) string {
	return string(embeddedBytes(name))
}
