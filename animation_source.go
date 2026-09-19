package main

import (
	"encoding/binary"
	"fmt"
)

// Count GIF frames before DecodeAll allocates paletted images or the compositor
// allocates full canvases. Compressed input size alone does not bound either.
func preflightGIFFrames(data []byte) error {
	if len(data) < 13 {
		return fmt.Errorf("truncated GIF header")
	}
	width, height := int64(binary.LittleEndian.Uint16(data[6:8])), int64(binary.LittleEndian.Uint16(data[8:10]))
	pos := 13
	skip := func(n int) bool {
		if n < 0 || n > len(data)-pos {
			return false
		}
		pos += n
		return true
	}
	if data[10]&128 != 0 && !skip(3*(1<<((data[10]&7)+1))) {
		return fmt.Errorf("truncated GIF palette")
	}
	blocks := func() bool {
		for pos < len(data) {
			size := int(data[pos])
			pos++
			if size == 0 {
				return true
			}
			if !skip(size) {
				return false
			}
		}
		return false
	}
	frames := int64(0)
	for pos < len(data) {
		tag := data[pos]
		pos++
		switch tag {
		case 0x3b:
			return nil
		case 0x21:
			if !skip(1) || !blocks() {
				return fmt.Errorf("truncated GIF extension")
			}
		case 0x2c:
			if len(data)-pos < 9 {
				return fmt.Errorf("truncated GIF frame")
			}
			flags := data[pos+8]
			pos += 9
			frames++
			if width*height*frames > 32_000_000 || frames > 10000 {
				return fmt.Errorf("animation exceeds the decoded frame limit")
			}
			if flags&128 != 0 && !skip(3*(1<<((flags&7)+1))) {
				return fmt.Errorf("truncated GIF frame palette")
			}
			if !skip(1) || !blocks() {
				return fmt.Errorf("truncated GIF pixels")
			}
		default:
			return fmt.Errorf("invalid GIF block")
		}
	}
	return fmt.Errorf("missing GIF trailer")
}
