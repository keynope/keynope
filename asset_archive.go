package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"regexp"
)

const maxDeckAssetArchiveBytes = 100 << 20

var assetArchivePrefixRE = regexp.MustCompile(`<!--\s*keynope-assets(?:\s|-->|$)`)

// Limits apply to the complete decompressed JSON, not a truncated prefix that
// happens to be valid JSON. The encoder and decoder share the same boundary.
func decodeDeckAssetArchive(text string, limit int) (map[string]DeckAsset, string, error) {
	markers := findDocumentMetadataIndices(text, assetArchivePrefixRE, 2)
	if len(markers) == 0 {
		return nil, text, nil
	}
	matches := findDocumentMetadata(text, keynopeAssetsRE, 2)
	if len(markers) != 1 || len(matches) != 1 {
		return nil, text, fmt.Errorf("invalid, unsupported or duplicate embedded asset metadata")
	}
	if limit < 1 || limit > maxDeckAssetArchiveBytes || len(matches[0][1]) > limit*2 {
		return nil, text, fmt.Errorf("embedded asset metadata exceeds its encoded size limit")
	}
	compressed, err := base64.StdEncoding.DecodeString(matches[0][1])
	if err != nil {
		return nil, text, fmt.Errorf("decode embedded assets: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, text, fmt.Errorf("open embedded assets: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	closeErr := reader.Close()
	if len(data) > limit {
		return nil, text, fmt.Errorf("embedded assets exceed %d decompressed bytes", limit)
	}
	if readErr != nil {
		return nil, text, fmt.Errorf("read embedded assets: %w", readErr)
	}
	if closeErr != nil {
		return nil, text, fmt.Errorf("close embedded assets: %w", closeErr)
	}
	if fields, err := decodeMetadataJSONObject(data); err != nil {
		return nil, text, fmt.Errorf("parse embedded asset registry: %w", err)
	} else if fields == nil {
		return nil, text, fmt.Errorf("embedded asset registry needs a JSON object")
	}
	assets := map[string]DeckAsset{}
	if err := json.Unmarshal(data, &assets); err != nil {
		return nil, text, fmt.Errorf("parse embedded assets: %w", err)
	}
	recoverLegacyWebPSources(assets)
	if err := validateDeckAssetPixels(context.Background(), assets); err != nil {
		return nil, text, err
	}
	return assets, rewriteDocumentMetadata(text, keynopeAssetsRE, nil), nil
}

// Older still-image imports retained WebP source bytes but labelled them PNG,
// the format of their sampling derivative. Repair only that known archive
// mistake. The normal admission check below still validates complete pixels,
// declared dimensions and budgets for both derivative and source.
func recoverLegacyWebPSources(assets map[string]DeckAsset) {
	for id, asset := range assets {
		source := asset.Source
		if asset.MIME != "image/png" || len(asset.Frames) != 0 ||
			!bytes.HasPrefix(asset.Data, []byte("\x89PNG\r\n\x1a\n")) ||
			source == nil || source.MIME != "image/png" || len(source.Frames) != 0 ||
			len(source.Data) < 12 || string(source.Data[:4]) != "RIFF" || string(source.Data[8:12]) != "WEBP" {
			continue
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(source.Data))
		if err != nil || format != "webp" || config.Width != source.Width || config.Height != source.Height {
			continue
		}
		corrected := *source
		corrected.MIME = "image/webp"
		asset.Source = &corrected
		assets[id] = asset
	}
}

func encodeDeckAssetArchive(assets map[string]DeckAsset, limit int) (string, error) {
	if len(assets) == 0 {
		return "", nil
	}
	if limit < 1 || limit > maxDeckAssetArchiveBytes {
		return "", fmt.Errorf("invalid embedded asset metadata limit")
	}
	if err := validateDeckAssetPixels(context.Background(), assets); err != nil {
		return "", err
	}
	data, err := json.Marshal(assets)
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return "", fmt.Errorf("embedded assets exceed %d decompressed bytes", limit)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(data); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return "<!-- keynope-assets version=1 base64:" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + " -->", nil
}
