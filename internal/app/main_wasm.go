//go:build js && wasm

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall/js"
)

type wasmResponse struct {
	Status      int               `json:"status"`
	ContentType string            `json:"contentType,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body"`
}

type wasmWorkspace struct {
	DocumentRevision  int64                 `json:"documentRevision"`
	Cols              int                   `json:"cols"`
	Rows              int                   `json:"rows"`
	Pages             []workspaceWirePage   `json:"pages"`
	Revision          int                   `json:"revision"`
	BaseRevision      int                   `json:"baseRevision,omitempty"`
	ReplaceSlides     []int                 `json:"replaceSlides,omitempty"`
	SlideCount        int                   `json:"slideCount"`
	Master            bool                  `json:"master,omitempty"`
	ProjectionMetrics *wasmWorkspaceMetrics `json:"projectionMetrics,omitempty"`
}

// Explicit local diagnostics only. Never part of deck/export metadata, and
// omitted from normal workspace requests. No assets, names or private results.
type wasmWorkspaceMetrics struct {
	RuntimeCaches            *browserRuntimeCacheStats `json:"runtimeCaches,omitempty"`
	StructuredComparisonHits int                       `json:"structuredComparisonHits"`
	NormalPageHits           int                       `json:"normalPageHits"`
	NormalPageMisses         int                       `json:"normalPageMisses"`
	NormalSourceResolutions  int                       `json:"normalSourceResolutions"`
	MasterPreviews           int                       `json:"masterPreviews"`
	MasterPreviewHits        int                       `json:"masterPreviewHits"`
	NormalReplacedSlides     []int                     `json:"normalReplacedSlides"`
}

var wasmCallbacks []js.Func
var wasmPageCache workspacePageCache
var wasmWorkspaceRevision int
var wasmWorkspaceCanDelta bool

func wasmJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		data, _ = json.Marshal(wasmResponse{Status: http.StatusInternalServerError, Body: err.Error()})
	}
	return string(data)
}

func wasmEditorInit(_ js.Value, args []js.Value) any {
	if len(args) == 0 {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "missing Markdown document"})
	}
	source := args[0].String()
	path := "Untitled.md"
	if len(args) > 1 && strings.TrimSpace(args[1].String()) != "" {
		path = args[1].String()
	}
	untitled := true
	if len(args) > 2 {
		untitled = args[2].Bool()
	}
	session, err := replaceBrowserDocument(path, []byte(source), untitled)
	if err != nil {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: err.Error()})
	}
	activeNativeEditor = session
	wasmPageCache = workspacePageCache{}
	wasmWorkspaceCanDelta = false
	return wasmJSON(wasmResponse{Status: http.StatusOK, ContentType: "application/json", Body: wasmJSON(activeNativeEditor.transportState())})
}

