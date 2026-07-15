package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMacPresenterHelperUsesAppBundleExecutable(t *testing.T) {
	base := filepath.Join("opt", "homebrew", "libexec", "keynope")
	candidates := macPresenterHelperCandidates(base)
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d", len(candidates))
	}
	want := filepath.Join(base, "KeynopePresenter.app", "Contents", "MacOS", "KeynopePresenter")
	if candidates[0].bundle != filepath.Join(base, "KeynopePresenter.app") || candidates[0].executable != want {
		t.Fatalf("presenter executable = %q, want %q", candidates[0].executable, want)
	}
}

func TestMacPresenterHelperResolvesHomebrewLibexecSymlink(t *testing.T) {
	root := t.TempDir()
	privateDir := filepath.Join(root, "libexec", "keynope")
	helper := filepath.Join(privateDir, "KeynopePresenter.app", "Contents", "MacOS", "KeynopePresenter")
	if err := os.MkdirAll(filepath.Dir(helper), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("helper"), 0o755); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(privateDir, "keynope")
	if err := os.WriteFile(cli, []byte("cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedCLI := filepath.Join(binDir, "keynope")
	if err := os.Symlink(cli, linkedCLI); err != nil {
		t.Fatal(err)
	}
	resolvedHelper, err := filepath.EvalSymlinks(helper)
	if err != nil {
		t.Fatal(err)
	}

	found, ok := findMacPresenterHelperFrom(linkedCLI, "")
	if !ok || found.executable != resolvedHelper {
		t.Fatalf("found=%#v ok=%v, want %q", found, ok, resolvedHelper)
	}
}

func TestMacPresenterLaunchUsesLaunchServicesAndTracksParent(t *testing.T) {
	got := macPresenterLaunchArgs("/opt/keynope/KeynopePresenter.app", "http://127.0.0.1:1234/", 42)
	want := []string{
		"-g", "-n", "-W", "/opt/keynope/KeynopePresenter.app",
		"--args", "http://127.0.0.1:1234/", "--parent-pid", "42",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("launch args = %#v, want %#v", got, want)
	}
}

func TestParseAppMode(t *testing.T) {
	args, ok := parseArgs([]string{"--app", "deck.md"})
	if !ok || !args.AppMode || args.DeckPath != "deck.md" {
		t.Fatalf("args = %#v, ok = %v", args, ok)
	}
	if _, ok := parseArgs([]string{"--app", "--classic", "deck.md"}); ok {
		t.Fatal("app and classic modes must be mutually exclusive")
	}
}

func TestCompanionAuthorizationEstablishesSession(t *testing.T) {
	companion := &presenterCompanion{token: "test-token"}
	handler := companion.authorized(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/?token=test-token", nil))
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authorized status = %d", authorized.Code)
	}
	if len(authorized.Result().Cookies()) != 1 || authorized.Result().Cookies()[0].Value != "test-token" {
		t.Fatalf("session cookies = %#v", authorized.Result().Cookies())
	}
}

func TestTerminalFrameSubscribersReceiveLatestFrame(t *testing.T) {
	updates := make(chan presenterTerminalFrame, 1)
	companion := &presenterCompanion{frames: map[chan presenterTerminalFrame]<-chan struct{}{updates: make(chan struct{})}}
	companion.PublishTerminalFrame("\033[2J\033[Hhello", 80, 25)
	select {
	case frame := <-updates:
		if frame.Version != 1 || frame.Cols != 80 || frame.Rows != 25 {
			t.Fatalf("frame = %#v", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal frame")
	}
}

func TestAxisScalingCapabilitiesExcludeText(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		status string
		want   bool
	}{
		{name: "text", kind: "text", status: "text selected", want: false},
		{name: "heading", kind: "heading", status: "text selected", want: false},
		{name: "code", kind: "code", status: "code text selected", want: false},
		{name: "image", kind: "image", status: "image selected", want: true},
		{name: "shape", kind: "shape", status: "shape selected", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			slide := Slide{Elements: []Element{{Kind: test.kind}}}
			ctx := interactionContextFor("select", slide, 0, nil, test.status)
			if got := actionSpecsAllow(editActionSpecs(ctx), "shape-toggle"); got != test.want {
				t.Fatalf("axis scale action = %v, want %v", got, test.want)
			}
		})
	}
}

func TestShortcutHelpDismissOrReplay(t *testing.T) {
	dismissed := []KeyEvent{
		{},
		{Action: "controls"},
		{Action: "escape"},
		{Action: "enter"},
		{Action: "shortcuts"},
		{Action: "text", Text: "?"},
	}
	for _, event := range dismissed {
		if !shortcutHelpDismissed(event) {
			t.Fatalf("expected %#v to dismiss help", event)
		}
	}
	for _, event := range []KeyEvent{{Action: "export"}, {Action: "copy"}, {Action: "color"}} {
		if shortcutHelpDismissed(event) {
			t.Fatalf("expected %#v to replay", event)
		}
	}
}

func TestSelectionActionMatrix(t *testing.T) {
	tests := []struct {
		kind       selectionKind
		allowed    []string
		disallowed []string
	}{
		{selectionText, []string{"enter", "promote", "color", "style", "outline", "rotate", "link", "copy"}, []string{"shape-toggle", "layer-back", "transparency"}},
		{selectionCode, []string{"enter", "color", "outline"}, []string{"up", "promote", "style", "copy", "shape-toggle"}},
		{selectionImage, []string{"up", "promote", "shape-toggle", "style", "outline", "layer-back", "link", "copy"}, []string{"color", "rotate", "transparency"}},
		{selectionShape, []string{"up", "promote", "shape-toggle", "color", "style", "outline", "layer-back", "link", "transparency", "copy"}, []string{"rotate"}},
		{selectionMulti, []string{"up", "shift-right", "align-center", "copy", "cut", "backspace"}, []string{"promote", "demote", "shape-toggle", "style", "color"}},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			specs := editActionSpecs(interactionContext{Mode: editorModeSelect, Selection: test.kind})
			for _, action := range test.allowed {
				if !actionSpecsAllow(specs, action) {
					t.Errorf("expected %q to be allowed", action)
				}
			}
			for _, action := range test.disallowed {
				if actionSpecsAllow(specs, action) {
					t.Errorf("expected %q to be disallowed", action)
				}
			}
		})
	}
}

