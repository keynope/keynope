package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	qrcode "github.com/skip2/go-qrcode"
)

// QRCodeOptions is shared by the Insert dialog, native commands and MCP.
// QR artwork is embedded, never fetched from a third-party generation service.
type QRCodeOptions struct {
	Content    string `json:"content"`
	Correction string `json:"correction,omitempty"` // L, M (default), Q or H.
	Foreground string `json:"foreground,omitempty"`
	Background string `json:"background,omitempty"`
}

func qrColour(value, fallback string) (string, float64, error) {
	if value == "" {
		value = fallback
	}
	if len(value) != 7 || value[0] != '#' {
		return "", 0, errors.New("QR colours must use #RRGGBB")
	}
	n, err := strconv.ParseUint(value[1:], 16, 24)
	if err != nil {
		return "", 0, errors.New("QR colours must use #RRGGBB")
	}
	linear := func(c uint64) float64 {
		v := float64(c&255) / 255
		if v <= .04045 {
			return v / 12.92
		}
		return math.Pow((v+.055)/1.055, 2.4)
	}
	return strings.ToLower(value), .2126*linear(n>>16) + .7152*linear(n>>8) + .0722*linear(n), nil
}

func qrCodeSVG(options QRCodeOptions) (string, int, error) {
	if strings.TrimSpace(options.Content) == "" || !utf8.ValidString(options.Content) {
		return "", 0, errors.New("enter a URL or text for the QR code")
	}
	if len(options.Content) > 2953 {
		return "", 0, errors.New("QR content is too long (maximum 2,953 UTF-8 bytes; higher correction levels hold less)")
	}
	level := qrcode.Medium
	switch strings.ToUpper(options.Correction) {
	case "", "M":
	case "L":
		level = qrcode.Low
	case "Q":
		level = qrcode.High
	case "H":
		level = qrcode.Highest
	default:
		return "", 0, errors.New("QR error correction must be L, M, Q or H")
	}
	fg, dark, err := qrColour(options.Foreground, "#000000")
	if err != nil {
		return "", 0, err
	}
	bg, light, err := qrColour(options.Background, "#ffffff")
	if err != nil {
		return "", 0, err
	}
	if (light+.05)/(dark+.05) < 4.5 {
		return "", 0, errors.New("use dark modules on a light background with stronger contrast for reliable scanning")
	}
	code, err := qrcode.New(options.Content, level)
	if err != nil {
		return "", 0, fmt.Errorf("content does not fit at this correction level; shorten it or choose a lower level: %w", err)
	}
	bitmap := code.Bitmap() // Includes the standard four-module quiet zone.
	n := len(bitmap)
	var out strings.Builder
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d"><rect width="%d" height="%d" fill="%s"/>`, n*8, n*8, n, n, n, n, bg)
	// Horizontal runs keep the embedded artwork small without smoothing modules.
	for y, row := range bitmap {
		for x := 0; x < n; x++ {
			if !row[x] {
				continue
			}
			start := x
			for x+1 < n && row[x+1] {
				x++
			}
			fmt.Fprintf(&out, `<rect x="%d" y="%d" width="%d" height="1" fill="%s"/>`, start, y, x-start+1, fg)
		}
	}
	out.WriteString(`</svg>`)
	return out.String(), n, nil
}

func prepareQRCodeAction(ctx context.Context, action nativeEditorAction) (nativeEditorAction, error) {
	if action.QRCode == nil {
		return action, errors.New("qrCode options are required")
	}
	svg, _, err := qrCodeSVG(*action.QRCode)
	if err != nil {
		return action, err
	}
	id, asset, path, err := embeddedImageAssetContext(ctx, []byte(svg))
	if err != nil {
		return action, err
	}
	q := url.Values{"image-style": {"modern"}}
	if link, err := url.Parse(action.QRCode.Content); err == nil && (link.Scheme == "https" || link.Scheme == "http") && link.Host != "" {
		q.Set("link", link.String())
	}
	action.Action, action.Kind = "add-element", "image"
	action.ElementData = &Element{Kind: "image", AssetID: id, Path: path, Query: q.Encode()}
	action.AssetData, action.preparedQRCode = &asset, true
	action.uploadContext = ctx // Recheck the slide/master and revision after encoding.
	return action, nil
}

func (deck Deck) placeInsertedQRCode(slide *Slide, index int) {
	// Grid cells aren't square. Size in physical pixels first, then convert each
	// axis independently; this also works for portrait and custom-sized decks.
	w, h := deck.pageDimensions()
	side := math.Min(w, h) * .42
	q, _ := url.ParseQuery(slide.Elements[index].Query)
	q.Del("scale")
	q.Set("image-fit", "contain")
	q.Set("width", strconv.FormatFloat(side/w*float64(authoredTerminalWidth), 'f', -1, 64))
	q.Set("height", strconv.FormatFloat(side/h*float64(authoredTerminalHeight), 'f', -1, 64))
	for _, axis := range []struct {
		key, offset string
		position    float64
	}{
		{"left", "x", (w - side) / 2 / w * float64(authoredTerminalWidth)},
		{"top", "y", (h - side) / 2 / h * float64(authoredTerminalHeight)},
	} {
		whole := math.Floor(axis.position)
		q.Set(axis.key, strconv.FormatFloat(whole, 'f', -1, 64))
		q.Set("object-offset-"+axis.offset, strconv.FormatFloat(axis.position-whole, 'f', -1, 64))
	}
	slide.Elements[index].Query = q.Encode()
}

func (s *nativeEditorSession) handleQRCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var options QRCodeOptions
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&options); err != nil {
		http.Error(w, "Invalid QR code options", http.StatusBadRequest)
		return
	}
	svg, modules, err := qrCodeSVG(options)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"svg": svg, "modules": modules})
}
