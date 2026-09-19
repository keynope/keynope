package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCancelledEditorPreviewDoesNotPublishDerivative(t *testing.T) {
	element := Element{ID: "text", Kind: "text", Text: "Original", Query: "render=truetype"}
	session := newNativeEditorSession("Untitled.md", Deck{Slides: []Slide{{Elements: []Element{element}}}}, true)
	payload, err := json.Marshal(nativeEditorAction{Element: 0, ElementData: &element, Cols: 245, Rows: 56})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/editor/preview", bytes.NewReader(payload)).WithContext(ctx)
	response := httptest.NewRecorder()
	session.handlePreview(response, request)
	if response.Body.Len() != 0 {
		t.Fatalf("cancelled preview published %d bytes", response.Body.Len())
	}
}

func TestBrowserDerivativeCachesAreBoundedAndAbortable(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	sceneSource, err := os.ReadFile("web/scene.js")
	if err != nil {
		t.Fatal(err)
	}
	mainText, sceneText := string(mainSource), string(sceneSource)
	for _, marker := range []string{
		"maxContentAnimationCacheEntries = 8",
		"maxContentAnimationCacheBytes = 16 * 1024 * 1024",
		"while(effectState.size>=8)",
		"editorTextPreviewController?.abort()",
		"editorMutationPreviewController?.abort()",
		"signal:controller.signal",
	} {
		if !strings.Contains(mainText, marker) {
			t.Fatalf("browser runtime omitted %q", marker)
		}
	}
	for _, marker := range []string{
		"draftController?.abort()",
		"renderDraft(currentCrop,currentMask,controller.signal)",
		"record.controller.abort()",
		"shapeDrafts.size>8",
	} {
		if !strings.Contains(sceneText, marker) {
			t.Fatalf("scene draft runtime omitted %q", marker)
		}
	}
}

func TestLocalSlideUpdatesRetainUnrelatedAnimationCaches(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"function contentAnimationKey(page){return page.slide+':'+page.page;}",
		"function invalidateContentAnimationSlides(slides)",
		"function invalidateEffectSlides(slides)",
		"invalidateContentAnimationSlides(workspace.replaceSlides)",
		"invalidateEffectSlides(workspace.replaceSlides)",
		"invalidateContentAnimationSlides([slideIndex])",
		"invalidateContentAnimationSlides([slide])",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("local animation invalidation omitted %q", marker)
		}
	}
	if strings.Contains(text, "const cached = contentAnimationCache.get(pageIndex)") ||
		strings.Contains(text, "retainContentAnimation(pageIndex,decoded)") {
		t.Fatal("content animation cache is still coupled to unstable array positions")
	}
}

func TestForegroundWorkspaceUpdatesRetainModernBackdrop(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"const modernBackdropContentKeys=new WeakMap()",
		"function modernBackdropContentKey(page)",
		"page.scene?.background||page.bg||'#000'",
		"page.backgroundLines||[]",
		"modernBackdropContentKey(page),modernPageGeneration",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("modern backdrop retention omitted %q", marker)
		}
	}
	workspaceStart := strings.Index(text, "window.keynopeLoadWebWorkspace = async (workspace,state) => {")
	if workspaceStart < 0 {
		t.Fatal("could not locate workspace loader")
	}
	workspaceEnd := strings.Index(text[workspaceStart:], "window.keynopeReloadWebDocument = async workspace => {")
	if workspaceEnd < 0 {
		t.Fatal("could not locate workspace loader end")
	}
	workspaceLoader := text[workspaceStart : workspaceStart+workspaceEnd]
	if strings.Contains(workspaceLoader, "modernBackdropKey=''") {
		t.Fatal("ordinary workspace updates still invalidate the complete modern backdrop")
	}
}

func TestModernLinkHitTargetsUseStableIDReconciliation(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"const modernLinkViews=new Map()",
		"function syncModernLinks(page)",
		"let entry=modernLinkViews.get(object.id)",
		"if(entry.key!==key)",
		"for(const [id,entry] of modernLinkViews)if(!keep.has(entry.node))",
		"syncModernLinks(page);",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("modern link reconciliation omitted %q", marker)
		}
	}
	legacy := "linkLayer.replaceChildren();\n    for(const object of page.scene.objects)"
	if strings.Contains(text, legacy) {
		t.Fatal("modern foreground updates still rebuild every link hit target")
	}
}

