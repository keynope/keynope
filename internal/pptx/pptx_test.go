package pptx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

type byteReaderAt []byte

func (b byteReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestWriteAndReadBasicPresentation(t *testing.T) {
	input := Presentation{Slides: []Slide{{Objects: []Object{
		{ID: "title", Kind: "text", X: 96, Y: 80, Width: 1000, Height: 120, Text: "Hello <Keynope>", FontFace: "Arial", FontSize: 42, Color: "#55AAFF", Link: "https://keynope.sh"},
		{ID: "box", Kind: "shape", X: 40, Y: 300, Width: 400, Height: 180, Fill: "#001020", Stroke: "#55FFFF", StrokeWidth: 2, Shape: "roundRect"},
		{ID: "pixel", Kind: "image", X: 600, Y: 300, Width: 120, Height: 120, Image: []byte{137, 80, 78, 71, 13, 10, 26, 10}, ImageMIME: "image/png"},
	}}}}
	var output bytes.Buffer
	if _, err := Write(context.Background(), &output, input); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(output.Bytes(), []byte("PK")) {
		t.Fatal("did not create a ZIP")
	}
	got, report, err := Read(context.Background(), byteReaderAt(output.Bytes()), int64(output.Len()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %#v", report.Warnings)
	}
	if len(got.Slides) != 1 || len(got.Slides[0].Objects) != 3 {
		t.Fatalf("wrong output %#v", got)
	}
	if got.Slides[0].Objects[0].Text != "Hello <Keynope>" {
		t.Fatalf("text lost: %#v", got.Slides[0].Objects[0])
	}
	if got.Slides[0].Objects[0].Link != "https://keynope.sh" {
		t.Fatalf("link lost: %#v", got.Slides[0].Objects[0])
	}
	if got.Slides[0].Objects[1].Fill != "#001020" {
		t.Fatalf("fill lost: %#v", got.Slides[0].Objects[1])
	}
	if len(got.Slides[0].Objects[2].Image) != 8 {
		t.Fatalf("image lost: %#v", got.Slides[0].Objects[2])
	}
}

func TestWriteEmitsPowerPointRequiredThemeAndMasterParts(t *testing.T) {
	input := Presentation{Slides: []Slide{{Objects: []Object{{ID: "text", Kind: "text", X: 1, Y: 1, Width: 100, Height: 40, Text: "Hello"}}}}}
	var output bytes.Buffer
	if _, err := Write(context.Background(), &output, input); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(byteReaderAt(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		parts[file.Name] = string(data)
	}
	for name, want := range map[string]string{
		"[Content_Types].xml":               "core-properties+xml",
		"ppt/slideMasters/slideMaster1.xml": "<p:clrMap ",
		"ppt/slideLayouts/slideLayout1.xml": "<p:clrMapOvr>",
		"ppt/theme/theme1.xml":              "<a:objectDefaults/>",
	} {
		if !strings.Contains(parts[name], want) {
			t.Fatalf("%s missing required PPTX structure %q", name, want)
		}
	}
	if strings.Count(parts["ppt/theme/theme1.xml"], "<a:effectStyle>") != 3 {
		t.Fatalf("theme effect style list is not schema-complete: %s", parts["ppt/theme/theme1.xml"])
	}
}

func TestWriteEmbedsNativeFontDataForEditableText(t *testing.T) {
	fontData := tinyEmbeddedFontTestTTF()
	binary.BigEndian.PutUint16(fontData[88:90], 0x0004) // preview-and-print source font
	input := Presentation{
		Slides:        []Slide{{Objects: []Object{{ID: "title", Kind: "text", X: 1, Y: 1, Width: 100, Height: 40, Text: "READY.", FontFace: "Yolomancer C64", TextAlign: "justify", VerticalAlign: "bottom"}}}},
		EmbeddedFonts: []EmbeddedFont{{Family: "Yolomancer C64", Data: fontData, ForceEditable: true}},
	}
	var output bytes.Buffer
	if _, err := Write(context.Background(), &output, input); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(byteReaderAt(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string][]byte{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		if closeErr := reader.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
		parts[file.Name] = data
	}
	fontPart := parts["ppt/fonts/font1.fntdata"]
	expectedFontData := append([]byte(nil), fontData...)
	binary.BigEndian.PutUint16(expectedFontData[88:90], 0x0008)
	if len(fontPart) <= len(fontData) || binary.LittleEndian.Uint32(fontPart[8:12]) != 0x00020001 || !bytes.Equal(fontPart[len(fontPart)-len(expectedFontData):], expectedFontData) {
		t.Fatalf("embedded font is not an EOT wrapper around the supplied font")
	}
	if binary.LittleEndian.Uint16(fontPart[32:34]) != 0x0008 {
		t.Fatalf("embedded font is not marked editable")
	}
	for _, assertion := range []struct{ name, want string }{
		{"[Content_Types].xml", `Extension="fntdata" ContentType="application/x-fontdata"`},
		{"ppt/presentation.xml", `embedTrueTypeFonts="1"`},
		{"ppt/presentation.xml", `typeface="Yolomancer C64"`},
		{"ppt/_rels/presentation.xml.rels", `relationships/font" Target="fonts/font1.fntdata"`},
	} {
		if !strings.Contains(string(parts[assertion.name]), assertion.want) {
			t.Fatalf("%s missing %q", assertion.name, assertion.want)
		}
	}
	for _, want := range []string{`<a:bodyPr wrap="square" lIns="0" rIns="0" tIns="0" bIns="0" anchor="b"/>`, `<a:pPr algn="just"/>`} {
		if !strings.Contains(string(parts["ppt/slides/slide1.xml"]), want) {
			t.Fatalf("slide alignment markup missing %q: %s", want, parts["ppt/slides/slide1.xml"])
		}
	}
}

func tinyEmbeddedFontTestTTF() []byte {
	// A minimal sfnt table directory sufficient for EOT metadata extraction.
	data := make([]byte, 166)
	binary.BigEndian.PutUint32(data[0:4], 0x00010000)
	binary.BigEndian.PutUint16(data[4:6], 2)
	copy(data[12:16], "head")
	binary.BigEndian.PutUint32(data[20:24], 64)
	binary.BigEndian.PutUint32(data[24:28], 12)
	copy(data[28:32], "OS/2")
	binary.BigEndian.PutUint32(data[36:40], 80)
	binary.BigEndian.PutUint32(data[40:44], 86)
	binary.BigEndian.PutUint32(data[72:76], 0xAABBCCDD)
	binary.BigEndian.PutUint16(data[84:86], 400)
	return data
}

func TestReadRejectsUnsafeOrNonPresentationInput(t *testing.T) {
	_, _, err := Read(context.Background(), byteReaderAt([]byte("not a zip")), 9, Options{})
	if err == nil || !strings.Contains(err.Error(), "ZIP") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadResolvesPlaceholderGeometryThemeAndLayoutBackground(t *testing.T) {
	parts := map[string]string{
		"[Content_Types].xml":                          `<?xml version="1.0"?><Types/>`,
		"ppt/presentation.xml":                         `<p:presentation xmlns:p="p" xmlns:r="r"><p:sldSz cx="12192000" cy="6858000"/><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels":              relationships(`rId1`, `slides/slide1.xml`),
		"ppt/slides/slide1.xml":                        `<p:sld xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="2"/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:p><a:r><a:rPr/><a:t>Inherited title</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/_rels/slide1.xml.rels":             relationships(`rId1`, `../slideLayouts/slideLayout1.xml`),
		"ppt/slideLayouts/slideLayout1.xml":            `<p:sldLayout xmlns:p="p" xmlns:a="a"><p:cSld><p:bg><p:bgPr><a:solidFill><a:srgbClr val="120034"/></a:solidFill></p:bgPr></p:bg><p:spTree/></p:cSld></p:sldLayout>`,
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": relationships(`rId1`, `../slideMasters/slideMaster1.xml`),
		"ppt/slideMasters/slideMaster1.xml":            `<p:sldMaster xmlns:p="p" xmlns:a="a"><p:clrMap bg1="lt1" tx1="dk1"/><p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="4"/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="635000" y="1270000"/><a:ext cx="6350000" cy="635000"/></a:xfrm></p:spPr><p:txBody><a:bodyPr anchor="ctr"/><a:lstStyle><a:lvl1pPr algn="ctr"><a:defRPr sz="4400"><a:solidFill><a:schemeClr val="bg1"/></a:solidFill></a:defRPr></a:lvl1pPr></a:lstStyle></p:txBody></p:sp></p:spTree></p:cSld></p:sldMaster>`,
		"ppt/theme/theme1.xml":                         `<a:theme xmlns:a="a"><a:themeElements><a:clrScheme><a:lt1><a:srgbClr val="FFFFFF"/></a:lt1><a:dk1><a:srgbClr val="000000"/></a:dk1></a:clrScheme></a:themeElements></a:theme>`,
	}
	data := testPPTX(t, parts)
	got, report, err := Read(context.Background(), byteReaderAt(data), int64(len(data)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 0 || len(got.Slides) != 1 || len(got.Slides[0].Objects) != 1 {
		t.Fatalf("unexpected import: %#v %#v", got, report)
	}
	if got.Slides[0].Background != "#120034" {
		t.Fatalf("layout background lost: %#v", got.Slides[0])
	}
	title := got.Slides[0].Objects[0]
	if title.X != 100 || title.Y != 200 || title.Width != 1000 || title.Height != 100 || title.FontSize != 44 || title.Color != "#FFFFFF" || title.TextAlign != "center" || title.VerticalAlign != "middle" {
		t.Fatalf("placeholder inheritance failed: %#v", title)
	}
}

func TestReadResolvesSlidePictureBackground(t *testing.T) {
	const picture = "not-a-real-png-but-a-package-part"
	parts := map[string]string{
		"[Content_Types].xml":              `<?xml version="1.0"?><Types/>`,
		"ppt/presentation.xml":             `<p:presentation xmlns:p="p" xmlns:r="r"><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels":  relationships(`rId1`, `slides/slide1.xml`),
		"ppt/slides/slide1.xml":            `<p:sld xmlns:p="p" xmlns:a="a" xmlns:r="r"><p:cSld><p:bg><p:bgPr><a:blipFill><a:blip r:embed="rId2"/></a:blipFill></p:bgPr></p:bg><p:spTree/></p:cSld></p:sld>`,
		"ppt/slides/_rels/slide1.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId2" Type="image" Target="../media/background.png"/></Relationships>`,
		"ppt/media/background.png":         picture,
	}
	data := testPPTX(t, parts)
	got, _, err := Read(context.Background(), byteReaderAt(data), int64(len(data)), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slides) != 1 || string(got.Slides[0].BackgroundImage) != picture || got.Slides[0].BackgroundMIME != "image/png" {
		t.Fatalf("picture background lost: %#v", got.Slides)
	}
}

func relationships(id, target string) string {
	return `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="` + id + `" Type="x" Target="` + target + `"/></Relationships>`
}

func testPPTX(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, value := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