func TestActionGateDoesNotLeakClipboardIntoCodeSelection(t *testing.T) {
	ctx := interactionContext{Mode: editorModeSelect, Selection: selectionCode}
	if actionAllowedInContext(ctx, "paste") {
		t.Fatal("code selection unexpectedly allows paste")
	}
	if !actionAllowedInContext(interactionContext{Mode: editorModeSelect, Selection: selectionNone}, "paste") {
		t.Fatal("empty selection should allow paste")
	}
}

func TestPrintableTextKeepsShortcutCharactersLiteral(t *testing.T) {
	event := printableKeyEvent([]byte("qgis?+/-"))
	if event.Action != "text" || event.Text != "qgis?+/-" {
		t.Fatalf("event = %#v", event)
	}
}

func TestAdaptiveToolbarFitsWholeSegments(t *testing.T) {
	left := toolbarSegmentsFromActions(mainActionSpecs())
	left = append(left, toolbarSegment{Long: "? shortcuts", Short: "?", Required: true})
	right := []toolbarSegment{
		{Long: "PRES: External / Live", Short: "P:E/L", Required: true},
		{Long: "1/14", Short: "1/14", Required: true},
	}
	known := map[string]bool{"": true}
	for _, segment := range append(append([]toolbarSegment{}, left...), right...) {
		known[segment.Long] = true
		known[segment.Short] = true
	}
	for _, width := range []int{245, 80, 40, 20, 5, 1} {
		leftText, rightText := fitToolbarSegments(width, left, right)
		gap := 0
		if leftText != "" && rightText != "" {
			gap = 2
		}
		if got := displayWidth(leftText) + gap + displayWidth(rightText); got > width {
			t.Fatalf("width %d produced %d columns: %q | %q", width, got, leftText, rightText)
		}
		for _, text := range []string{leftText, rightText} {
			for _, part := range strings.Split(text, "  ") {
				if !known[part] {
					t.Fatalf("width %d produced partial/unknown segment %q", width, part)
				}
			}
		}
	}
}