func wasmEditorHandler(path string) http.HandlerFunc {
	if activeNativeEditor == nil {
		return nil
	}
	switch path {
	case "/api/editor/templates":
		return activeNativeEditor.handleTemplates
	case "/api/editor/clipboard":
		return activeNativeEditor.handleObjectClipboard
	case "/api/editor/paste-clipboard":
		return activeNativeEditor.handlePasteObjectClipboard
	case "/api/editor/state":
		return activeNativeEditor.handleState
	case "/api/editor/scene":
		return activeNativeEditor.handleScene
	case "/api/editor/scene-fonts":
		return activeNativeEditor.handleSceneFonts
	case "/api/editor/typeface":
		return activeNativeEditor.handleTypeface
	case "/api/editor/font-export":
		return activeNativeEditor.handleFontExport
	case "/api/editor/action":
		return activeNativeEditor.handleAction
	case "/api/editor/preview":
		return activeNativeEditor.handlePreview
	case "/api/editor/connector-preview":
		return activeNativeEditor.handleConnectorPreview
	case "/api/editor/connector-draft":
		return activeNativeEditor.handleConnectorDraft
	case "/api/editor/fit-text":
		return activeNativeEditor.handleFitText
	case "/api/editor/normalize-text-kind":
		return activeNativeEditor.handleNormalizeTextKind
	case "/api/editor/emojis":
		return activeNativeEditor.handleEmojiCatalog
	case "/api/editor/emoji-text-fonts":
		return activeNativeEditor.handleEmojiTextFonts
	case "/api/editor/shape-draft":
		return activeNativeEditor.handleShapeDraft
	case "/api/editor/shape-adjustment-draft":
		return activeNativeEditor.handleShapeAdjustmentDraft
	case "/api/editor/media-draft":
		return activeNativeEditor.handleMediaDraft
	case "/api/editor/table-draft":
		return activeNativeEditor.handleTableDraft
	case "/api/editor/table-cell-style-draft":
		return activeNativeEditor.handleTableCellStyleDraft
	case "/api/editor/text-style-draft":
		return activeNativeEditor.handleTextStyleDraft
	case "/api/editor/activity-qr":
		return activeNativeEditor.handleActivityQR
	case "/api/editor/workspace":
		return activeNativeEditor.handleWorkspace
	case "/api/editor/upload":
		return activeNativeEditor.handleUpload
	case "/api/editor/upload-video":
		return activeNativeEditor.handleVideoUpload
	case "/api/editor/document":
		return activeNativeEditor.handleDocument
	case "/api/editor/participant-page":
		return activeNativeEditor.handleParticipantPage
	case "/api/editor/export-document":
		return activeNativeEditor.handleExportDocument
	case "/api/editor/export-pptx":
		return activeNativeEditor.handleExportPPTX
	case "/api/editor/pptx-captures":
		return activeNativeEditor.handlePPTXCaptureManifest
	case "/api/editor/export-pdf":
		return activeNativeEditor.handleExportPDF
	case "/api/editor/import-pptx":
		return activeNativeEditor.handleImportPPTX
	default:
		return nil
	}
}

func wasmEditorRequest(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "invalid editor request"})
	}
	method, target := args[0].String(), args[1].String()
	body := ""
	if len(args) > 2 {
		body = args[2].String()
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "invalid editor URL"})
	}
	handler := wasmEditorHandler(parsed.Path)
	if handler == nil {
		status := http.StatusNotFound
		message := "editor is not initialized"
		if activeNativeEditor != nil {
			message = "editor endpoint not found"
		}
		return wasmJSON(wasmResponse{Status: status, Body: message})
	}
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if len(args) > 3 && acceptsEditorProblem(args[3].String()) {
		request.Header.Set("Accept", "application/problem+json")
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	result := recorder.Result()
	contentType := result.Header.Get("Content-Type")
	// Preserve bounded local diagnostics/cache policy across the browser adapter.
	// Never forward arbitrary request/response headers or authentication data.
	headers := map[string]string{}
	for _, name := range []string{"Cache-Control", "Server-Timing", "X-Keynope-Error-Code", "X-Keynope-Preview-Scope", "X-Keynope-Scene-Cache", "X-Keynope-Scene-Builds", "X-Keynope-Text-Builds", "X-Keynope-Text-Cache-Hits", "X-Keynope-Table-Builds", "X-Keynope-Table-Cache-Hits", "X-Keynope-Layout-Builds", "X-Keynope-Layout-Cache-Hits", "X-Keynope-Object-Layout-Builds", "X-Keynope-Object-Layout-Cache-Hits", "X-Keynope-Object-Artwork-Layouts"} {
		if value := result.Header.Get(name); value != "" {
			headers[name] = value
		}
	}
	return wasmJSON(wasmResponse{Status: result.StatusCode, ContentType: contentType, Headers: headers, Body: recorder.Body.String()})
}

func wasmEditorUpload(_ js.Value, args []js.Value) any {
	if activeNativeEditor == nil || len(args) == 0 {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "editor is not initialized"})
	}
	video := len(args) > 1 && args[1].String() == "video"
	if video && args[0].Get("byteLength").Int() > maxEmbeddedVideoBytes {
		return wasmJSON(wasmResponse{Status: 400, Body: videoCapacityMessage})
	}
	data := make([]byte, args[0].Get("byteLength").Int())
	js.CopyBytesToGo(data, args[0])
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	field := "image"
	if video {
		field = "video"
	}
	part, err := writer.CreateFormFile(field, field)
	if err == nil {
		_, err = part.Write(data)
	}
	if video && err == nil && len(args) > 2 && !args[2].IsNull() && !args[2].IsUndefined() {
		if args[2].Get("byteLength").Int() > 2<<20 {
			return wasmJSON(wasmResponse{Status: 400, Body: "Video poster exceeds 2 MiB"})
		}
		poster := make([]byte, args[2].Get("byteLength").Int())
		js.CopyBytesToGo(poster, args[2])
		part, err = writer.CreateFormFile("poster", "poster.png")
		if err == nil {
			_, err = part.Write(poster)
		}
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: err.Error()})
	}
	request := httptest.NewRequest(http.MethodPost, "/api/editor/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	if video {
		activeNativeEditor.handleVideoUpload(recorder, request)
	} else {
		activeNativeEditor.handleUpload(recorder, request)
	}
	return wasmJSON(wasmResponse{Status: recorder.Code, ContentType: recorder.Header().Get("Content-Type"), Body: recorder.Body.String()})
}