func TestUnifiedWorkspaceSkipsDetachedLegacyInspectorRebuild(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	marker := "The visible inspector belongs to KeynopeWorkspace."
	start := strings.Index(text, marker)
	legacy := strings.Index(text, "inspector.replaceChildren();")
	if start < 0 || legacy < 0 || start >= legacy {
		t.Fatal("unified workspace guard is missing before the detached legacy inspector")
	}
	between := text[start:legacy]
	for _, required := range []string{
		"renderEditorTopbar();",
		"renderSpeakerNotes(activeSlide)",
		"scheduleEditorCanvasOverlay();",
		"return;",
	} {
		if !strings.Contains(between, required) {
			t.Fatalf("incremental chrome path omitted %q", required)
		}
	}
}

func TestContextualRibbonRetainsControlsForEquivalentState(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"editorTopbarPanelSignature = ''",
		"function editorTopbarElementSnapshot(element)",
		"const panelMemberIndices=slide?[...new Set(selectedIndices.flatMap(candidate=>canvasGroupMembers(candidate)))]:[]",
		"if(panelSignature===editorTopbarPanelSignature)return;",
		"editorTopbarPanelSignature=panelSignature;",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("contextual-ribbon retention omitted %q", marker)
		}
	}
	early := strings.Index(text, "if(panelSignature===editorTopbarPanelSignature)return;")
	if early < 0 {
		t.Fatal("contextual ribbon retention guard is missing")
	}
	rebuild := strings.Index(text[early:], "ribbonPanels[id].replaceChildren()")
	if rebuild < 0 {
		t.Fatal("contextual ribbon does not guard its panel rebuild")
	}
	signatureStart := strings.Index(text, "const panelSignature=JSON.stringify({")
	signatureEnd := strings.Index(text[signatureStart:], "});")
	if signatureStart < 0 || signatureEnd < 0 {
		t.Fatal("contextual ribbon signature is missing")
	}
	signature := text[signatureStart : signatureStart+signatureEnd]
	if strings.Contains(signature, "version") {
		t.Fatal("document revision alone must not invalidate retained contextual controls")
	}
}

func TestNativeEditorPerformanceInstrumentationIsOptInAndBounded(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	nativeSource, err := os.ReadFile("presenter/KeynopePresenter.swift")
	if err != nil {
		t.Fatal(err)
	}
	mainText, nativeText := string(mainSource), string(nativeSource)
	for _, marker := range []string{
		"window.keynopeEditorPerformanceSnapshot",
		"window.keynopeMeasureEditorAction",
		"if(window.KEYNOPE_PERFORMANCE)window.keynopeBeginInlineEdit=beginInlineEdit",
		"if(!started||!window.KEYNOPE_PERFORMANCE)return",
		"if(editorPerformanceSamples.length>256)editorPerformanceSamples.shift()",
		"requestAnimationFrame(()=>requestAnimationFrame(()",
		"setTimeout(()=>finish('timeout'),250)",
		"frameSource,visibility:document.visibilityState",
		"skipOuterPerformance:true",
		"performanceKind:'drag'",
		"performanceKind:'resize'",
		"performanceKind:'rotate'",
		"performanceKind:'crop'",
		"performanceKind:'typing'",
		"if (fitted) {nextEditorElementPerformanceKind='resize';updateEditorElementByID(sourceElementID, index, fitted)",
		"nextEditorElementPerformanceKind='resize';",
		"}, 'rotate');",
		"updated.query=q.toString();},'treatment')",
	} {
		if !strings.Contains(mainText, marker) {
			t.Fatalf("editor instrumentation omitted %q", marker)
		}
	}
	for _, marker := range []string{
		"ProcessInfo.processInfo.environment[\"KEYNOPE_PERFORMANCE\"] == \"1\"",
		"window.KEYNOPE_PERFORMANCE = true;",
		"action == \"editor-performance\"",
		"Keynope performance %@ response=%.2fms command=%.2fms frame=%.2fms source=%@ visibility=%@",
	} {
		if !strings.Contains(nativeText, marker) {
			t.Fatalf("native instrumentation omitted %q", marker)
		}
	}
}

func TestInlineEditorIgnoresEquivalentStateReplacement(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"requestedPath=editorState.path",
		"requestedRevision=editorState.version",
		"requestedMaster=!!editorState.masterMode",
		"editorState.path===requestedPath",
		"editorState.version===requestedRevision",
		"!!editorState.masterMode===requestedMaster",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("inline edit stale-state guard omitted %q", marker)
		}
	}
	if strings.Contains(text, "editorState===requestedState") {
		t.Fatal("equivalent editor-state replacement still cancels the inline editor")
	}
}

