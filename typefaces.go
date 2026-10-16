package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/image/font/sfnt"
)

// Installed fonts become immutable, content-addressed deck resources. Restricted
// faces are references only: their bytes never enter a saved deck or broadcast.
type DeckTypeface struct {
	Name           string `json:"name"`
	PostScriptName string `json:"postScriptName"`
	Data           []byte `json:"data,omitempty"`
	LocalOnly      bool   `json:"localOnly,omitempty"`
	EmbeddingFlags uint16 `json:"embeddingFlags,omitempty"`
	metric         *sfnt.Font
}

const maxTypefaceBytes = 16 << 20
const maxTypefaceArchiveBytes = 64 << 20

var typefaceIDRE = regexp.MustCompile(`^font-[a-f0-9]{64}$`)
var typefacesRE = regexp.MustCompile(`(?m)^<!--\s*keynope-typefaces\s+version=1\s+base64:([A-Za-z0-9+/=]+)\s*-->[\t ]*\r?\n?`)

func validTextFont(id string) bool { return validBundledFont(id) || typefaceIDRE.MatchString(id) }
func typefaceFamily(id string) string {
	if typefaceIDRE.MatchString(id) {
		return "KeynopeFont_" + strings.TrimPrefix(id, "font-")
	}
	switch id {
	case "c64":
		return "KeynopeC64"
	case "mono":
		return "KeynopeModernMono"
	default:
		return "KeynopeModern"
	}
}
func (f DeckTypeface) ID() string {
	data := f.Data
	if f.LocalOnly {
		data = []byte("local\x00" + f.PostScriptName)
	}
	sum := sha256.Sum256(data)
	return "font-" + hex.EncodeToString(sum[:])
}
func sfntTable(data []byte, tag string) []byte {
	if len(data) < 12 {
		return nil
	}
	n := int(binary.BigEndian.Uint16(data[4:6]))
	if n > 256 || 12+n*16 > len(data) {
		return nil
	}
	for i := 0; i < n; i++ {
		p := 12 + i*16
		if string(data[p:p+4]) != tag {
			continue
		}
		offset, size := uint64(binary.BigEndian.Uint32(data[p+8:])), uint64(binary.BigEndian.Uint32(data[p+12:]))
		if offset+size > uint64(len(data)) {
			return nil
		}
		return data[offset : offset+size]
	}
	return nil
}
func typefaceEmbeddingFlags(data []byte) uint16 {
	table := sfntTable(data, "OS/2")
	if len(table) < 10 {
		return 2 // Unknown permissions: do not redistribute outlines.
	}
	flags := binary.BigEndian.Uint16(table[8:10])
	if binary.BigEndian.Uint16(table[:2]) < 2 {
		flags &= 15 // Higher bits were reserved in these OS/2 versions.
	}
	return flags
}
func typefacePermission(flags uint16) uint16 {
	// Older fonts can set multiple permissions; use the least restrictive.
	if flags&8 != 0 {
		return 8
	}
	if flags&4 != 0 {
		return 4
	}
	if flags&2 != 0 {
		return 2
	}
	return 0
}
func editableTypefaceEmbedding(data []byte) bool {
	flags := typefaceEmbeddingFlags(data)
	return flags&0x200 == 0 && typefacePermission(flags) != 2 && typefacePermission(flags) != 4
}
func (f DeckTypeface) previewOnly() bool {
	return !f.LocalOnly && len(f.Data) != 0 && typefacePermission(typefaceEmbeddingFlags(f.Data)) == 4
}
func (f DeckTypeface) referenceOnly() bool {
	flags := f.EmbeddingFlags
	if len(f.Data) != 0 {
		flags = typefaceEmbeddingFlags(f.Data)
	}
	return typefacePermission(flags) == 2 || flags&0x200 != 0
}
func prepareTypeface(value DeckTypeface) (DeckTypeface, error) {
	for _, s := range []string{value.Name, value.PostScriptName} {
		if len(s) == 0 || len(s) > 256 || !utf8.ValidString(s) || strings.IndexFunc(s, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return value, fmt.Errorf("invalid font name")
		}
	}
	if value.LocalOnly {
		if len(value.Data) > 0 {
			return value, fmt.Errorf("local-only fonts cannot contain embedded font data")
		}
		return value, nil
	}
	if len(value.Data) < 12 || len(value.Data) > maxTypefaceBytes {
		return value, fmt.Errorf("font file exceeds the supported size or is empty")
	}
	value.Data = bytes.Clone(value.Data)
	font, err := sfnt.Parse(value.Data)
	if err != nil {
		return value, fmt.Errorf("unsupported or damaged OpenType font: %w", err)
	}
	value.EmbeddingFlags = typefaceEmbeddingFlags(value.Data)
	value.metric = font
	var buffer sfnt.Buffer
	if name, err := font.Name(&buffer, sfnt.NameIDFull); err == nil && name != "" {
		value.Name = name
	}
	if name, err := font.Name(&buffer, sfnt.NameIDPostScript); err == nil && name != "" {
		value.PostScriptName = name
	}
	for _, s := range []string{value.Name, value.PostScriptName} {
		if len(s) > 256 || !utf8.ValidString(s) || strings.IndexFunc(s, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
			return value, fmt.Errorf("invalid internal font name")
		}
	}
	if value.referenceOnly() {
		value.LocalOnly, value.Data = true, nil
	}
	return value, nil
}
func cloneTypefaces(values map[string]DeckTypeface, pixels bool) map[string]DeckTypeface {
	if values == nil {
		return nil
	}
	out := make(map[string]DeckTypeface, len(values))
	for id, value := range values {
		if pixels {
			value.Data = bytes.Clone(value.Data)
		}
		out[id] = value
	}
	return out
}
func decodeTypefaces(text string) (map[string]DeckTypeface, string, error) {
	envelope := metadataNamespaceEnvelopeRE("keynope-typefaces")
	markers := findDocumentMetadataIndices(text, envelope, 2)
	if len(markers) == 0 {
		return nil, text, nil
	}
	matches := findDocumentMetadata(text, typefacesRE, 2)
	if len(markers) != 1 || len(matches) != 1 || len(matches[0][1]) > maxTypefaceArchiveBytes*2 {
		return nil, text, fmt.Errorf("invalid, duplicated or oversized typeface metadata")
	}
	compressed, err := base64.StdEncoding.DecodeString(matches[0][1])
	if err != nil {
		return nil, text, err
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, text, err
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxTypefaceArchiveBytes+1))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || len(data) > maxTypefaceArchiveBytes {
		return nil, text, fmt.Errorf("invalid or oversized typeface archive")
	}
	if _, err = decodeMetadataJSONObject(data); err != nil {
		return nil, text, err
	}
	var values map[string]DeckTypeface
	if err = json.Unmarshal(data, &values); err != nil {
		return nil, text, err
	}
	if len(values) > 128 {
		return nil, text, fmt.Errorf("too many embedded typefaces")
	}
	for id, value := range values {
		originalID := value.ID()
		prepared, err := prepareTypeface(value)
		if err != nil {
			return nil, text, err
		}
		if !typefaceIDRE.MatchString(id) || id != originalID {
			return nil, text, fmt.Errorf("typeface identity does not match its font data")
		}
		values[id] = prepared
	}
	return values, rewriteDocumentMetadata(text, typefacesRE, nil), nil
}
func encodeTypefaces(values map[string]DeckTypeface) (string, error) {
	if len(values) == 0 {
		return "", nil
	}
	if len(values) > 128 {
		return "", fmt.Errorf("too many typefaces")
	}
	for id, value := range values {
		if id != value.ID() {
			return "", fmt.Errorf("invalid typeface identity")
		}
		if !value.LocalOnly && value.referenceOnly() {
			return "", fmt.Errorf("restricted fonts must be stored as installed-font references")
		}
	}
	data, err := json.Marshal(values)
	if err != nil || len(data) > maxTypefaceArchiveBytes {
		return "", fmt.Errorf("typeface archive exceeds its size limit")
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err = writer.Write(data); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	return "<!-- keynope-typefaces version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->", nil
}
func typefaceCSS(values map[string]DeckTypeface) string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var css strings.Builder
	for _, id := range ids {
		value := values[id]
		if !typefaceIDRE.MatchString(id) {
			continue
		}
		source := ""
		if value.LocalOnly {
			name, _ := json.Marshal(value.PostScriptName)
			source = "local(" + string(name) + ")"
		} else {
			source = "url(data:font/otf;base64," + base64.StdEncoding.EncodeToString(value.Data) + ")"
		}
		fmt.Fprintf(&css, "@font-face{font-family:%s;font-weight:400;font-style:normal;font-display:block;src:%s}\n", typefaceFamily(id), source)
	}
	return css.String()
}

