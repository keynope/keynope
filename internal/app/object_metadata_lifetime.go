package app

import (
	"reflect"
	"sync"
)

type opaqueMetadataField struct {
	index     int
	extension bool
}

var opaqueMetadataFields sync.Map // reflect.Type -> immutable []opaqueMetadataField

func objectMetadataFields(typeOf reflect.Type) []opaqueMetadataField {
	if cached, ok := opaqueMetadataFields.Load(typeOf); ok {
		return cached.([]opaqueMetadataField)
	}
	fields := []opaqueMetadataField{}
	for i := 0; i < typeOf.NumField(); i++ {
		field := typeOf.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if typeOf == reflect.TypeOf(Deck{}) && (field.Name == "Assets" || field.Name == "Diagnostics") {
			continue
		}
		if typeOf == reflect.TypeOf(Slide{}) && (field.Name == "ModernScene" || field.Name == "ThemeColors" || field.Name == "ShapeStrokeDefaults") {
			continue
		}
		switch field.Type.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Struct, reflect.Slice, reflect.Array:
			if (field.Type.Kind() == reflect.Slice || field.Type.Kind() == reflect.Array) && field.Type.Elem().Kind() == reflect.Uint8 {
				continue
			}
			fields = append(fields, opaqueMetadataField{i, field.Name == "Extra"})
		}
	}
	cached, _ := opaqueMetadataFields.LoadOrStore(typeOf, fields)
	return cached.([]opaqueMetadataField)
}

// Unknown authored fields may contain references that this version cannot
// interpret. Do not garbage-collect their possible dependencies at save time.
// Inspect metadata only: encoded media and runtime projections are irrelevant
// to authored object lifetime and must not be traversed byte by byte.
func hasOpaqueObjectMetadata(deck Deck) bool {
	// Media is excluded from reflective walking to avoid scanning pixel/frame
	// payloads. Its newly version-compatible fields still own unknown references.
	for _, asset := range deck.Assets {
		if assetHasOpaqueMetadata(asset) {
			return true
		}
	}
	seen := map[uintptr]bool{}
	var walk func(reflect.Value) bool
	walk = func(value reflect.Value) bool {
		if !value.IsValid() {
			return false
		}
		switch value.Kind() {
		case reflect.Interface:
			if !value.IsNil() {
				return walk(value.Elem())
			}
		case reflect.Pointer:
			if value.IsNil() {
				return false
			}
			pointer := value.Pointer()
			if seen[pointer] {
				return false
			}
			seen[pointer] = true
			return walk(value.Elem())
		case reflect.Struct:
			if value.Type() == reflect.TypeOf(Element{}) && !standardElementKind(value.FieldByName("Kind").String()) {
				return true
			}
			for _, field := range objectMetadataFields(value.Type()) {
				v := value.Field(field.index)
				if field.extension {
					for v.Kind() == reflect.Pointer && !v.IsNil() {
						v = v.Elem()
					}
					if v.Kind() == reflect.Map && v.Len() > 0 {
						return true
					}
					continue
				}
				if walk(v) {
					return true
				}
			}
		case reflect.Map:
			for iter := value.MapRange(); iter.Next(); {
				if walk(iter.Value()) {
					return true
				}
			}
		case reflect.Slice, reflect.Array:
			if value.Type().Elem().Kind() == reflect.Uint8 {
				return false
			}
			for i := 0; i < value.Len(); i++ {
				if walk(value.Index(i)) {
					return true
				}
			}
		}
		return false
	}
	return walk(reflect.ValueOf(deck))
}