func TestNativeParticipantPublicationTimerSleepsOutsidePresentation(t *testing.T) {
	source, err := os.ReadFile("presenter/KeynopePresenter.swift")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"private func updateParticipantPublicationTimer()",
		"guard presentationMode != \"none\" else {",
		"participantPresentationTimer?.invalidate()",
		"participantPresentationTimer = nil",
		"guard participantPresentationTimer == nil else { return }",
		"guard let self, self.presentationMode != \"none\" else { return }",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("participant publication timer omitted %q", marker)
		}
	}
	if got := strings.Count(text, "participantPresentationTimer = Timer.scheduledTimer"); got != 1 {
		t.Fatalf("participant publication timer has %d schedule sites, want one gated site", got)
	}
	if got := strings.Count(text, "updateParticipantPublicationTimer()"); got != 3 { // declaration + start + stop
		t.Fatalf("participant publication timer has %d lifecycle references, want declaration/start/stop", got)
	}
}

func TestObjectNavigatorReconcilesRowsByStableID(t *testing.T) {
	source, err := os.ReadFile("web/workspace.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"const objectNodes=new Map()",
		"const retained=new Set(items.map(item=>item.id))",
		"let button=objectNodes.get(item.id)",
		"objectNodes.set(item.id,button)",
		"button.keynopeObject=item",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("object navigator reconciliation omitted %q", marker)
		}
	}
	if strings.Contains(text, "objectPanel.replaceChildren()") {
		t.Fatal("object selection still rebuilds the complete navigator")
	}
}

func TestWorkspaceRefreshProjectsObjectsOnce(t *testing.T) {
	source, err := os.ReadFile("web/workspace.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"function refreshObjects(allObjects)",
		"const allObjects=section!=='slide'||scope?(objects?.()||[]):[]",
		"const opacityItems=allObjects.filter(item=>item.selected)",
		"const groupingLocked=opacityItems.some(item=>item.locked)||allObjects.some",
		"if(section==='objects')refreshObjects(allObjects)",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("single workspace object snapshot omitted %q", marker)
		}
	}
	start := strings.Index(text, "function refresh() {")
	if start < 0 {
		t.Fatal("workspace refresh function is missing")
	}
	end := strings.Index(text[start:], "function selectPanel(")
	if end < 0 {
		t.Fatal("workspace refresh boundary is missing")
	}
	if got := strings.Count(text[start:start+end], "objects?.()||[]"); got != 1 {
		t.Fatalf("workspace refresh projects the complete object stack %d times, want one", got)
	}
}

func TestAccessibleSlideSemanticsRetainUnchangedPage(t *testing.T) {
	source := exportHTMLSuffix()
	for _, want := range []string{
		"let slideSemanticsSource = null;",
		"let slideSemanticsName = '';",
		"if (slideSemanticsSource !== page || slideSemanticsName !== pageName)",
		"slideSemanticsSource = page;",
		"updateSlideSemantics(page, pageName);",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("accessible semantic retention is missing %q", want)
		}
	}
	if strings.Contains(source, "stage.setAttribute('aria-label', pageName);\n  updateSlideSemantics(page, pageName);") {
		t.Fatal("accessible slide semantics are still rebuilt unconditionally")
	}
}

func TestHostedWorkspaceDoesNotRenderMatchingActionTwice(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"window.keynopeWorkspaceAppliedRevision=state.version",
		"Number(window.keynopeWorkspaceAppliedRevision)===nextState.version",
		"else if(!workspaceAlreadyApplied)",
		"if(!workspaceAlreadyApplied)await syncEditorWorkspace()",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("hosted workspace duplicate-render guard omitted %q", marker)
		}
	}
}

func TestUnifiedInlineEditorUsesConcreteObjectStyle(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, marker := range []string{
		"Unified decks choose their editor from the concrete object projection.",
		"if(original&&['text','heading','code','bullet','shape','image'].includes(original.kind))",
		"(object?.kind==='shape'?(object.labelStyle||object.style):object?.style)!=='retro'",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("unified inline-editor routing omitted %q", marker)
		}
	}
	legacy := "editorState.appearance?.version===2||(editorState.appearance?.defaultStyle||editorState.appearance?.mode)==='modern'"
	if strings.Contains(text, legacy) {
		t.Fatal("inline editing is still gated by retired deck-level appearance")
	}
}
