package app

import "encoding/json"

func assetHasOpaqueMetadata(asset DeckAsset) bool {
	has := func(extra *jsonExtensions) bool { return extra != nil && len(*extra) > 0 }
	if has(asset.Extra) || asset.Vector != nil && has(asset.Vector.Extra) {
		return true
	}
	for _, frame := range asset.Frames {
		if has(frame.Extra) {
			return true
		}
	}
	if asset.Source != nil {
		if has(asset.Source.Extra) {
			return true
		}
		for _, frame := range asset.Source.Frames {
			if has(frame.Extra) {
				return true
			}
		}
	}
	return false
}

func (asset DeckAsset) MarshalJSON() ([]byte, error) {
	type stored DeckAsset
	return encodeJSONExtensions(stored(asset), asset.Extra)
}

func (asset *DeckAsset) UnmarshalJSON(data []byte) error {
	type stored DeckAsset
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "vector", "video", "source", "mime", "width", "height", "data", "frames", "loopCount")
	if err != nil {
		return err
	}
	*asset = DeckAsset(value)
	asset.Extra = extra
	return nil
}

func (source DeckAssetSource) MarshalJSON() ([]byte, error) {
	type stored DeckAssetSource
	return encodeJSONExtensions(stored(source), source.Extra)
}

func (source *DeckAssetSource) UnmarshalJSON(data []byte) error {
	type stored DeckAssetSource
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "mime", "width", "height", "data", "frames")
	if err != nil {
		return err
	}
	*source = DeckAssetSource(value)
	source.Extra = extra
	return nil
}

func (frame DeckAssetFrame) MarshalJSON() ([]byte, error) {
	type stored DeckAssetFrame
	return encodeJSONExtensions(stored(frame), frame.Extra)
}

func (frame *DeckAssetFrame) UnmarshalJSON(data []byte) error {
	type stored DeckAssetFrame
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "data", "delayMs")
	if err != nil {
		return err
	}
	*frame = DeckAssetFrame(value)
	frame.Extra = extra
	return nil
}

func (vector DeckAssetVector) MarshalJSON() ([]byte, error) {
	type stored DeckAssetVector
	return encodeJSONExtensions(stored(vector), vector.Extra)
}

func (vector *DeckAssetVector) UnmarshalJSON(data []byte) error {
	type stored DeckAssetVector
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	extra, err := decodeJSONExtensions(data, "version", "mime", "width", "height", "data")
	if err != nil {
		return err
	}
	*vector = DeckAssetVector(value)
	vector.Extra = extra
	return nil
}

// Public Markdown needs media pixels, never opaque authoring extensions.
// Copy containers only; rendering never modifies the immutable pixel bytes.
func participantMediaAssets(assets map[string]DeckAsset) map[string]DeckAsset {
	if assets == nil {
		return nil
	}
	public := make(map[string]DeckAsset, len(assets))
	clearFrames := func(frames []DeckAssetFrame) []DeckAssetFrame {
		result := append([]DeckAssetFrame(nil), frames...)
		for i := range result {
			result[i].Extra = nil
		}
		return result
	}
	for id, asset := range assets {
		// A movie's poster can also be used as an ordinary image/background.
		// Preserve those pixels, but never serialize its movie bytes publicly.
		asset.Video = nil
		asset.Extra = nil
		asset.Frames = clearFrames(asset.Frames)
		if asset.Source != nil {
			source := *asset.Source
			source.Extra = nil
			source.Frames = clearFrames(source.Frames)
			asset.Source = &source
		}
		if asset.Vector != nil {
			vector := *asset.Vector
			vector.Extra = nil
			asset.Vector = &vector
		}
		public[id] = asset
	}
	return public
}
