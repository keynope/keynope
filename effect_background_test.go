package main

import (
	"strings"
	"testing"
)

func TestApplyANSIBackgroundKeepsEffectGlyphsOnSlideBackground(t *testing.T) {
	frame := " \033[0;32mA\033[4;7H \033[1;37mB"
	got := applyANSIBackground(frame, "44")
	for _, want := range []string{
		"\033[44m ",
		"\033[0;32;44mA",
		"\033[4;7H ",
		"\033[1;37;44mB",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("background-aware frame missing %q in %q", want, got)
		}
	}
}

func TestApplyANSIBackgroundSupportsTrueColourAndEmptyReset(t *testing.T) {
	background := "48;2;17;34;51"
	got := applyANSIBackground("\033[mX\033[2;4HY", background)
	if !strings.HasPrefix(got, "\033["+background+"m") {
		t.Fatalf("frame prefix = %q", got)
	}
	if !strings.Contains(got, "\033[0;"+background+"mX") {
		t.Fatalf("empty SGR reset did not retain true-colour background: %q", got)
	}
	if !strings.Contains(got, "\033[2;4HY") {
		t.Fatalf("cursor sequence was changed: %q", got)
	}
}

func TestEffectFrameUsesResolvedInheritedAndExplicitBackgrounds(t *testing.T) {
	masters := defaultMasterDeck()
	masters.Base.Slide.BG = "44"
	masters.Base.Slide.BGSet = true
	deck := Deck{Masters: masters, Slides: []Slide{
		{LayoutID: "blank"},
		{LayoutID: "blank", BG: "48;2;17;34;51", BGSet: true},
	}}

	for index, want := range []string{"44", "48;2;17;34;51"} {
		slide := deck.ResolveSlide(index, false)
		if got := slideBG(slide); got != want {
			t.Fatalf("resolved slide %d background = %q, want %q", index, got, want)
		}
		frame := captureTerminalOutput(func() {
			drawEffectFrame("scanline", 12, 6, 0, newMatrix(12, 6), newStars(12, 6), newBursts("scanline", 12, 6), slideBG(slide))
		})
		assertEverySGRHasBackground(t, frame, want)
	}
}

func TestEveryStaticBackgroundUsesResolvedSlideBackground(t *testing.T) {
	background := "48;2;17;34;51"
	for _, name := range availableBackgrounds {
		if name == "none" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			frame := captureTerminalOutput(func() {
				drawStaticBackground(name, 40, 20, background)
			})
			assertEverySGRHasBackground(t, frame, background)
		})
	}
}

func assertEverySGRHasBackground(t *testing.T, frame, background string) {
	t.Helper()
	count := 0
	for offset := 0; offset < len(frame); {
		start := strings.Index(frame[offset:], "\033[")
		if start < 0 {
			break
		}
		start += offset
		end := start + 2
		for end < len(frame) && (frame[end] < 0x40 || frame[end] > 0x7e) {
			end++
		}
		if end >= len(frame) {
			break
		}
		if frame[end] == 'm' {
			count++
			params := frame[start+2 : end]
			if params != background && !strings.HasSuffix(params, ";"+background) {
				t.Fatalf("SGR %q does not end with slide background %q in %q", params, background, frame)
			}
		}
		offset = end + 1
	}
	if count == 0 {
		t.Fatalf("effect frame contained no SGR sequences: %q", frame)
	}
}
