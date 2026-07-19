package main

import (
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestNativeInputReaderSignalsOwnerPipeClosure(t *testing.T) {
	nativeInputMu.Lock()
	previous := nativeInputBuffer.String()
	nativeInputBuffer.Reset()
	nativeInputMu.Unlock()
	defer func() {
		nativeInputMu.Lock()
		nativeInputBuffer.Reset()
		_, _ = nativeInputBuffer.WriteString(previous)
		nativeInputMu.Unlock()
	}()

	closed := startNativeInputReader(strings.NewReader("editor input"))
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("native input reader did not signal EOF")
	}

	nativeInputMu.Lock()
	got := nativeInputBuffer.String()
	nativeInputMu.Unlock()
	if got != "editor input" {
		t.Fatalf("buffered native input = %q, want %q", got, "editor input")
	}
}

func TestSandboxedAppUsesContainerImageCache(t *testing.T) {
	t.Setenv("APP_SANDBOX_CONTAINER_ID", "sh.keynope.app")
	got := imageCacheDirectory()
	if got == filepath.Join(".keynope", "cache") || !filepath.IsAbs(got) {
		t.Fatalf("sandboxed image cache = %q, want absolute container cache path", got)
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
	untitled, ok := parseArgs([]string{"--app", "deck.md", "--untitled"})
	if !ok || !untitled.AppMode || !untitled.Untitled {
		t.Fatalf("untitled args = %#v, ok = %v", untitled, ok)
	}
	if _, ok := parseArgs([]string{"deck.md", "--untitled"}); ok {
		t.Fatal("untitled mode requires the windowed app")
	}
}

func TestParseLicensesMode(t *testing.T) {
	args, ok := parseArgs([]string{"--licenses"})
	if !ok || !args.Licenses || args.DeckPath != "" {
		t.Fatalf("args = %#v, ok = %v", args, ok)
	}
	for _, invalid := range [][]string{{"--licenses", "deck.md"}, {"--licenses", "--app"}, {"--licenses", "--export"}} {
		if _, ok := parseArgs(invalid); ok {
			t.Fatalf("licenses accepted incompatible arguments %v", invalid)
		}
	}
}

func TestBundledLicensesContainProgramAndEmojiNotices(t *testing.T) {
	licenses := bundledLicenseText()
	for _, required := range []string{
		"===== KEYNOPE =====",
		"MIT License",
		"Keynope Emoji Glyphs",
		"Copyright 2013 Google LLC",
		"SIL OPEN FONT LICENSE Version 1.1",
		"UNICODE LICENSE V3",
	} {
		if !strings.Contains(licenses, required) {
			t.Fatalf("bundled license output is missing %q", required)
		}
	}
}

func TestParseDeckDefaultsMissingAuthoredSize(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	t.Cleanup(func() {
		authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight
	})
	path := filepath.Join(t.TempDir(), "deck.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseDeck(path); err != nil {
		t.Fatal(err)
	}
	ensureDefaultAuthoredSize()
	if authoredTerminalWidth != 245 || authoredTerminalHeight != 56 {
		t.Fatalf("authored size = %dx%d, want 245x56", authoredTerminalWidth, authoredTerminalHeight)
	}
}

func TestParseDeckPreservesExplicitAuthoredSize(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	t.Cleanup(func() {
		authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight
	})
	path := filepath.Join(t.TempDir(), "deck.md")
	content := "<!-- keynope width=132 height=41 -->\n\n# Title\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseDeck(path); err != nil {
		t.Fatal(err)
	}
	ensureDefaultAuthoredSize()
	if authoredTerminalWidth != 132 || authoredTerminalHeight != 41 {
		t.Fatalf("authored size = %dx%d, want 132x41", authoredTerminalWidth, authoredTerminalHeight)
	}
}

func TestExternalImageSourceRemainsExternal(t *testing.T) {
	slide := parseSlide("![image](https://example.com/image.png)", t.TempDir())
	if len(slide.Elements) != 1 || slide.Elements[0].Kind != "image" {
		t.Fatalf("elements = %#v", slide.Elements)
	}
	if slide.Elements[0].Path != "https://example.com/image.png" {
		t.Fatalf("external image path = %q", slide.Elements[0].Path)
	}
}

