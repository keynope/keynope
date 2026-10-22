package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
)

// The largest possible movie is bounded by the deck's existing asset archive,
// not an independent video/broadcast limit. Base64 needs four bytes per three
// source bytes; the final admission check also charges poster/metadata/assets.
const maxEmbeddedVideoBytes = maxDeckAssetArchiveBytes / 4 * 3

const videoCapacityMessage = "This video will not fit in the deck's embedded-media capacity (100 MiB total, including encoding). Remove embedded media or choose a smaller clip."

// The still image stays in the normal asset fields for editing, thumbnails and
// static exports. Original movie bytes are immutable, embedded, never fetched.
type DeckAssetVideo struct {
	MIME       string `json:"mime"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMS int64  `json:"durationMs"`
	Data       []byte `json:"data"`
}

type sceneVideo struct {
	Source   string `json:"source"`
	Autoplay bool   `json:"autoplay"`
	Loop     bool   `json:"loop"`
	Muted    bool   `json:"muted"`
}

type movieAtom struct {
	kind string
	data []byte
}

// ISO BMFF/QuickTime atoms are length-delimited. Never scan arbitrary payload
// bytes for signatures, follow external references, or invoke a media decoder.
func movieAtoms(data []byte) ([]movieAtom, error) {
	var atoms []movieAtom
	for len(data) > 0 {
		if len(data) < 8 || len(atoms) >= 8192 {
			return nil, fmt.Errorf("invalid or excessive movie atoms")
		}
		size, header := uint64(binary.BigEndian.Uint32(data)), 8
		if size == 1 {
			if len(data) < 16 {
				return nil, fmt.Errorf("truncated movie atom")
			}
			size = binary.BigEndian.Uint64(data[8:])
			header = 16
		}
		if size == 0 {
			size = uint64(len(data))
		}
		if size < uint64(header) || size > uint64(len(data)) {
			return nil, fmt.Errorf("truncated movie atom")
		}
		atoms = append(atoms, movieAtom{string(data[4:8]), data[header:int(size)]})
		data = data[int(size):]
	}
	return atoms, nil
}

func movieChild(atoms []movieAtom, kind string) []byte {
	for _, a := range atoms {
		if a.kind == kind {
			return a.data
		}
	}
	return nil
}

func inspectMovie(data []byte) (DeckAssetVideo, error) {
	fail := func() (DeckAssetVideo, error) {
		return DeckAssetVideo{}, fmt.Errorf("Use a self-contained MP4 or MOV video (H.264/AAC recommended)")
	}
	if len(data) > maxEmbeddedVideoBytes {
		return DeckAssetVideo{}, fmt.Errorf("%s", videoCapacityMessage)
	}
	if len(data) < 32 {
		return fail()
	}
	atoms, err := movieAtoms(data)
	if err != nil {
		return DeckAssetVideo{}, err
	}
	ftyp := movieChild(atoms, "ftyp")
	if len(ftyp) < 8 || len(movieChild(atoms, "mdat")) == 0 {
		return fail()
	}
	mime := "video/mp4"
	if string(ftyp[:4]) == "qt  " {
		mime = "video/quicktime"
	}
	moov, err := movieAtoms(movieChild(atoms, "moov"))
	if err != nil {
		return DeckAssetVideo{}, err
	}
	mvhd := movieChild(moov, "mvhd")
	if len(mvhd) < 20 {
		return fail()
	}
	var scale uint32
	var duration uint64
	if mvhd[0] == 0 {
		scale = binary.BigEndian.Uint32(mvhd[12:])
		duration = uint64(binary.BigEndian.Uint32(mvhd[16:]))
	} else if mvhd[0] == 1 && len(mvhd) >= 32 {
		scale = binary.BigEndian.Uint32(mvhd[20:])
		duration = binary.BigEndian.Uint64(mvhd[24:])
	} else {
		return fail()
	}
	if scale == 0 || duration == 0 || float64(duration)/float64(scale) > 86400 {
		return fail()
	}
	result := DeckAssetVideo{MIME: mime, DurationMS: int64(math.Round(float64(duration) * 1000 / float64(scale)))}
	for _, trak := range moov {
		if trak.kind != "trak" {
			continue
		}
		track, err := movieAtoms(trak.data)
		if err != nil {
			return DeckAssetVideo{}, err
		}
		mdia, err := movieAtoms(movieChild(track, "mdia"))
		if err != nil {
			return DeckAssetVideo{}, err
		}
		minf, err := movieAtoms(movieChild(mdia, "minf"))
		if err != nil {
			return DeckAssetVideo{}, err
		}
		dinf, err := movieAtoms(movieChild(minf, "dinf"))
		if err != nil {
			return DeckAssetVideo{}, err
		}
		dref := movieChild(dinf, "dref")
		if len(dref) < 8 {
			return fail()
		}
		refs, err := movieAtoms(dref[8:])
		if err != nil || len(refs) == 0 || uint32(len(refs)) != binary.BigEndian.Uint32(dref[4:]) {
			return fail()
		}
		for _, ref := range refs {
			if ref.kind != "url " || len(ref.data) != 4 || binary.BigEndian.Uint32(ref.data)&1 == 0 {
				return fail()
			}
		}
		handler := movieChild(mdia, "hdlr")
		if len(handler) < 12 || string(handler[8:12]) != "vide" {
			continue
		}
		tkhd := movieChild(track, "tkhd")
		offset := 76
		if len(tkhd) > 0 && tkhd[0] == 1 {
			offset = 88
		} else if len(tkhd) == 0 || tkhd[0] != 0 {
			return fail()
		}
		if len(tkhd) < offset+8 {
			return fail()
		}
		w, h := int(binary.BigEndian.Uint32(tkhd[offset:])>>16), int(binary.BigEndian.Uint32(tkhd[offset+4:])>>16)
		// A quarter-turn track matrix swaps the displayed aspect ratio.
		matrix := tkhd[offset-36 : offset]
		if binary.BigEndian.Uint32(matrix) == 0 && binary.BigEndian.Uint32(matrix[16:]) == 0 {
			w, h = h, w
		}
		if w < 1 || h < 1 || w > 8192 || h > 8192 || int64(w)*int64(h) > 33_177_600 {
			return fail()
		}
		stbl, err := movieAtoms(movieChild(minf, "stbl"))
		if err != nil {
			return DeckAssetVideo{}, err
		}
		stsd := movieChild(stbl, "stsd")
		if len(stsd) < 8 {
			return fail()
		}
		entries, err := movieAtoms(stsd[8:])
		if err != nil || len(entries) != 1 {
			return fail()
		}
		switch entries[0].kind {
		case "avc1", "avc3", "hvc1", "hev1", "mp4v":
		default:
			return fail()
		}
		if result.Width != 0 {
			return fail()
		} // One visual track; no external/reference movies.
		result.Width, result.Height = w, h
	}
	if result.Width == 0 || result.DurationMS < 1 {
		return fail()
	}
	return result, nil
}

func embeddedVideoAsset(ctx context.Context, data, poster []byte) (string, DeckAsset, string, error) {
	if err := ctx.Err(); err != nil {
		return "", DeckAsset{}, "", err
	}
	video, err := inspectMovie(data)
	if err != nil {
		return "", DeckAsset{}, "", err
	}
	if len(poster) == 0 {
		w, h := boundedImageSize(video.Width, video.Height, 640)
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.Draw(img, img.Bounds(), image.NewUniform(color.NRGBA{22, 30, 42, 255}), image.Point{}, draw.Src)
		for y := h / 3; y < h*2/3; y++ {
			span := h/6 - abs(y-h/2)
			for x := w/2 - h/12; x < w/2-h/12+span; x++ {
				if image.Pt(x, y).In(img.Bounds()) {
					img.Set(x, y, color.White)
				}
			}
		}
		var buffer bytes.Buffer
		if err = png.Encode(&buffer, img); err != nil {
			return "", DeckAsset{}, "", err
		}
		poster = buffer.Bytes()
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(poster))
	if err != nil || format != "png" || len(poster) > 2<<20 || config.Width < 1 || config.Height < 1 || config.Width > 1280 || config.Height > 1280 {
		return "", DeckAsset{}, "", fmt.Errorf("invalid video poster image")
	}
	if _, _, err = image.Decode(bytes.NewReader(poster)); err != nil {
		return "", DeckAsset{}, "", err
	}
	video.Data = append([]byte(nil), data...)
	asset := DeckAsset{MIME: "image/png", Width: config.Width, Height: config.Height, Data: append([]byte(nil), poster...), Video: &video}
	hash := sha256.New()
	hash.Write([]byte("keynope-video-v1\x00"))
	hash.Write(poster)
	hash.Write(data)
	id := hex.EncodeToString(hash.Sum(nil))
	path := embeddedAssetPath(id, asset)
	registerEmbeddedStillAsset(id, asset)
	return id, asset, path, nil
}

func videoElement(id, path string, asset DeckAsset, canvas *Deck) Element {
	cols, rows := authoredRenderSize(defaultAuthoredTerminalWidth, defaultAuthoredTerminalHeight)
	physical := canvas.physicalCanvas()
	w := float64(cols) * .6
	h := w * physical.Width / float64(cols) * float64(asset.Video.Height) / float64(asset.Video.Width) * float64(rows) / physical.Height
	if h > float64(rows)*.75 {
		w *= float64(rows) * .75 / h
		h = float64(rows) * .75
	}
	q := url.Values{}
	for key, value := range map[string]float64{"left": (float64(cols) - w) / 2, "top": (float64(rows) - h) / 2, "width": w, "height": h} {
		if key == "left" || key == "top" {
			whole := math.Floor(value)
			q.Set(key, strconv.Itoa(int(whole)))
			axis := "x"
			if key == "top" {
				axis = "y"
			}
			if value != whole {
				q.Set("object-offset-"+axis, strconv.FormatFloat(value-whole, 'f', 4, 64))
			}
		} else {
			q.Set(key, strconv.FormatFloat(value, 'f', 4, 64))
		}
	}
	return Element{Kind: "video", AssetID: id, Path: path, Query: q.Encode()}
}

func (s *nativeEditorSession) handleVideoUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}
	s.mu.RLock()
	revision, master, destination := s.version, s.masterMode, s.current
	if master {
		destination = s.currentMaster
	}
	s.mu.RUnlock()
	r.Body = http.MaxBytesReader(w, r.Body, maxEmbeddedVideoBytes+(3<<20))
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		http.Error(w, "Invalid video upload or insufficient embedded-media capacity", 400)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "Choose an MP4 or MOV video", 400)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxEmbeddedVideoBytes+1))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var poster []byte
	if p, _, err := r.FormFile("poster"); err == nil {
		defer p.Close()
		poster, err = io.ReadAll(io.LimitReader(p, (2<<20)+1))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	id, asset, path, err := embeddedVideoAsset(r.Context(), data, poster)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.mu.RLock()
	element := videoElement(id, path, asset, &s.deck)
	s.mu.RUnlock()
	if err = s.apply(nativeEditorAction{Action: "add-element", Kind: "video", ElementData: &element, AssetData: &asset, SceneRevision: &revision, SceneMaster: master, Slide: destination, uploadContext: r.Context()}); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.transportState())
}

func projectedVideo(asset DeckAsset, q url.Values) *sceneVideo {
	return &sceneVideo{Source: sharedSceneSources.uri(asset.Video.MIME, asset.Video.Data), Autoplay: q.Get("video-autoplay") == "1", Loop: q.Get("video-loop") == "1", Muted: q.Get("video-muted") == "1"}
}

// The caller holds the command lock. Reject an import before changing history
// or the deck, rather than accepting a clip which would later prevent saving.
func (s *nativeEditorSession) checkEmbeddedMediaCapacityLocked(action nativeEditorAction) error {
	if action.Action != "add-element" || action.AssetData == nil || action.ElementData == nil {
		return nil
	}
	candidate := s.deck
	candidate.Assets = make(map[string]DeckAsset, len(s.deck.Assets)+1)
	for id, asset := range s.deck.Assets {
		candidate.Assets[id] = asset
	}
	pruneUnusedDeckAssets(&candidate)
	candidate.Assets[action.ElementData.AssetID] = *action.AssetData
	size, err := deckAssetArchiveStorageSize(candidate.Assets)
	if err != nil {
		return err
	}
	if size > maxDeckAssetArchiveBytes {
		message := videoCapacityMessage
		if action.AssetData.Video == nil {
			message = "This media will not fit in the deck's embedded-media capacity (100 MiB total, including encoding). Remove embedded media or choose a smaller file."
		}
		return editorFailure("media-capacity", "assetData", action.ObjectID, fmt.Errorf("%s", message))
	}
	return nil
}