func TestOverlayLoopCommit(t *testing.T) {
	draws := 0
	reads := 0
	decision := runOverlayLoop(overlayLoopSpec{
		Draw: func(frame int) { draws++ },
		Read: func() KeyEvent {
			reads++
			if reads < 2 {
				return KeyEvent{}
			}
			return KeyEvent{Action: "enter"}
		},
		Handle: func(event KeyEvent) overlayDecision {
			return overlayDecision{Disposition: overlayCommit}
		},
	})
	if decision.Disposition != overlayCommit || draws == 0 {
		t.Fatalf("decision=%v draws=%d", decision.Disposition, draws)
	}
}

func TestPresenterStatusHandler(t *testing.T) {
	previousNativeAppMode := nativeAppModeActive
	nativeAppModeActive = true
	defer func() { nativeAppModeActive = previousNativeAppMode }()

	companion := &presenterCompanion{}
	request := httptest.NewRequest(http.MethodPost, "/presenter-status", strings.NewReader(`{"mode":"external"}`))
	recorder := httptest.NewRecorder()
	companion.handlePresenterStatus(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
	available, target, live := companion.Status()
	if !available || target != "external" || !live {
		t.Fatalf("available=%v target=%q live=%v", available, target, live)
	}

	request = httptest.NewRequest(http.MethodPost, "/presenter-status", strings.NewReader(`{"mode":"none"}`))
	recorder = httptest.NewRecorder()
	companion.handlePresenterStatus(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("none status = %d", recorder.Code)
	}
	_, target, live = companion.Status()
	if target != "none" || live {
		t.Fatalf("stopped target=%q live=%v", target, live)
	}

	request = httptest.NewRequest(http.MethodPost, "/presenter-status", strings.NewReader(`{"mode":"projector"}`))
	recorder = httptest.NewRecorder()
	companion.handlePresenterStatus(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid mode status = %d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/presenter-status", nil)
	recorder = httptest.NewRecorder()
	companion.handlePresenterStatus(recorder, request)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d", recorder.Code)
	}
}

func TestPresenterStatusLabels(t *testing.T) {
	previousMode, previousPresenter := presenterModeActive, activePresenter
	defer func() {
		presenterModeActive = previousMode
		activePresenter = previousPresenter
	}()

	presenterModeActive = false
	activePresenter = nil
	if label, _ := presenterStatusLabels(); label != "" {
		t.Fatalf("classic label = %q", label)
	}

	presenterModeActive = true
	if label, _ := presenterStatusLabels(); label != "PRES: Unavailable" {
		t.Fatalf("unavailable label = %q", label)
	}

	activePresenter = &presenterCompanion{helper: true, target: "external", state: presenterState{Presenting: true}}
	if label, _ := presenterStatusLabels(); label != "PRES: External / Live" {
		t.Fatalf("external label = %q", label)
	}

	activePresenter = &presenterCompanion{helper: true, target: "main"}
	if label, _ := presenterStatusLabels(); label != "PRES: Main / Signal off" {
		t.Fatalf("main label = %q", label)
	}
}

func TestMultiSelectionContextLabel(t *testing.T) {
	slide := Slide{Elements: []Element{{Kind: "image"}, {Kind: "shape"}}}
	if got := editContextLabel("select", "2 selected", slide, 0, ""); got != "SELECT · 2 / Image active" {
		t.Fatalf("label = %q", got)
	}
}

func TestErrorNoticeOutlivesSuccessNotice(t *testing.T) {
	setUINotice("ok")
	successExpiry := activeUINotice.ExpiresAt
	setUIError("failed")
	if !activeUINotice.ExpiresAt.After(successExpiry.Add(3 * time.Second)) {
		t.Fatalf("error expiry %v should be substantially later than success %v", activeUINotice.ExpiresAt, successExpiry)
	}
}

func TestParseMarkdownStyledSpans(t *testing.T) {
	got := parseMarkdownStyledSpans("plain *highlight* and **bold**")
	want := []styledTextSpan{
		{Text: "plain "},
		{Text: "highlight", Highlight: true},
		{Text: " and "},
		{Text: "bold", Bold: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans = %#v, want %#v", got, want)
	}

	unmatched := parseMarkdownStyledSpans("keep *this and **that")
	if len(unmatched) != 1 || unmatched[0].Text != "keep *this and **that" || unmatched[0].Bold || unmatched[0].Highlight {
		t.Fatalf("unmatched markers changed: %#v", unmatched)
	}

	combined := parseMarkdownStyledSpans("***both***")
	if len(combined) != 1 || combined[0] != (styledTextSpan{Text: "both", Bold: true, Highlight: true}) {
		t.Fatalf("combined emphasis = %#v", combined)
	}
}

func TestToggleMarkdownStyle(t *testing.T) {
	if got := toggleMarkdownStyle("hello", "**"); got != "**hello**" {
		t.Fatalf("bold on = %q", got)
	}
	if got := toggleMarkdownStyle("**hello**", "**"); got != "hello" {
		t.Fatalf("bold off = %q", got)
	}
	if got := toggleMarkdownStyle("*hello*", "**"); got != "***hello***" {
		t.Fatalf("combined style = %q", got)
	}
	if got := toggleMarkdownStyle("***hello***", "*"); got != "**hello**" {
		t.Fatalf("highlight off = %q", got)
	}
}

func TestTextSelectionAllowsMarkdownStyleToggles(t *testing.T) {
	text := interactionContext{Mode: editorModeSelect, Selection: selectionText}
	if !actionAllowedInContext(text, "toggle-bold") || !actionAllowedInContext(text, "toggle-highlight") {
		t.Fatal("text selection does not allow Markdown style toggles")
	}
	for _, selection := range []selectionKind{selectionCode, selectionImage, selectionShape, selectionPage, selectionMulti} {
		ctx := interactionContext{Mode: editorModeSelect, Selection: selection}
		if actionAllowedInContext(ctx, "toggle-bold") || actionAllowedInContext(ctx, "toggle-highlight") {
			t.Fatalf("selection %q unexpectedly allows Markdown style toggles", selection)
		}
	}
}

func TestQuadMarkdownUsesBoldGlyphsAndHighlightColor(t *testing.T) {
	normal := renderQuadRaw("A")
	bold := renderQuadStyled([]styledTextSpan{{Text: "A", Bold: true}})
	highlight := renderQuadStyled([]styledTextSpan{{Text: "A", Highlight: true}})

	if strings.Contains(strings.Join(bold, ""), "\033[1m") {
		t.Fatal("bold glyph unexpectedly uses ANSI highlight")
	}
	if reflect.DeepEqual(bold, normal) {
		t.Fatal("bold glyph is identical to the normal glyph")
	}
	if maxLineDisplayWidth(bold) != 5 {
		t.Fatalf("bold glyph width = %d, want 5", maxLineDisplayWidth(bold))
	}
	if !strings.Contains(strings.Join(highlight, ""), "\033[1m") {
		t.Fatal("highlight does not use the brighter foreground treatment")
	}
	for i := range normal {
		if stripANSI(highlight[i]) != normal[i] {
			t.Fatalf("highlight row %d changed glyph: %q != %q", i, stripANSI(highlight[i]), normal[i])
		}
	}
}

func TestStyledWrappingMeasuresBoldGlyphWidth(t *testing.T) {
	spans := parseMarkdownStyledSpans("**AA**")
	lines := wrapStyledSpans(spans, 5, 4, 5)
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(lines))
	}
	for i, line := range lines {
		if width := styledSpansWidth(line, 4, 5); width != 5 {
			t.Fatalf("line %d width = %d, want 5", i, width)
		}
	}
}