func TestUnavailableImagesUseStandardTextPlaceholder(t *testing.T) {
	want := renderBodyWrapped("[IMG]", 80, "", "")
	existing := filepath.Join(t.TempDir(), "existing.png")
	if err := os.WriteFile(existing, []byte("available but referenced"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, element := range []Element{
		{Kind: "image", Path: filepath.Join(t.TempDir(), "missing.png")},
		{Kind: "image", Path: existing},
		{Kind: "image", Path: "https://example.com/image.png"},
	} {
		if got := renderImageElementRows(element, 80, 25); !reflect.DeepEqual(got, want) {
			t.Fatalf("placeholder rows = %#v, want %#v", got, want)
		}
	}
}

func TestBundledWelcomeDeckParses(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	t.Cleanup(func() {
		authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight
	})
	deck, err := parseDeck(filepath.Join("app", "Welcome.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(deck.Slides) != 1 {
		t.Fatalf("welcome slide count = %d, want 1", len(deck.Slides))
	}
	if authoredTerminalWidth != 245 || authoredTerminalHeight != 56 {
		t.Fatalf("welcome authored size = %dx%d", authoredTerminalWidth, authoredTerminalHeight)
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
		{selectionText, []string{"enter", "promote", "color", "style", "outline", "rotate", "link", "copy", "align-left", "align-center", "align-right"}, []string{"shape-toggle", "layer-back"}},
		{selectionCode, []string{"enter", "color", "outline"}, []string{"up", "promote", "style", "copy", "shape-toggle"}},
		{selectionImage, []string{"up", "promote", "shape-toggle", "style", "outline", "layer-back", "link", "copy", "align-left", "align-center", "align-right"}, []string{"color", "rotate"}},
		{selectionShape, []string{"up", "promote", "shape-toggle", "color", "style", "outline", "layer-back", "link", "copy", "align-left", "align-center", "align-right"}, []string{"rotate"}},
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

func TestSeeThroughActionOnlyAppearsForBlockGlyphElements(t *testing.T) {
	tests := []struct {
		name    string
		element Element
		status  string
		want    bool
	}{
		{name: "default block text", element: Element{Kind: "text"}, status: "text selected", want: true},
		{name: "block text", element: Element{Kind: "text", Query: "glyph=blocks"}, status: "text selected", want: true},
		{name: "block image", element: Element{Kind: "image", Path: "still.png", Query: "glyph=blocks"}, status: "image selected", want: true},
		{name: "block shape", element: Element{Kind: "shape", Query: "glyph=blocks"}, status: "shape selected", want: true},
		{name: "braille text", element: Element{Kind: "text", Query: "glyph=braille"}, status: "text selected"},
		{name: "ascii image", element: Element{Kind: "image", Path: "still.png", Query: "glyph=ascii"}, status: "image selected"},
		{name: "ascii shape", element: Element{Kind: "shape", Query: "glyph=ascii"}, status: "shape selected"},
		{name: "animated gif", element: Element{Kind: "image", Path: "animated.gif", Query: "glyph=blocks"}, status: "image selected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			slide := Slide{Elements: []Element{test.element}}
			ctx := interactionContextFor("select", slide, 0, nil, test.status)
			if got := actionSpecsAllow(editActionSpecs(ctx), "transparency"); got != test.want {
				t.Fatalf("see-through action = %v, want %v for %#v", got, test.want, test.element)
			}
		})
	}
}

func TestEmptySelectionUsesSForShapePicker(t *testing.T) {
	specs := editActionSpecs(interactionContext{Mode: editorModeSelect, Selection: selectionNone})
	if !actionSpecsAllow(specs, "shape-picker") {
		t.Fatal("empty selection does not allow shape picker")
	}
	if actionSpecsAllow(specs, "style") {
		t.Fatal("empty selection unexpectedly routes s to style")
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

func TestFittedTerminalContentGeometryUsesUniformScale(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight
	}()
	authoredTerminalWidth, authoredTerminalHeight = 245, 56

	tests := []struct {
		name                     string
		width, height            int
		wantWidth, wantHeight    int
		wantOffsetX, wantOffsetY int
		wantFitted               bool
	}{
		{name: "width constrained", width: 180, height: 56, wantWidth: 180, wantHeight: 41, wantOffsetY: 7, wantFitted: true},
		{name: "height constrained", width: 245, height: 40, wantWidth: 175, wantHeight: 40, wantOffsetX: 35, wantFitted: true},
		{name: "both fit", width: 300, height: 70, wantWidth: 245, wantHeight: 56},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := fittedTerminalContentGeometry(test.width, test.height)
			if got.Width != test.wantWidth || got.Height != test.wantHeight || got.OffsetX != test.wantOffsetX || got.OffsetY != test.wantOffsetY || got.Fitted != test.wantFitted {
				t.Fatalf("geometry = %#v, want %dx%d offset %d,%d fitted=%v", got, test.wantWidth, test.wantHeight, test.wantOffsetX, test.wantOffsetY, test.wantFitted)
			}
			if got.Fitted {
				scaleX := float64(got.Width) / 245
				scaleY := float64(got.Height) / 56
				if math.Abs(scaleX-scaleY) > 0.02 {
					t.Fatalf("non-uniform scale: x=%.3f y=%.3f", scaleX, scaleY)
				}
			}
		})
	}
}

func TestFittedDisplayLinesStayInsidePhysicalViewport(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight
	}()
	authoredTerminalWidth, authoredTerminalHeight = 245, 56

	slide := Slide{Elements: []Element{
		{Kind: "heading", Level: 1, Text: "RIGHT EDGE", Query: "align=right"},
		{Kind: "shape", Query: "shape=square&right=0&bottom=0&width=24&height=8"},
	}}
	const width, height = 120, 32
	for _, line := range displayLines(slide, width, height, 0) {
		if line.Row < 0 || line.Row >= height {
			t.Fatalf("line row %d outside height %d", line.Row, height)
		}
		if line.Col < 0 || line.Col >= width {
			t.Fatalf("line column %d outside width %d", line.Col, width)
		}
		if end := line.Col + displayWidth(stripANSI(line.Text)); end > width {
			t.Fatalf("line ends at column %d beyond width %d: %#v", end, width, line)
		}
	}
}

func TestVerticalAlignmentPlacesElementsRelativeToSlide(t *testing.T) {
	const width, height = 80, 25
	for _, test := range []struct {
		name    string
		element Element
		wantTop int
	}{
		{name: "top", element: Element{Kind: "text", Text: "top", Query: "valign=top"}, wantTop: 0},
		{name: "middle", element: Element{Kind: "text", Text: "middle", Query: "valign=middle"}, wantTop: 10},
		{name: "bottom", element: Element{Kind: "text", Text: "bottom", Query: "valign=bottom"}, wantTop: 21},
	} {
		t.Run(test.name, func(t *testing.T) {
			top, bottom, _, _, ok := elementFullBox(Slide{Elements: []Element{test.element}}, 0, width, height)
			if !ok || top != test.wantTop || bottom != test.wantTop+3 {
				t.Fatalf("vertical bounds = top:%d bottom:%d ok=%v, want rows %d-%d", top, bottom, ok, test.wantTop, test.wantTop+3)
			}
		})
	}
}

func TestLargerTerminalKeepsSlideInsideAuthoredViewport(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight
	}()
	authoredTerminalWidth, authoredTerminalHeight = 80, 25

	slide := Slide{Elements: []Element{
		{Kind: "shape", Query: "shape=square&right=0&bottom=0&width=12&height=6"},
	}}
	lines := displayLines(slide, 120, 40, 0)
	if len(lines) == 0 {
		t.Fatal("expected rendered shape")
	}
	for _, line := range lines {
		if line.Row < 0 || line.Row >= 25 {
			t.Fatalf("line row %d escaped authored height", line.Row)
		}
		if line.Col < 0 || line.Col+displayWidth(stripANSI(line.Text)) > 80 {
			t.Fatalf("line escaped authored width: %#v", line)
		}
	}
	if got := elementAtPoint(lines, 100, 10); got != -1 {
		t.Fatalf("outside-authored click selected element %d", got)
	}
}

func TestSlideCanvasBackgroundDoesNotPaintTerminalMargins(t *testing.T) {
	previousWidth, previousHeight := authoredTerminalWidth, authoredTerminalHeight
	previousFrame := terminalFrame
	defer func() {
		authoredTerminalWidth, authoredTerminalHeight = previousWidth, previousHeight
		terminalFrame = previousFrame
	}()
	authoredTerminalWidth, authoredTerminalHeight = 80, 25

	slide := Slide{Background: "mesh", BG: "44", BGSet: true}
	var output strings.Builder
	terminalFrame = &output
	frame, geometry := drawTerminalSlideCanvas(slide, 120, 40, nil)
	terminalFrame = previousFrame
	if geometry.Width != 80 || geometry.Height != 25 {
		t.Fatalf("canvas geometry = %#v", geometry)
	}
	for _, line := range ansiFrameToExportLines(frame, 120, 40, "#ffffff") {
		if line.Row < geometry.OffsetY || line.Row >= geometry.OffsetY+geometry.Height {
			t.Fatalf("background row %d painted terminal margin", line.Row)
		}
		for _, part := range line.Parts {
			if part.Col < geometry.OffsetX || part.Col+displayWidth(part.Text) > geometry.OffsetX+geometry.Width {
				t.Fatalf("background part painted terminal margin: %#v", part)
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

	request = httptest.NewRequest(http.MethodPost, "/presenter-status", strings.NewReader(`{"mode":"external","paused":true}`))
	recorder = httptest.NewRecorder()
	companion.handlePresenterStatus(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("paused status = %d", recorder.Code)
	}
	_, target, live = companion.Status()
	if target != "external" || live {
		t.Fatalf("paused target=%q live=%v", target, live)
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

func TestParseInlineCodeSpansAndLeaveUnmatchedBacktickVisible(t *testing.T) {
	got := parseMarkdownStyledSpans("before `code text` after")
	want := []styledTextSpan{{Text: "before "}, {Text: "code text", Code: true}, {Text: " after"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inline-code spans = %#v, want %#v", got, want)
	}
	unmatched := parseMarkdownStyledSpans("code text`")
	if len(unmatched) != 1 || unmatched[0].Text != "code text`" || unmatched[0].Code {
		t.Fatalf("unmatched backtick was hidden: %#v", unmatched)
	}
}

func TestParseAndRenderInlineTextColour(t *testing.T) {
	spans := parseMarkdownStyledSpans("plain [color=#55aaff]blue[/color] text")
	if len(spans) != 3 || spans[1].Text != "blue" || spans[1].Color != "#55aaff" {
		t.Fatalf("colour spans = %#v", spans)
	}
	rendered := strings.Join(renderQuadStyled(spans), "")
	if !strings.Contains(rendered, "\033[38;2;85;170;255m") {
		t.Fatalf("colour span was not rendered with true colour: %q", rendered)
	}
}

func TestRenderBodyWrappedPreservesExplicitNewlines(t *testing.T) {
	rows := renderBodyWrapped("First\nSecond", 80, "", "")
	if len(rows) != 8 {
		t.Fatalf("multiline body rendered %d rows, want two four-row text lines", len(rows))
	}
	rows = renderBodyWrapped("First\n\nSecond", 80, "", "")
	if len(rows) != 12 {
		t.Fatalf("body with blank separator rendered %d rows, want three four-row lines", len(rows))
	}
}

func TestRenderStyledBodyPreservesColourAcrossExplicitNewline(t *testing.T) {
	spans := parseMarkdownStyledSpans("[color=#55aaff]First\nSecond[/color]")
	rows := renderStyledBodyWrapped(spans, 80, "", "")
	if len(rows) != 8 {
		t.Fatalf("coloured multiline body rendered %d rows, want two four-row text lines", len(rows))
	}
	for index, row := range rows {
		if !strings.Contains(row, "\x1b[38;2;85;170;255m") {
			t.Fatalf("multiline colour missing from row %d: %q", index, row)
		}
	}
}

func TestBulletRendersInlineColourMarkupAsStyledText(t *testing.T) {
	rows := renderBulletWrapped("before [color=#55aaff]blue[/color] after", 120)
	rendered := strings.Join(rows, "\n")
	if strings.Contains(rendered, "[color=") || strings.Contains(rendered, "[/color]") {
		t.Fatalf("bullet exposed inline colour markup: %q", rendered)
	}
	if !strings.Contains(rendered, "\x1b[38;2;85;170;255m") {
		t.Fatalf("bullet did not render selected text colour: %q", rendered)
	}
}

func TestRenderResizedTextPreservesExplicitNewlines(t *testing.T) {
	element := Element{Kind: "text", Text: "First\nSecond", Query: "render=text-image&source=bitmap&scale=2.00&text-size=10"}
	lineHeight := len(renderBitmapTextImage("M", 2))
	rows := renderTextImageElement(element, 160)
	if len(rows) != lineHeight*2 {
		t.Fatalf("resized multiline text rendered %d rows, want %d", len(rows), lineHeight*2)
	}
}

func TestInlineCodeRendersRoundedBackgroundField(t *testing.T) {
	rows := renderBodyWrapped("before `code` after", 160, "", "")
	if len(rows) != 4 {
		t.Fatalf("inline code rendered %d rows, want 4", len(rows))
	}
	if !strings.Contains(stripANSI(rows[0]), "▄") || !strings.Contains(stripANSI(rows[len(rows)-1]), "▀") {
		t.Fatalf("inline code is missing rounded half-block corners: %#v", rows)
	}
	backgroundFound := false
	for _, row := range rows {
		for _, part := range exportANSITextParts(row, 0, "#ffffff", 160) {
			if part.Background != "" {
				backgroundFound = true
			}
		}
	}
	if !backgroundFound {
		t.Fatal("inline code field has no exported background")
	}
}

func TestTrueColourForegroundChannelsAreNotParsedAsANSIBackground(t *testing.T) {
	parts := exportANSITextParts("\033[38;2;250;189;42m▙  \033[0m", 0, "#ffffff", 20)
	if len(parts) != 1 {
		t.Fatalf("exported %d parts, want 1: %#v", len(parts), parts)
	}
	if parts[0].Background != "" {
		t.Fatalf("true-colour foreground leaked background %q", parts[0].Background)
	}
}

func TestResizedCodeBlockScalesGlyphWithoutLosingBoxPadding(t *testing.T) {
	normal := renderCodeBlockRows(Element{Kind: "code", Text: "code"}, 100)
	resized := renderCodeBlockRows(Element{Kind: "code", Text: "code", Query: "render=text-image&source=bitmap&scale=2.00&text-size=10"}, 100)
	if len(resized) <= len(normal) {
		t.Fatalf("resized code block height = %d, normal = %d", len(resized), len(normal))
	}
	if strings.TrimSpace(resized[0]) != "" || strings.TrimSpace(resized[len(resized)-1]) != "" {
		t.Fatalf("resized code block lost its fixed outer padding: %#v", resized)
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

func TestTransparentCodeKeepsTextOpaqueAndMakesBackdropTransparent(t *testing.T) {
	slide := Slide{Elements: []Element{{Kind: "code", Text: "go", Query: "transparent=1"}}}
	lines := []Line{{Text: "go  ", Role: "code", Row: 2, Col: 3, Element: 0, Query: "transparent=1"}}
	exported := exportLines(lines, slide, 20, 10, 1)
	if len(exported) != 1 || exported[0].Role != "code" {
		t.Fatalf("transparent code export = %#v, want opaque code text", exported)
	}
	for _, part := range exported[0].Parts {
		if part.Background != "" {
			t.Fatalf("transparent code text retained background %q", part.Background)
		}
	}
	cells := transparentShapeCells(lines, 20, 10, slide)
	for col := 3; col < 7; col++ {
		if got := cells[2][col]; got != "100" {
			t.Fatalf("transparent code backdrop cell %d = %q, want gray background 100", col, got)
		}
	}
}

func TestTransparencyOnlyAppliesToBlockGlyphElements(t *testing.T) {
	for _, element := range []Element{
		{Kind: "text", Query: "transparent=1"},
		{Kind: "shape", Query: "glyph=blocks&transparent=1"},
		{Kind: "image", Path: "still.png", Query: "glyph=block&transparent=1"},
	} {
		if !elementTransparent(element) {
			t.Fatalf("block element should be transparent: %#v", element)
		}
	}
	for _, element := range []Element{
		{Kind: "text", Query: "glyph=braille&transparent=1"},
		{Kind: "shape", Query: "glyph=ascii&transparent=1"},
		{Kind: "image", Path: "animated.gif", Query: "glyph=blocks&transparent=1"},
	} {
		if elementTransparent(element) {
			t.Fatalf("non-block or animated GIF element should ignore transparency: %#v", element)
		}
		if values, _ := url.ParseQuery(element.Query); values.Get("transparent") != "1" {
			t.Fatalf("ignored transparency flag was removed: %#v", element)
		}
	}
}

func TestTransparencyAveragesUnderlyingAndOverlayColours(t *testing.T) {
	const want = "38;2;85;85;85"
	if got := blendFGForTransparency("33", "44"); got != want {
		t.Fatalf("bright underlying item blended with dark see-through item = %q, want %q", got, want)
	}
	if got := blendFGForTransparency("34", "43"); got != want {
		t.Fatalf("dark underlying item blended with bright see-through item = %q, want %q", got, want)
	}
}

func TestTransparentImageExportsAsSelectableTransparencyMask(t *testing.T) {
	slide := Slide{Elements: []Element{{Kind: "image", Query: "transparent=1"}}}
	lines := []Line{{Text: "\033[31m█\033[34m█", Role: "image", Row: 2, Col: 3, Element: 0, Query: "transparent=1"}}
	exported := exportLines(lines, slide, 20, 10, 1)
	if len(exported) != 1 || exported[0].Role != "transparent-image" {
		t.Fatalf("transparent image export = %#v, want selectable transparent-image source", exported)
	}
	mask := transparentShapeExportLines(lines, 20, 10, slide)
	if len(mask) != 1 || mask[0].Row != 2 || len(mask[0].Parts) == 0 {
		t.Fatalf("transparent image mask = %#v, want row 2 mask", mask)
	}
	cells := transparentShapeCells(lines, 20, 10, slide)
	if got := ansiCSSColour(cells[2][3]); got != "rgb(170,0,0)" {
		t.Fatalf("first transparent image cell = %q, want red", got)
	}
	if got := ansiCSSColour(cells[2][4]); got != "rgb(0,0,170)" {
		t.Fatalf("second transparent image cell = %q, want blue", got)
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

func TestSlideElementsCanonicalizeByTopLeftNameAndEncounterOrder(t *testing.T) {
	slide := Slide{Elements: []Element{
		{Kind: "text", Text: "Bottom", Query: "top=20&left=2", ID: "bottom"},
		{Kind: "text", Text: "Zulu", Query: "top=3&left=12", ID: "zulu"},
		{Kind: "text", Text: "Alpha", Query: "top=3&left=12", ID: "alpha-1"},
		{Kind: "text", Text: "Alpha", Query: "top=3&left=12", ID: "alpha-2"},
		{Kind: "text", Text: "Left", Query: "top=3&left=1", ID: "left"},
	}}
	mapping := canonicalizeSlideElementOrder(&slide, 80, 30)
	want := []string{"left", "alpha-1", "alpha-2", "zulu", "bottom"}
	for index, id := range want {
		if slide.Elements[index].ID != id {
			t.Fatalf("canonical order[%d]=%q, want %q; elements=%#v", index, slide.Elements[index].ID, id, slide.Elements)
		}
	}
	if mapping[0] != 4 || mapping[4] != 0 {
		t.Fatalf("old-to-new mapping=%v, want bottom->4 and left->0", mapping)
	}
}

func TestSaveDeckMovesMetadataWithCanonicallyOrderedElement(t *testing.T) {
	oldWidth, oldHeight := authoredTerminalWidth, authoredTerminalHeight
	authoredTerminalWidth, authoredTerminalHeight = 80, 30
	defer func() { authoredTerminalWidth, authoredTerminalHeight = oldWidth, oldHeight }()
	path := filepath.Join(t.TempDir(), "ordered.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{
		{Kind: "text", Text: "Bottom", Query: "top=20&left=2&fg=%23ff0000"},
		{Kind: "text", Text: "Top", Query: "top=2&left=4&fg=%2300ff00"},
	}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	top, bottom := strings.Index(text, "Top\n"), strings.Index(text, "Bottom\n")
	if top < 0 || bottom < 0 || top >= bottom {
		t.Fatalf("saved elements are not top-to-bottom: %q", text)
	}
	if !strings.Contains(text[:top], "fg=#00ff00") || !strings.Contains(text[top:bottom], "fg=#ff0000") {
		t.Fatalf("element metadata did not move with its Markdown item: %q", text)
	}
	reloaded, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{reloaded.Slides[0].Elements[0].Text, reloaded.Slides[0].Elements[1].Text}; !reflect.DeepEqual(got, []string{"Top", "Bottom"}) {
		t.Fatalf("load did not retain canonical order: %v", got)
	}
}

func TestParseDeckCorrectsNonCanonicalElementOrderOnLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unordered.md")
	raw := "<!-- keynope width=80 height=30 -->\n\n" +
		"<!-- top=18&left=3&fg=#ff0000 -->\nBottom\n\n" +
		"<!-- top=2&left=6&fg=#00ff00 -->\nTop\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	deck, err := parseDeck(path)
	if err != nil {
		t.Fatal(err)
	}
	if !parsedDeckElementOrderChanged {
		t.Fatal("non-canonical load was not marked for one-time persistence")
	}
	if len(deck.Slides) != 1 || len(deck.Slides[0].Elements) != 2 {
		t.Fatalf("parsed deck=%#v", deck)
	}
	query, _ := url.ParseQuery(deck.Slides[0].Elements[0].Query)
	if deck.Slides[0].Elements[0].Text != "Top" || query.Get("top") != "2" || query.Get("left") != "6" || query.Get("fg") != "#00ff00" {
		t.Fatalf("load did not reorder complete element record: %#v", deck.Slides[0].Elements)
	}
}

func TestBulletMarkerUsesRoundedBitmap(t *testing.T) {
	want := []string{
		"    ",
		" ▄▖ ",
		"▐██ ",
		"▐██ ",
		" ▀▘ ",
		"    ",
	}
	if got := renderBulletMarkerRows(); !reflect.DeepEqual(got, want) {
		t.Fatalf("rounded bullet marker = %#v, want %#v", got, want)
	}
	if got := renderBulletWrapped("Item", 80); len(got) != len(want) {
		t.Fatalf("rendered rounded bullet has %d rows, want %d", len(got), len(want))
	}
}

func TestContiguousMarkdownBulletsParseAsOneElement(t *testing.T) {
	slide := parseSlide("- First item\n- Second item\n\n- Separate list", t.TempDir())
	if len(slide.Elements) != 2 {
		t.Fatalf("parsed %d elements, want two bullet-list blocks", len(slide.Elements))
	}
	if got := slide.Elements[0]; got.Kind != "bullet" || got.Text != "First item\nSecond item" {
		t.Fatalf("first bullet block = %#v", got)
	}
	if got := slide.Elements[1]; got.Kind != "bullet" || got.Text != "Separate list" {
		t.Fatalf("second bullet block = %#v", got)
	}
}

func TestMarkdownBulletContinuationStaysInListBlock(t *testing.T) {
	slide := parseSlide("- First item\n  continued text\n- Second item", t.TempDir())
	if len(slide.Elements) != 1 {
		t.Fatalf("parsed %d elements, want one bullet-list block", len(slide.Elements))
	}
	if got := slide.Elements[0].Text; got != "First item\n  continued text\nSecond item" {
		t.Fatalf("bullet continuation text = %q", got)
	}
	rows := renderBulletWrapped(slide.Elements[0].Text, 80)
	if len(rows) != 16 {
		t.Fatalf("bullet plus continuation rendered %d rows, want 16", len(rows))
	}
}

func TestBulletBlockSaveOmitsEmptyItems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bullets.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "bullet", Text: "First\n  \nSecond"}}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "- First\n- Second\n") || strings.Contains(got, "- \n") {
		t.Fatalf("saved bullet block = %q", got)
	}
}

func TestBulletBlockSavePreservesIndentedContinuation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bullet-continuation.md")
	deck := Deck{Slides: []Slide{{Elements: []Element{{Kind: "bullet", Text: "First\n  continued\nSecond"}}}}}
	if err := saveDeck(path, deck); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "- First\n  continued\n- Second\n") {
		t.Fatalf("saved bullet continuation = %q", got)
	}
}

func TestBulletBlockWrapsWithHangingIndent(t *testing.T) {
	rows := renderBulletWrapped("abcdefgh\nTwo", 24)
	if len(rows) != 16 {
		t.Fatalf("wrapped bullet block has %d rows, want 16", len(rows))
	}
	for rowIndex, row := range rows {
		if displayWidth(stripANSI(row)) > 24 {
			t.Fatalf("row %d spills beyond viewport: %q", rowIndex, row)
		}
	}
	// The wrapped continuation begins after the same eight-cell hanging indent.
	for _, row := range rows[6:10] {
		if strings.TrimSpace(row) != "" && displayWidth(row)-displayWidth(strings.TrimLeft(row, " ")) < 8 {
			t.Fatalf("continuation is not hanging-indented: %q", row)
		}
	}
}

func TestResizedBulletBlockWrapsInsteadOfCropping(t *testing.T) {
	element := Element{Kind: "bullet", Text: "A resized bullet point that needs multiple lines", Query: "render=text-image&source=bitmap&scale=2.00"}
	rows := renderElementRowsBase(element, 48)
	if len(rows) <= 8 {
		t.Fatalf("resized bullet rendered only %d rows; text was cropped instead of wrapped", len(rows))
	}
	for rowIndex, row := range rows {
		if displayWidth(stripANSI(row)) > 48 {
			t.Fatalf("resized bullet row %d spills beyond viewport: %q", rowIndex, row)
		}
	}
}

func TestEmptyBulletAtCursorEndsList(t *testing.T) {
	text := "First\n\nThird"
	if !bulletItemAtCursorEmpty(text, len([]rune("First\n"))) {
		t.Fatal("empty current bullet was not detected")
	}
	if bulletItemAtCursorEmpty(text, 2) {
		t.Fatal("non-empty current bullet was detected as empty")
	}
	if got := normalizeBulletText(text); got != "First\nThird" {
		t.Fatalf("normalized bullet text = %q", got)
	}
}

func TestGroupedBulletCaretTracksItemAndKeepsGlyphWidth(t *testing.T) {
	element := Element{Kind: "bullet", Text: "First\nSecond  "}
	row, col, cells := bulletCaretMetrics(element, 80, len([]rune(element.Text)))
	if row != 10 || col != 40 || cells != 4 {
		t.Fatalf("caret after trailing spaces = row %d col %d cells %d, want row 10 col 40 cells 4", row, col, cells)
	}
	row, col, cells = bulletCaretMetrics(element, 80, len([]rune("First\n")))
	if row != 10 || col != 8 || cells != 4 {
		t.Fatalf("caret on new bullet = row %d col %d cells %d, want row 10 col 8 cells 4", row, col, cells)
	}
}

func TestBulletContinuationCaretUsesIndentedTextLine(t *testing.T) {
	element := Element{Kind: "bullet", Text: "First\n  continued"}
	row, col, cells := bulletCaretMetrics(element, 80, len([]rune(element.Text)))
	if row != 9 || col != 44 || cells != 4 {
		t.Fatalf("continuation caret = row %d col %d cells %d, want row 9 col 44 cells 4", row, col, cells)
	}
}

func TestRemoveEmptyCodeLineAtCursor(t *testing.T) {
	element := Element{Kind: "code", Text: "first\n\nthird"}
	if cursor := removeEmptyTextLineAtCursor(&element, len([]rune("first\n"))); cursor != len([]rune("first")) {
		t.Fatalf("cursor after removing empty code line = %d", cursor)
	}
	if element.Text != "first\nthird" {
		t.Fatalf("code after removing empty line = %q", element.Text)
	}
}

func TestResizedBulletCaretKeepsScaledWidthAcrossTrailingSpaces(t *testing.T) {
	base := Element{Kind: "bullet", Text: "First", Query: "render=text-image&source=bitmap&scale=2.00"}
	_, baseCol, baseCells := bulletCaretMetrics(base, 100, len([]rune(base.Text)))
	spaced := base
	spaced.Text += "  "
	_, spacedCol, spacedCells := bulletCaretMetrics(spaced, 100, len([]rune(spaced.Text)))
	if baseCells != 8 || spacedCells != 8 {
		t.Fatalf("resized caret widths = %d/%d, want 8/8", baseCells, spacedCells)
	}
	if spacedCol-baseCol != 16 {
		t.Fatalf("two trailing spaces moved resized caret %d cells, want 16", spacedCol-baseCol)
	}
}

func TestLeftAnchoredBulletWrapDoesNotShiftItsAnchor(t *testing.T) {
	element := Element{Kind: "bullet", Text: strings.Repeat("word ", 30), Query: "left=20&top=0"}
	lines := layout(Slide{Elements: []Element{element}}, 100, 80)
	minCol := 100
	maxRow := 0
	for _, line := range lines {
		if line.Element == 0 && line.Role != "outline" {
			minCol = min(minCol, line.Col)
			maxRow = max(maxRow, line.Row)
		}
	}
	if minCol != 20 {
		t.Fatalf("left-anchored bullet shifted to column %d, want 20", minCol)
	}
	if maxRow < 6 {
		t.Fatalf("left-anchored bullet did not wrap: max row %d", maxRow)
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
