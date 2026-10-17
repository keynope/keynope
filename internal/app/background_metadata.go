package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

func (media slideBackgroundMedia) MarshalJSON() ([]byte, error) {
	type stored slideBackgroundMedia
	return encodeJSONExtensions(stored(media), media.Extra)
}

func (media *slideBackgroundMedia) UnmarshalJSON(data []byte) error {
	type stored slideBackgroundMedia
	var value stored
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value.SourceObjectID != "" && !validObjectID(value.SourceObjectID) {
		return fmt.Errorf("invalid background source object identity")
	}
	extra, err := decodeJSONExtensions(data, "assetId", "fit", "opacity", "opacitySet", "query", "sourceObjectId")
	if err != nil {
		return err
	}
	*media = slideBackgroundMedia(value)
	media.Extra = extra
	return nil
}

func decodeBackgroundMetadata(comment string) (*slideBackgroundMedia, error) {
	payload, err := backgroundMetadataEnvelope.decodeComment(comment)
	if err != nil {
		return nil, err
	}
	var media *slideBackgroundMedia // null explicitly clears an inherited image.
	if err := json.Unmarshal(payload, &media); err != nil {
		return nil, err
	}
	return media, nil
}

func encodeBackgroundMetadata(media *slideBackgroundMedia) (string, error) {
	payload, err := json.Marshal(media)
	if err != nil || len(payload) > maxJSONExtensionBytes {
		return "", fmt.Errorf("background media metadata is invalid or exceeds 1 MiB")
	}
	return "<!-- keynope-background-media version=1 base64:" + base64.StdEncoding.EncodeToString(payload) + " -->", nil
}
