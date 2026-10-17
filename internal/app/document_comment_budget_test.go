package app

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"image/color"
	"os"
	"reflect"
	"strings"
	"testing"
)

func compressedCommentFixture(t *testing.T, name, text string) string {
	t.Helper()
	var out bytes.Buffer
	writer := gzip.NewWriter(&out)
	if _, err := writer.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return "<!-- " + name + " version=1 base64:" + base64.StdEncoding.EncodeToString(out.Bytes()) + " -->"
}

func TestDocumentCommentBudgetBoundaries(t *testing.T) {
	for _, fragment := range []string{"<!-->", "<!--->", "<!---->"} {
		if err := validateDocumentCommentBudget(fragment, documentCommentLimits{Count: 1, Bytes: int64(len(fragment)), Expanded: int64(len(fragment))}); err != nil {
			t.Fatal("short comment handling", fragment, err)
		}
	}
	text := "prefix <!-- a --> text\n<!-- b\nc --> suffix"
	bytes := int64(len("<!-- a -->") + len("<!-- b\nc -->"))
	if err := validateDocumentCommentBudget(text, documentCommentLimits{Count: 2, Bytes: bytes, Expanded: bytes}); err != nil {
		t.Fatal("exact boundary rejected", err)
	}
	for _, limits := range []documentCommentLimits{{Count: 1, Bytes: bytes, Expanded: bytes}, {Count: 2, Bytes: bytes - 1, Expanded: bytes}, {Count: 2, Bytes: bytes, Expanded: bytes - 1}} {
		if err := validateDocumentCommentBudget(text, limits); err == nil {
			t.Fatal("aggregate limit bypassed", limits)
		}
	}
	if err := validateDocumentCommentBudget(strings.Repeat("body", 10000), documentCommentLimits{}); err != nil {
		t.Fatal("non-comment text charged", err)
	}
	if err := validateDocumentCommentBudget("<!-- "+strings.Repeat("x", 100), documentCommentLimits{Count: 1, Bytes: 20, Expanded: 20}); err == nil {
		t.Fatal("unterminated comment bypassed byte budget")
	}
}

func TestDocumentCommentBudgetFenceAndCommentScope(t *testing.T) {
	limits := documentCommentLimits{Count: 1, Bytes: 64, Expanded: 64}
	for _, fence := range []string{"````md", "~~~~md"} {
		close := fence[:4]
		text := fence + "\n" + strings.Repeat("<!-- huge -->\n", 100) + "```\n<!-- literal -->\n" + close + "\n<!-- real -->"
		if err := validateDocumentCommentBudget(text, limits); err != nil {
			t.Fatal("literal code charged", fence, err)
		}
	}
	if err := validateDocumentCommentBudget("<!--\n```\n-->\n<!-- literal -->\n```\n<!-- real -->", limits); err == nil {
		t.Fatal("comment state hid metadata after a fenced block")
	}
	if err := validateDocumentCommentBudget("<!-- one --><!-- two -->", limits); err == nil {
		t.Fatal("adjacent comments collapsed")
	}
	if err := validateDocumentCommentBudget("<!--<!-->", limits); err == nil {
		t.Fatal("overlapping delimiter hid an open marker")
	}
}

func TestDocumentCommentBudgetAggregateExpansion(t *testing.T) {
	for _, name := range []string{"keynope-assets", "keynope-objects", "keynope-engagement-results"} {
		comment := compressedCommentFixture(t, name, strings.Repeat("x", 600))
		limits := documentCommentLimits{Count: 10, Bytes: 4096, Expanded: int64(len(comment)*2 + 1000)}
		if err := validateDocumentCommentBudget(comment, limits); err != nil {
			t.Fatal("single bounded archive rejected", name, err)
		}
		if err := validateDocumentCommentBudget(comment+"\n"+comment, limits); err == nil {
			t.Fatal("compressed aggregate bypassed", name)
		}
		if err := validateDocumentCommentBudget("<!-- ignored\n"+comment+"\n<!-- ignored\n"+comment, limits); err == nil {
			t.Fatal("nested metadata bypassed expansion budget", name)
		}
		if err := validateDocumentCommentBudget("```\n"+comment+"\n"+comment+"\n```", documentCommentLimits{}); err != nil {
			t.Fatal("fenced compressed samples inflated", err)
		}
	}
	// Unknown/future formats remain opaque; their encoded bytes still count.
	future := compressedCommentFixture(t, "keynope-future", strings.Repeat("x", 4096))
	if err := validateDocumentCommentBudget(future, documentCommentLimits{Count: 1, Bytes: 4096, Expanded: 4096}); err != nil {
		t.Fatal("future payload interpreted", err)
	}
}

func TestCommentBudgetRejectedDocumentKeepsRuntimeAndPixels(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("APP_SANDBOX_CONTAINER_ID", "")
	w, h, order := authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged
	defer func() { authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = w, h, order }()
	id := "comment-admission-asset"
	asset := DeckAsset{MIME: "image/png", Width: 8, Height: 8, Data: identityTestPNG(t, color.NRGBA{R: 255, A: 255})}
	registerEmbeddedStillAsset(id, asset)
	t.Cleanup(func() {
		embeddedStillAssets.Lock()
		delete(embeddedStillAssets.assets, id)
		delete(embeddedStillAssets.identities, id)
		embeddedStillAssets.Unlock()
	})
	identity, revision := embeddedImageCacheIdentity("keynope-asset:"+id+".png"), embeddedImageRegistrationRevision.Load()
	asset.Data = identityTestPNG(t, color.NRGBA{G: 255, A: 255})
	archive, err := encodeDeckAssets(map[string]DeckAsset{id: asset})
	if err != nil {
		t.Fatal(err)
	}
	authoredTerminalWidth, authoredTerminalHeight, parsedDeckElementOrderChanged = 132, 41, true
	text := "<!-- keynope width=245 height=56 -->\n" + archive + "\n" + strings.Repeat("<!-- x -->\n", defaultDocumentCommentLimits.Count+1) + "Text"
	if _, err := parseEditorDeckData("bad.md", []byte(text)); err == nil || !strings.Contains(err.Error(), "metadata comments") {
		t.Fatal("over-count document accepted", err)
	}
	if authoredTerminalWidth != 132 || authoredTerminalHeight != 41 || !parsedDeckElementOrderChanged || embeddedImageRegistrationRevision.Load() != revision || embeddedImageCacheIdentity("keynope-asset:"+id+".png") != identity {
		t.Fatal("preflight failure changed runtime")
	}
	if _, err := os.Stat(embeddedAssetPath(id, asset)); !os.IsNotExist(err) {
		t.Fatal("rejected comment document materialized asset", err)
	}
}

func TestCommentBudgetSaveReturnsNoUnreadablePayload(t *testing.T) {
	useTestAuthoredSize(t, 245, 56)
	d := Deck{Slides: []Slide{{Elements: []Element{{ID: "text", Kind: "text", Text: "Keep", Query: "render=truetype&top=2"}}}}}
	before := cloneDeck(d)
	if data, err := serializeDeckWithCommentLimits("small.md", d, documentCommentLimits{Count: 1, Bytes: 4096, Expanded: 4096}); err == nil || data != nil {
		t.Fatal("save returned over-budget metadata", len(data), err)
	}
	if _, err := serializeDeck("normal.md", d); err != nil {
		t.Fatal("request-local quota poisoned later save", err)
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("comment admission modified authored source on save")
	}
}
