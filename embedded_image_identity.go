package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// Repair replaced or stale compact pixels at the document-load boundary. Keep
// identical files untouched so their stamp remains reusable. Atomic replacement
// also means concurrent readers never decode a partially written PNG, and an
// existing symlink cannot redirect the write outside the embedded cache.
func materializeEmbeddedStillFile(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Size() == int64(len(data)) {
		if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".embedded-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0o644); err == nil {
		_, err = file.Write(data)
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}

// Geometry snapshots must not be published under an earlier source identity if
// another document replaces a registered compact asset during legacy layout.
var embeddedImageRegistrationRevision atomic.Uint64

// Compute once at registration, not for every frame/preview lookup. Assets are
// registered again when their source changes; paths alone are not identities.
// Only the compact pixels used by the Retro renderer participate, not the
// independent full-resolution source used by modern media.
func embeddedImageSourceIdentity(asset DeckAsset) string {
	h := sha256.New()
	writeInt := func(value int64) {
		var bytes [8]byte
		binary.LittleEndian.PutUint64(bytes[:], uint64(value))
		h.Write(bytes[:])
	}
	writeBytes := func(data []byte) { writeInt(int64(len(data))); h.Write(data) }
	writeBytes([]byte(asset.MIME))
	writeInt(int64(asset.Width))
	writeInt(int64(asset.Height))
	writeInt(int64(asset.LoopCount))
	writeBytes(asset.Data)
	writeInt(int64(len(asset.Frames)))
	for _, frame := range asset.Frames {
		writeInt(frame.DelayMS)
		writeBytes(frame.Data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func embeddedImageCacheIdentity(path string) string {
	if !strings.HasPrefix(path, "keynope-asset:") {
		if identity, ok := nativeEmbeddedImageCacheIdentity(path); ok {
			return identity
		}
		return path
	}
	id := strings.TrimPrefix(path, "keynope-asset:")
	ext := filepath.Ext(id)
	id = strings.TrimSuffix(id, ext)
	var identity string
	if strings.EqualFold(ext, ".gif") {
		embeddedAnimatedAssets.RLock()
		identity = embeddedAnimatedAssets.identities[id]
		embeddedAnimatedAssets.RUnlock()
	}
	if identity == "" {
		embeddedStillAssets.RLock()
		identity = embeddedStillAssets.identities[id]
		embeddedStillAssets.RUnlock()
	}
	if identity == "" {
		return path
	}
	return path + "#" + identity
}

func nativeEmbeddedImageCacheIdentity(path string) (string, bool) {
	ext := filepath.Ext(path)
	id := strings.TrimSuffix(filepath.Base(path), ext)
	if (ext != ".png" && ext != ".gif") || !validObjectID(id) || filepath.Clean(path) != filepath.Join(imageCacheDirectory(), "embedded", id+ext) {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	registered := embeddedImageCacheIdentity("keynope-asset:" + id + ext)
	return path + "#" + registered + "|" + strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10), true
}

// A scene already owns the deck's embedded pixels. Use their registered source
// for layout as well as media paint, rather than stat/decode an incidental disk
// materialisation on every edit. Legacy/local file rendering remains file-backed.
func sceneEmbeddedImagePath(deck Deck, element Element) string {
	if strings.HasPrefix(element.Path, "keynope-asset:") || !validObjectID(element.AssetID) {
		return element.Path
	}
	asset, exists := deck.Assets[element.AssetID]
	if !exists || len(asset.Data) == 0 || len(asset.Frames) != 0 || element.Path != embeddedAssetPath(element.AssetID, asset) || filepath.Ext(element.Path) != ".png" {
		return element.Path
	}
	virtual := "keynope-asset:" + element.AssetID + ".png"
	registered, ok := embeddedStillAsset(virtual)
	if !ok || registered.MIME != asset.MIME || registered.Width != asset.Width || registered.Height != asset.Height || !bytes.Equal(registered.Data, asset.Data) {
		return element.Path
	}
	return virtual
}
