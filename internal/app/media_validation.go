package app

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
)

// Successful validations retain only a digest and decoded-pixel count, never
// source bytes or decoded images. FIFO bounds metadata even for many decks.
type mediaValidationCache struct {
	mu      sync.Mutex
	entries map[[32]byte]int64
	order   [][32]byte
}

var sharedMediaValidation mediaValidationCache

func (cache *mediaValidationCache) lookup(key [32]byte) (int64, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	usage, ok := cache.entries[key]
	return usage, ok
}

func (cache *mediaValidationCache) remember(key [32]byte, usage int64) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if _, exists := cache.entries[key]; exists {
		return
	}
	if cache.entries == nil {
		cache.entries = map[[32]byte]int64{}
	}
	if len(cache.order) >= 512 {
		delete(cache.entries, cache.order[0])
		copy(cache.order, cache.order[1:])
		cache.order = cache.order[:len(cache.order)-1]
	}
	cache.entries[key] = usage
	cache.order = append(cache.order, key)
}

// Include every known field consumed by pixel/vector validation, with typed
// boundaries. Unknown authoring extensions are not executable/rendering inputs.
// Hash actual bytes rather than IDs/pointers so in-place replacement is safe.
func mediaValidationDigest(ctx context.Context, asset DeckAsset) ([32]byte, error) {
	hash := sha256.New()
	integer := func(value int64) {
		var data [8]byte
		binary.LittleEndian.PutUint64(data[:], uint64(value))
		hash.Write(data[:])
	}
	data := func(value []byte) error {
		integer(int64(len(value)))
		for len(value) > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			n := min(len(value), 64<<10)
			hash.Write(value[:n])
			value = value[n:]
		}
		return ctx.Err()
	}
	image := func(mime string, width, height int, bytes []byte, frames []DeckAssetFrame) error {
		if err := data([]byte(mime)); err != nil {
			return err
		}
		integer(int64(width))
		integer(int64(height))
		if err := data(bytes); err != nil {
			return err
		}
		integer(int64(len(frames)))
		for _, frame := range frames {
			integer(frame.DelayMS)
			if err := data(frame.Data); err != nil {
				return err
			}
		}
		return nil
	}
	integer(int64(asset.LoopCount))
	if err := image(asset.MIME, asset.Width, asset.Height, asset.Data, asset.Frames); err != nil {
		return [32]byte{}, err
	}
	integer(0)
	if asset.Source != nil {
		integer(1)
		if err := image(asset.Source.MIME, asset.Source.Width, asset.Source.Height, asset.Source.Data, asset.Source.Frames); err != nil {
			return [32]byte{}, err
		}
	}
	integer(0)
	if asset.Vector != nil {
		integer(1)
		integer(int64(asset.Vector.Version))
		if err := image(asset.Vector.MIME, asset.Vector.Width, asset.Vector.Height, asset.Vector.Data, nil); err != nil {
			return [32]byte{}, err
		}
	}
	var key [32]byte
	copy(key[:], hash.Sum(nil))
	return key, ctx.Err()
}

func validateDeckAssetPixels(ctx context.Context, assets map[string]DeckAsset) error {
	for id, asset := range assets {
		if !validObjectID(id) {
			return fmt.Errorf("invalid embedded asset ID")
		}
		// Decks lazily render separate images. Do not turn the clipboard bundle's
		// aggregate limit into a whole-workshop pixel limit.
		var pixels int64
		if err := validateClipboardAssetBudgetContext(ctx, asset, &pixels); err != nil {
			return fmt.Errorf("embedded asset %s: %w", id, err)
		}
	}
	return ctx.Err()
}