func TestBitmapTextImageRendersMarkdownStylesWithoutMarkerGlyphs(t *testing.T) {
	query := "render=text-image&source=bitmap&scale=1.00&text-size=1"
	normal := renderTextImageElement(Element{Kind: "text", Text: "A", Query: query}, 80)
	bold := renderTextImageElement(Element{Kind: "text", Text: "**A**", Query: query}, 80)
	highlight := renderTextImageElement(Element{Kind: "text", Text: "*A*", Query: query}, 80)
	if normalWidth, boldWidth := maxLineDisplayWidth(normal), maxLineDisplayWidth(bold); boldWidth <= normalWidth || boldWidth >= normalWidth*2 {
		t.Fatalf("bitmap Markdown widths normal=%d bold=%d", normalWidth, boldWidth)
	}
	if !strings.Contains(strings.Join(highlight, ""), "\033[1m") {
		t.Fatal("bitmap highlight is missing the brighter foreground treatment")
	}
	for row := range normal {
		if row >= len(highlight) || stripANSI(highlight[row]) != normal[row] {
			t.Fatalf("bitmap highlight row %d changed glyph", row)
		}
	}
}

func TestRemovedImageGlyphStylesAreNotAcceptedOrOffered(t *testing.T) {
	for _, glyph := range []string{"half", "vertical"} {
		if got := parseImageASCIIOptions("glyph=" + glyph).glyph; got != "blocks" {
			t.Fatalf("removed glyph %q parsed as %q, want blocks", glyph, got)
		}
		for _, field := range append(imageSettingFields, textSettingFields...) {
			if field.Key == "glyph" && slicesContain(field.Values, glyph) {
				t.Fatalf("CLI settings still offer removed glyph %q", glyph)
			}
		}
	}
}