// wasmEditorPPTXImport deliberately transfers raw ArrayBuffer bytes. PPTX is
// a ZIP and must never be routed through JavaScript strings or JSON base64 on
// the import path.
func wasmEditorPPTXImport(_ js.Value, args []js.Value) any {
	if activeNativeEditor == nil || len(args) == 0 {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: "editor is not initialized"})
	}
	data := make([]byte, args[0].Get("byteLength").Int())
	js.CopyBytesToGo(data, args[0])
	name := "Imported.pptx"
	if len(args) > 1 && strings.TrimSpace(args[1].String()) != "" {
		name = args[1].String()
	}
	request := httptest.NewRequest(http.MethodPost, "/api/editor/import-pptx", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/vnd.openxmlformats-officedocument.presentationml.presentation")
	request.Header.Set("X-Keynope-Filename", name)
	recorder := httptest.NewRecorder()
	activeNativeEditor.handleImportPPTX(recorder, request)
	return wasmJSON(wasmResponse{Status: recorder.Code, ContentType: recorder.Header().Get("Content-Type"), Body: recorder.Body.String()})
}

func currentWASMWorkspace(comparisons ...*workspaceSceneComparisons) (wasmWorkspace, error) {
	if activeNativeEditor == nil {
		return wasmWorkspace{}, fmt.Errorf("editor is not initialized")
	}
	cols, rows := authoredRenderSize(authoredTerminalWidth, authoredTerminalHeight)
	activeNativeEditor.mu.RLock()
	deck := cloneDeckForScene(activeNativeEditor.deck)
	masterMode, current := activeNativeEditor.masterMode, activeNativeEditor.current
	if masterMode {
		current = activeNativeEditor.currentMaster
	}
	documentRevision := activeNativeEditor.version
	activeNativeEditor.mu.RUnlock()
	workspace := wasmWorkspace{Cols: cols, Rows: rows, SlideCount: len(deck.Slides), Master: masterMode}
	workspace.DocumentRevision = documentRevision
	var err error
	pages, renderErr := wasmPageCache.renderOwnedCompared(deck, masterMode, current, cols, rows, workspaceComparison(comparisons))
	err = renderErr
	workspace.Pages = workspaceWirePages(pages, nil, false)
	return workspace, err
}