func typefaceDescriptions(values map[string]DeckTypeface) map[string]DeckTypeface {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]DeckTypeface, len(values))
	for id, value := range values {
		value.Data = nil
		value.metric = nil
		out[id] = value
	}
	return out
}
func mergeTypefaces(a, b map[string]DeckTypeface) map[string]DeckTypeface {
	if len(b) == 0 {
		return a
	}
	out := cloneTypefaces(a, false)
	if out == nil {
		out = map[string]DeckTypeface{}
	}
	for id, value := range b {
		out[id] = value
	}
	return out
}

// Content IDs are immutable. Caches need availability, not a fresh hash of all
// font bytes on each keystroke.
func typefaceAvailabilityKey(values map[string]DeckTypeface) string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func textTypefaceResources(text *sceneText, fonts map[string]DeckTypeface) map[string]DeckTypeface {
	return sceneTypefaceResources(slideScene{Objects: []sceneObject{{Text: text}}}, fonts)
}

func sceneTypefaceResources(scene slideScene, fonts map[string]DeckTypeface) map[string]DeckTypeface {
	var result map[string]DeckTypeface
	add := func(id string) {
		if face, ok := fonts[id]; ok {
			if result == nil {
				result = map[string]DeckTypeface{}
			}
			result[id] = face
		}
	}
	text := func(t *sceneText) {
		if t == nil {
			return
		}
		add(t.FontID)
		for _, id := range t.FontRoles {
			add(id)
		}
		for _, run := range t.Runs {
			add(run.FontID)
		}
		for _, p := range t.Paragraphs {
			for _, run := range p.Runs {
				add(run.FontID)
			}
		}
	}
	for _, o := range scene.Objects {
		text(o.Text)
		text(o.Label)
		if o.Table != nil {
			for _, c := range o.Table.Cells {
				text(c.Text)
			}
		}
		if o.Chart != nil {
			for _, l := range o.Chart.Labels {
				text(l.Text)
			}
		}
	}
	return result
}
func (s *nativeEditorSession) registerTypeface(action nativeEditorAction) (resultErr error) {
	if action.TypefaceData == nil {
		return errInvalidEditorAction
	}
	value, err := prepareTypeface(*action.TypefaceData)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if err = s.checkCommandOwnerLocked(action); err != nil {
		s.mu.Unlock()
		return err
	}
	if err = s.checkMutationRevisionLocked(action); err != nil {
		s.mu.Unlock()
		return err
	}
	defer s.mu.Unlock()
	all := mergeTypefaces(s.deck.Typefaces, s.fontLibrary)
	if _, ok := all[value.ID()]; ok {
		return nil
	}
	if len(all) >= 128 {
		return fmt.Errorf("a deck can contain at most 128 typefaces")
	}
	total := len(value.Data)
	for _, face := range all {
		total += len(face.Data)
	}
	if total > 40<<20 {
		return fmt.Errorf("embedded fonts exceed the deck font budget")
	}
	if s.fontLibrary == nil {
		s.fontLibrary = map[string]DeckTypeface{}
	}
	s.fontLibrary[value.ID()] = value
	// Resource admission is not a content edit: preserve open text/theme draft
	// ownership and undo history. The next content command owns the resource.
	return nil
}

// Inline/table/theme drafts may choose a font not yet used by the current
// scene. Fetch just that immutable resource instead of every font at startup.
func (s *nativeEditorSession) handleTypeface(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	s.mu.RLock()
	face, ok := s.deck.Typefaces[id]
	if !ok {
		face, ok = s.fontLibrary[id]
	}
	s.mu.RUnlock()
	if !ok || !typefaceIDRE.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	_ = json.NewEncoder(w).Encode(face)
}