func TestTransparentTextExportsAsSelectableTransparencyMask(t *testing.T) {
	slide := Slide{Elements: []Element{{Kind: "text", Text: "Hi", Query: "transparent=1"}}}
	lines := []Line{{Text: "Hi", Role: "body", Row: 2, Col: 3, Element: 0, Query: "transparent=1"}}
	exported := exportLines(lines, slide, 20, 10, 1)
	if len(exported) != 1 || exported[0].Role != "transparent-text" {
		t.Fatalf("transparent text export = %#v, want selectable transparent-text source", exported)
	}
	mask := transparentShapeExportLines(lines, 20, 10, slide)
	if len(mask) != 1 || mask[0].Row != 2 || len(mask[0].Parts) == 0 {
		t.Fatalf("transparent text mask = %#v, want row 2 mask", mask)
	}
}

func TestTransparentTextMaskUsesResolvedTextColour(t *testing.T) {
	redBG, _ := ansiBG("red")
	if got := transparentElementBG(Slide{}, Element{Kind: "text", Query: "fg=red"}, "body"); got != redBG {
		t.Fatalf("transparent body background = %q, want %q", got, redBG)
	}
	blueBG, _ := ansiBG("blue")
	if got := transparentElementBG(Slide{}, Element{Kind: "heading", Query: "header=blue"}, "heading"); got != blueBG {
		t.Fatalf("transparent heading background = %q, want %q", got, blueBG)
	}
	slide := Slide{FG: "32", HeaderFG: "35"}
	if got := transparentElementBG(slide, Element{Kind: "heading"}, "heading"); got != "45" {
		t.Fatalf("transparent inherited heading background = %q, want 45", got)
	}
}

func TestTransparentImageExportsAsSelectableTransparencyMask(t *testing.T) {
	slide := Slide{Elements: []Element{{Kind: "image", Query: "transparent=1"}}}
	lines := []Line{{Text: "██", Role: "image", Row: 2, Col: 3, Element: 0, Query: "transparent=1"}}
	exported := exportLines(lines, slide, 20, 10, 1)
	if len(exported) != 1 || exported[0].Role != "transparent-image" {
		t.Fatalf("transparent image export = %#v, want selectable transparent-image source", exported)
	}
	mask := transparentShapeExportLines(lines, 20, 10, slide)
	if len(mask) != 1 || mask[0].Row != 2 || len(mask[0].Parts) == 0 {
		t.Fatalf("transparent image mask = %#v, want row 2 mask", mask)
	}
}

func TestBulletElementSavesWithMarkdownDash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bullet.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "bullet", Text: "Converted text"}}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "- Converted text\n") {
		t.Fatalf("saved bullet does not use Markdown dash: %q", data)
	}
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