func wasmEditorWorkspace(_ js.Value, args []js.Value) any {
	base := 0
	if len(args) > 0 && args[0].Type() == js.TypeNumber {
		base = args[0].Int()
	}
	oldCols, oldRows := wasmPageCache.cols, wasmPageCache.rows
	previousPages := wasmPageCache.pages
	var comparisons workspaceSceneComparisons
	workspace, err := currentWASMWorkspace(&comparisons)
	if err != nil {
		return wasmJSON(wasmResponse{Status: http.StatusBadRequest, Body: err.Error()})
	}
	if len(args) > 1 && args[1].Type() == js.TypeBoolean && args[1].Bool() {
		workspace.ProjectionMetrics = &wasmWorkspaceMetrics{
			NormalPageHits: wasmPageCache.hits, NormalPageMisses: wasmPageCache.misses,
			NormalSourceResolutions: wasmPageCache.resolutions, MasterPreviews: wasmPageCache.masterPreviews,
			MasterPreviewHits:    wasmPageCache.masterHits,
			NormalReplacedSlides: append([]int(nil), wasmPageCache.changed...),
		}
		stats := browserDocumentCacheStats()
		workspace.ProjectionMetrics.RuntimeCaches = &stats
	}
	if base > 0 && base == wasmWorkspaceRevision && wasmWorkspaceCanDelta && !workspace.Master && oldCols == workspace.Cols && oldRows == workspace.Rows {
		workspace.BaseRevision = base
		workspace.ReplaceSlides = append([]int(nil), wasmPageCache.changed...)
		changed := map[int]bool{}
		for _, index := range workspace.ReplaceSlides {
			changed[index] = true
		}
		pages := make([]workspaceWirePage, 0)
		for _, page := range workspace.Pages {
			if changed[page.Slide] {
				pages = append(pages, page)
			}
		}
		workspace.Pages = pages
		if len(args) > 2 && args[2].Type() == js.TypeBoolean && args[2].Bool() {
			full := make([]exportPage, 0, len(pages))
			for _, page := range pages {
				full = append(full, page.exportPage)
			}
			workspace.Pages = workspaceWirePages(full, previousPages, true, &comparisons)
		}
	}
	wasmWorkspaceRevision++
	if workspace.ProjectionMetrics != nil {
		workspace.ProjectionMetrics.StructuredComparisonHits = comparisons.hits
	}
	workspace.Revision = wasmWorkspaceRevision
	wasmWorkspaceCanDelta = !workspace.Master
	return wasmJSON(wasmResponse{Status: http.StatusOK, ContentType: "application/json", Body: wasmJSON(workspace)})
}

func registerWASMFunction(name string, fn func(js.Value, []js.Value) any) {
	callback := js.FuncOf(func(this js.Value, args []js.Value) any {
		// Init owns the write gate inside replaceBrowserDocument. All other
		// callbacks must finish before its successful lifetime cleanup can run.
		if name == "keynopeWasmInit" {
			return fn(this, args)
		}
		return browserDocumentRead(func() any { return fn(this, args) })
	})
	wasmCallbacks = append(wasmCallbacks, callback)
	js.Global().Set(name, callback)
}

func Run() {
	registerWASMFunction("keynopeWasmRenderParticipantPage", wasmRenderParticipantPage)
	registerWASMFunction("keynopeWasmInit", wasmEditorInit)
	registerWASMFunction("keynopeWasmRequest", wasmEditorRequest)
	registerWASMFunction("keynopeWasmUpload", wasmEditorUpload)
	registerWASMFunction("keynopeWasmPPTXImport", wasmEditorPPTXImport)
	registerWASMFunction("keynopeWasmWorkspace", wasmEditorWorkspace)
	js.Global().Set("keynopeWasmReady", true)
	select {}
}

func wasmRenderParticipantPage(_ js.Value, args []js.Value) any {
	if len(args) < 4 || len(args[0].String()) > 16<<20 {
		return wasmJSON(wasmResponse{Status: 400, Body: "invalid page document"})
	}
	deck, err := parseDeckData("Presentation.md", []byte(args[0].String()))
	if err != nil || len(deck.Slides) != 1 {
		return wasmJSON(wasmResponse{Status: 400, Body: "invalid page Markdown"})
	}
	ensureDefaultAuthoredSize()
	cols, rows := authoredRenderSize(245, 56)
	if cols > 1024 || rows > 512 {
		return wasmJSON(wasmResponse{Status: 400, Body: "Page dimensions too large"})
	}
	pageIndex, slideIndex, slideCount := args[1].Int(), args[2].Int(), args[3].Int()
	restoreParticipantLayers(&deck.Slides[0])
	pages := participantMarkdownExportPages(deck, slideIndex, slideCount, cols, rows)
	if pageIndex < 0 || pageIndex >= len(pages) {
		return wasmJSON(wasmResponse{Status: 400, Body: "Page out of range"})
	}
	payload, err := json.Marshal(exportDeck{Cols: cols, Rows: rows, Pages: []exportPage{pages[pageIndex]}})
	if err != nil {
		return wasmJSON(wasmResponse{Status: 500, Body: err.Error()})
	}
	html := exportHTMLPrefix(preservedExportHead{}, false) + "<script id=\"keynope-data\" type=\"application/json\">" + string(payload) + "</script>" + exportHTMLSuffix()
	return wasmJSON(wasmResponse{Status: 200, Body: html})
}
