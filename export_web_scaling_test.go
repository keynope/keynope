package main

import (
	"strings"
	"testing"
)

func TestWebExportUsesPresentationCanvasRenderer(t *testing.T) {
	javascript := exportHTMLSuffix()
	for _, marker := range []string{
		"const keynopeCanvasRenderer = true;",
		"function fitCanvasFontToGrid()",
		"canvasCharWidth * 0.98",
		"function sizeCanvasToAspect(availableWidth, availableHeight, targetAspect)",
		"function sizeEditorCanvas(stageRect)",
		"sizeEditorCanvas(stageRect);",
		"const notesHeight = editorSpeakerNotesVisible ? 132 : 0;",
		"--editor-notes-top",
		"sizeCanvasToAspect(innerWidth, innerHeight, 16 / 9);",
		"if (keynopeCanvasRenderer) {\n    drawPresenterPage(page, frame, contentLines);",
		"renderCanvasLinkHitAreas(contentLines)",
		"if (keynopeCanvasRenderer && presenterTransitionUntil",
		"startPageTransition(previousPageIndex, pageIndex);",
		"function publishPresenterPage(index)",
		"async function navigateEditorPage(delta)",
		"action: 'navigate-presentation'",
		"action: 'start-timer'",
		"notesToggleButton.className = 'keynope-app-icon-button keynope-app-tag-button'",
		"e.key.toLowerCase() === 'z' || e.key.toLowerCase() === 'y'",
		"function drawEditorCanvasCaret(lines)",
		"function drawEditorCanvasSelection()",
		"rgba(85, 170, 255, .58)",
		"fetch('/api/editor/preview'",
		"editor.className = 'keynope-inline-capture'",
		"function inlineEditorInitialCursor(value)",
		"function applyInlineSelectionWrapper(marker)",
		"function applyInlineSelectionColour(color, savedSelection)",
		"editor.setSelectionRange(initialCursor, initialCursor);",
		"const pendingEdit = activeInlineEditor ? activeInlineEditor.finish(true) : inlineEditCompletion;",
		"activeInlineEditor.finish(true).then(() => editorAction({action: 'select-element', element: index",
		"canvasOverlay.addEventListener('pointerdown', event => {",
		"if (!activeInlineEditor || activeInlineEditor.editor.contains(event.target)) return;",
		"function editorElementIndexMaps()",
		"const rawElement = resolvedToRaw.get(line.element);",
		"slideContextMenu.className = 'keynope-slide-context'",
		"button.addEventListener('contextmenu'",
		"Use master appearance",
		"if (!editorState.masterMode) {\n      const reset = document.createElement('button');",
		"(performance.now() - editorCanvasCaret.started) % 900",
		"if (editorCanvasCaret.exact)",
		"Math.max(1, caret.cells || 1) * canvasCharWidth",
		"stage.addEventListener('contextmenu'",
		"masterModeButton.addEventListener('click', () => editorAction({action: 'toggle-master-mode'}",
		"slidesHeader.className = 'keynope-slides-header'",
		"button.classList.add('master-reorderable');",
		"masterDropPlaceholder.className = 'keynope-master-drop-placeholder';",
		"masterDropDestination = reference ? remaining.indexOf(reference) + 1 : remaining.length + 1;",
		"else slidesPanel.appendChild(masterDropPlaceholder);",
		"event.dataTransfer.setDragImage(",
		"slidesPanel.scrollBy({top: -18});",
		"editorAction({action: 'reorder-master', slide: source, value: destination})",
		"action: 'toggle-master-mode'",
		"fetch('/api/editor/workspace?cols='",
		"speakerNotesSaveTimer = setTimeout(flushSpeakerNotes, 2000);",
		"action: 'update-slide-notes'",
		"await navigator.clipboard.writeText(speakerNotesInput.value.slice(start, end));",
		"const pasted = await navigator.clipboard.readText();",
		"function changeCanvasTextSize(index, delta)",
		"function setCanvasTextKind(index, kind, level)",
		"for (const key of ['render','source','scale','text-size']) query.delete(key);",
		"const colour = query.get(wasHeading ? 'header' : 'fg') || query.get(wasHeading ? 'fg' : 'header');",
		"fetch('/api/editor/normalize-text-kind'",
		"function appendCanvasTextKindTools(container, index, element)",
		"const selectedText = ['heading','text','text-image','bullet','code'].includes(element.kind);",
		"appendCanvasTextKindTools(selectionTopbar, textIndex, textElement);",
		"action:'update-elements', elementIndices:indices, elementsData:updates",
		"action:'convert-selected-text-kind'",
		"editor.value.startsWith(String.fromCharCode(96))",
		"[...editor.value.slice(0, editor.selectionStart || 0)].length",
		"function ensureEmojiPicker()",
		"function emojiPickerPreview(lines)",
		"fetch('/api/editor/emojis?'",
		"function canvasEmojiTool(index)",
		"Add emoji",
		"limit:'72', size:'10'",
		"query:'render=text-image&source=bitmap&scale=5.00&text-size=25'",
		"['⏺', 'bullet', 0, element.kind === 'bullet', false]",
		"['', 'code', 0, element.kind === 'code', true]",
		"action: 'convert-text-kind'",
		"function cycleCanvasOutline(index)",
		"function canvasOutlineTool(index, element, query)",
		"keynope-image-outline-button",
		"M391 74 C468 70 499 124 550 151",
		"if (shape === 'circle') label = '⃣⃣⃣⃣⃣';",
		"else if (shape === 'square') label = '𓉘𓉝';",
		"symbol.className = 'keynope-shape-outline-symbol keynope-shape-outline-' + shapeIcon",
		"function canvasDuplicateTool(index)",
		"function canvasLayerTool(index, direction)",
		"keynope-layer-button",
		"button.title = backward ? 'Send backward' : 'Bring forward';",
		"function rotateCanvasText(index)",
		"'keynope-icon-button keynope-rotate-button'",
		"<span class=\"keynope-rotate-label\">ROTATE</span>",
		"function canvasStyleSelect(index, element)",
		"function canvasColourTool(index, element, renderedColour)",
		"const colourKey = element.kind === 'heading' ? 'header' : 'fg';",
		"function setCanvasAlignment(index, alignment)",
		"function setCanvasVerticalAlignment(index, alignment)",
		"function canvasVerticalAlignmentTool(index, alignment, active)",
		"query.set('valign', alignment);",
		"middle: 'M5 5v10M2.5 7.5 5 5l2.5 2.5M2.5 12.5 5 15l2.5-2.5'",
		"M11 6v8M14 6v8M17 6v8",
		"function applyCanvasLink(index, input, value)",
		"function openCanvasLinkDialog(index)",
		"for (const [value, label] of [['url','URL'],['slide','Slide']])",
		"urlLabel.hidden = mode.value !== 'url';",
		"slideLabel.hidden = mode.value !== 'slide';",
		"function canvasLinkTool(index, query)",
		"keynope-icon-button keynope-link-button",
		"function drawCanvasLinkUnderlines(lines)",
		"presenterContext.strokeStyle = group.color || '#f3efe0';",
		"editorButton('⟲', {action: 'undo'}",
		"editorButton('⟳', {action: 'redo'}",
		"undoButton.classList.add('keynope-icon-button', 'keynope-history-button')",
		"<span class=\"keynope-history-label\">UNDO</span>",
		"<span class=\"keynope-history-label\">REDO</span>",
		"handler.postMessage({action: 'export-html'})",
		"e.key.toLowerCase() === 's'",
		"handler.postMessage({action: 'save-presentation'})",
		"exportButton.innerHTML = '<svg viewBox=\"0 0 800 600\"",
		"function showEditorExportConfirmation()",
		"function drawEditorExportConfirmation()",
		"const text = editorExportConfirmation.text || 'EXPORTED';",
		"function renderEditorTopbar()",
		"const normalizedText = value => {",
		"name:'inline-edit'",
		"editorCanvasCaret.exact = payload.caret",
		"function inlineEditorVerticalCursor(value, cursor, direction, kind)",
		"function inlineEditorFormattingJump(value, cursor, direction)",
		"function moveInlineEditorAcrossFormatting(editor, direction, extend)",
		"target = direction < 0\n      ? inlineEditorPreviousTextBoundary(editor.value, target)\n      : inlineEditorNextTextBoundary(editor.value, target);",
		"function cleanupInlineEditorEmptyColorTags(editor)",
		"function deleteInlineEditorColorContent(editor, direction)",
		"function deleteInlineEditorAcrossFormatting(editor, direction)",
		"function deleteInlineEditorSelectionPreservingFormatting(editor)",
		"editor.addEventListener('input', () => {\n      cleanupInlineEditorEmptyColorTags(editor);",
		"/\\[color=#[0-9a-f]{6}\\]|\\[\\/color\\]/ig",
		"moveInlineEditorAcrossFormatting(editor, keyEvent.key === 'ArrowLeft' ? -1 : 1, keyEvent.shiftKey)",
		"Math.min(visibleColumn, targetLength)",
		"keyEvent.key === 'ArrowUp' || keyEvent.key === 'ArrowDown'",
		"lines.push(continuation ? '  ' + text : text);",
		"const newline = element.kind === 'bullet' && keyEvent.shiftKey ? '\\n  ' : '\\n';",
		"element.kind === 'bullet' || element.kind === 'code' || element.kind === 'text'",
		"Enter on empty bullet save",
		"Editing text block · Enter newline · Enter on empty line save · Shift+Enter newline",
		"const confirmEscape = () => {",
		"heading.textContent = 'Keep text changes?';",
		"canvasTool('No, revert', ''",
		"blocker.className = 'keynope-modal-blocker';",
		"dialog.setAttribute('aria-modal', 'true');",
		"document.activeElement === keep ? revert : keep",
		"editor.selectionDirection === 'backward' ? selectionStart : selectionEnd",
		"Shift+Enter continuation",
		"Enter on empty line save",
		"concat(effectLines(page, frameValue))",
		"if (!keynopeAppSurface) drawPresenterPhosphor",
		"if (fromIndex === toIndex || keynopeAppSurface) return;",
		"function svgToolbarButton(title, drawing, action)",
		"const cloneIconSVG = '<svg viewBox=\"0 0 800 800\"",
		"const deleteIconSVG = '<svg viewBox=\"0 0 800 600\"",
		"svgToolbarButton('Appearance'",
		"function addElementIconButton(title, kind, drawing, level, activate)",
		"addElementIconButton('Add title', 'heading'",
		"addElementIconButton('Add subtitle', 'heading'",
		"addElementIconButton('Add bullet point', 'bullet'",
		"addElementIconButton('Show slide number', 'page-number'",
		"addPageNumberButton.hidden = !(editorState && editorState.masterMode);",
		"addPageNumberButton.classList.toggle('active', pageNumberState === 'on');",
		"addPageNumberButton.classList.toggle('inherited', pageNumberState === 'inherited');",
		"addPageNumberButton.classList.toggle('off', pageNumberState === 'off');",
		"addPageNumberTag.textContent = pageNumberState === 'inherited' ? 'INH.'",
		"editorAction({action: 'toggle-page-number'})",
		"addShapeMenu.className = 'keynope-add-shape-menu'",
		"editorAction({action: 'add-element', kind:'shape', name:shape})",
		"<svg viewBox=\"-13 80 1050 1040\" aria-hidden=\"true\"><path",
		"function toggleCanvasMarkdownStyle(index, marker)",
		"function canvasVisualMenu(index, element)",
		"keynope-adjust-image-button",
		"const uploadSVG = importButton.querySelector('svg');",
		"function canvasTransparencyTool(index, query)",
		"keynope-see-through-button",
		"selectionTopbar.appendChild(canvasTransparencyTool(index, query));\n    selectionTopbar.appendChild(canvasDeleteTool(index));",
		"keynope-checkerboard-",
		"function appendCanvasShapeKindTools(container, index, query)",
		"function canvasDeleteTool(index)",
		"function appendCanvasVisualControls(panel, index, element)",
		"visualControls.className = 'keynope-context-visual'",
		"document.body.appendChild(panel)",
		"function closeCanvasVisualMenu()",
		"activeCanvasVisualMenu.index !== index",
		"function showElementContextMenu(index, clientX, clientY)",
		"function canvasShapeSelectionBounds(element, pageNumber)",
		"const bounds = canvasShapeSelectionBounds(element, page.page)",
		"function cycleCanvasSelection(reverse)",
		"cycleCanvasSelection(e.shiftKey)",
		"setCanvasAlignment(editorState.selected, e.key === '<' ? 'left' : e.key === '>' ? 'right' : 'center')",
		"cycleCanvasOutline(editorState.selected)",
		"presenterTimerMode === 'config' && !e.metaKey",
		"presenterTimerMode === 'running' && !e.metaKey",
		"data-keynope-timer-active",
		"canvasOverlay.hidden = active;",
		"timerButton.title = active ? 'Stop timer' : 'Timer'",
		"button !== stopPresentationButton && button !== pauseButton",
		"function refreshEditorBottomToolbarVisibility()",
		"notesToggleButton.hidden = masterMode;",
		"previousButton.hidden = masterMode;",
		"presentButton.hidden = masterMode || presenting;",
		"if (navigationKey || ['0', '2', 'p', 'P'].includes(e.key))",
		"function keynopeFormControlTarget(target)",
		"if (keynopeFormControlTarget(e.target)) return;",
		"if (keynopeAppSurface && keynopeEditorMasterMode) return;",
		"setSpeakerNotesVisible(false, false);",
		"editorAction({action: 'select-element', element: -1}).catch(() => {});",
		"line.role === 'transparent-text'",
		"line.role === 'transparent-image'",
		"function blendCSSForTransparency(color, overlay)",
		"contentFrames[contentAnimationFrame % contentFrames.length].lines",
		"if (!keynopeEditorSelectionActive) contentAnimationFrame++;",
		"function canvasElementIsGIF(element)",
		"return glyph === '' || glyph === 'blocks' || glyph === 'block';",
		"if (selectedElements.length === 1 && canvasElementSupportsTransparency(element)) selectionTopbar.appendChild(canvasTransparencyTool(index, query));",
		"const editorClipboardPrefix = 'keynope-elements:';",
		"editorClipboardCommand(command).catch(() => {});",
		"function previewCanvasMutation(index, element, refreshOverlay = true, frozenImage = false)",
		"previewCanvasMutation(index, resizedElementForBounds(bounds), false, sourceElement.kind === 'image')",
		"function showEditorRenderingProgress()",
		"function hideEditorRenderingProgress()",
		"function fitCanvasTextElement(index, element, boxWidth, boxHeight)",
		"fetch('/api/editor/fit-text'",
		"if (fittingText) queueTextFit(next);",
		"else if (resizingVisual) queueVisualPreview(next);",
		"const fitted = await finishTextFit(resizedBounds(dx, dy));",
		"if (!presenterMainSurface) {\n      presenterTimerMode = state.timerMode || '';",
		"if (!keynopeAppSurface || page.hideChromePageNumber) return;",
		"chromeLayer.innerHTML = keynopeAppSurface && !page.hideChromePageNumber",
		"setInterval(syncPresenterState, 120);",
	} {
		if !strings.Contains(javascript, marker) {
			t.Fatalf("web export is missing shared canvas renderer marker %q", marker)
		}
	}
	prefix := exportHTMLPrefix(preservedExportHead{}, false)
	if !strings.Contains(prefix, ".keynope-slides-header button.active { border-color: #70b7ff; background: #244766;") {
		t.Fatal("master-mode sidebar button is missing its active appearance")
	}
	if !strings.Contains(prefix, ".keynope-canvas-element.active { border: 2px solid #ffd166;") ||
		!strings.Contains(prefix, ".keynope-resize-handle { position: absolute; z-index: 2; width: 9px; height: 9px; border: 1px solid #111; background: #ffd166;") {
		t.Fatal("canvas element selection does not use the yellow outline and handles")
	}
	if !strings.Contains(prefix, ".keynope-modal-blocker { position: fixed; inset: 0; z-index: 119;") {
		t.Fatal("dirty-text confirmation is missing its full-window interaction blocker")
	}
	if !strings.Contains(prefix, ".keynope-editor-topbar button.keynope-page-number-button.inherited { border-color: #62d98b;") {
		t.Fatal("inherited page-number state is missing its green outline")
	}
	for _, marker := range []string{
		"button.keynope-page-number-button.off:hover:not(:disabled) { border-color: #555 !important;",
		"button.keynope-page-number-button.inherited:hover:not(:disabled) { border-color: #62d98b !important;",
		"button.keynope-page-number-button.active:hover:not(:disabled) { border-color: #70b7ff !important;",
	} {
		if !strings.Contains(prefix, marker) {
			t.Fatalf("page-number hover changes its state outline: missing %q", marker)
		}
	}
	if !strings.Contains(prefix, ".keynope-page-number-tag { position: absolute; left: 50%; bottom: 0; transform: translateX(-50%); padding: 0 3px;") ||
		!strings.Contains(prefix, "font: 700 6px/8px -apple-system, BlinkMacSystemFont, sans-serif; letter-spacing: .05em;") {
		t.Fatal("page-number state tag no longer matches the readable toolbar tag size")
	}
	if !strings.Contains(prefix, ".keynope-editor-topbar button[hidden] { display: none; }") {
		t.Fatal("hidden contextual toolbar buttons can be forced visible by their display style")
	}
	if !strings.Contains(prefix, ".keynope-master-drop-placeholder { width: 100%; min-height: 30px;") ||
		!strings.Contains(prefix, ".keynope-master-drag-image { position: fixed; top: -1000px;") {
		t.Fatal("master reordering is missing its live placeholder or full-opacity drag card")
	}
	if strings.Contains(prefix, ".keynope-slide-item.master-dragging { opacity: .5;") {
		t.Fatal("master reordering still greys out the dragged item")
	}
	if strings.Contains(javascript, "document.body.appendChild(inspector)") {
		t.Fatal("windowed editor still mounts the inspector sidebar")
	}
	if strings.Contains(javascript, "tools.className = 'keynope-selection-tools'") {
		t.Fatal("windowed editor still mounts element controls over the canvas")
	}
	for _, removedStyle := range []string{"Style: Half", "Style: Vertical", "['half','Half']", "['vertical','Vertical']"} {
		if strings.Contains(javascript, removedStyle) {
			t.Fatalf("windowed editor still offers removed image style %q", removedStyle)
		}
	}
	if !strings.Contains(javascript, "if (!window.KEYNOPE_PRESENTER) return;") {
		t.Fatal("presenter state synchronization is no longer gated to live presenter documents")
	}
	for _, staleSizing := range []string{"innerWidth - 490", "deck.cols - 2", "deck.rows - 5"} {
		if strings.Contains(javascript, staleSizing) {
			t.Fatalf("web export still contains divergent canvas sizing %q", staleSizing)
		}
	}
}

func TestCanvasBlockGlyphsShareRoundedPixelBoundaries(t *testing.T) {
	javascript := exportHTMLSuffix()
	for _, marker := range []string{
		"const x1 = Math.round(col * canvasCharWidth);",
		"const xMid = Math.round((col + 0.5) * canvasCharWidth);",
		"const x2 = Math.round((col + 1) * canvasCharWidth);",
		"const yMid = Math.round((row + 0.5) * canvasCell);",
		"const x2 = Math.round((col + len) * canvasCharWidth);",
	} {
		if !strings.Contains(javascript, marker) {
			t.Fatalf("canvas block renderer is missing shared boundary %q", marker)
		}
	}
	for _, overlapping := range []string{
		"Math.floor(col * canvasCharWidth)",
		"Math.ceil((col + 1) * canvasCharWidth)",
		"Math.floor((col + 0.5) * canvasCharWidth)",
	} {
		if strings.Contains(javascript, overlapping) {
			t.Fatalf("canvas block renderer retains overlapping boundary %q", overlapping)
		}
	}
}
