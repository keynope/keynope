import Cocoa
import WebKit
import PDFKit

// PDFKit objects stay on one actor; only immutable image/PDF bytes cross the
// boundary. Encoding a large document must not block the export Cancel button.
actor SlidePDFEncoder {
    private let document = PDFDocument()

    init(title: String = "Keynope Presentation") {
        document.documentAttributes = [
            PDFDocumentAttribute.titleAttribute: title,
            PDFDocumentAttribute.creatorAttribute: "Keynope"
        ]
    }

    func append(imageData: Data) throws {
        try Task.checkCancellation()
        guard let image = NSImage(data: imageData), let page = PDFPage(image: image) else {
            throw CocoaError(.fileReadCorruptFile)
        }
        document.insert(page, at: document.pageCount)
        try Task.checkCancellation()
    }

    func finish() throws -> Data {
        try Task.checkCancellation()
        guard document.pageCount > 0, let data = document.dataRepresentation() else {
            throw CocoaError(.fileWriteUnknown)
        }
        // Encoding itself is synchronous in PDFKit. A cancelled export must
        // discard these bytes rather than replace the user's destination.
        try Task.checkCancellation()
        return data
    }
}

@MainActor
final class SlideExportProgress: NSObject {
    private let panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 380, height: 136), styleMask: [.titled], backing: .buffered, defer: false)
    private let status = NSTextField(labelWithString: "Preparing presentation…")
    private let bar = NSProgressIndicator()
    private let cancelButton = NSButton(title: "Cancel", target: nil, action: nil)
    private(set) var cancelled = false
    var onCancel: (() -> Void)?

    override init() {
        super.init()
        panel.title = "Export Presentation"
        panel.isReleasedWhenClosed = false
        status.frame = NSRect(x: 20, y: 88, width: 340, height: 24)
        status.setAccessibilityLabel("Export progress")
        bar.frame = NSRect(x: 20, y: 58, width: 340, height: 18)
        bar.style = .bar
        bar.isIndeterminate = true
        bar.startAnimation(nil)
        cancelButton.frame = NSRect(x: 270, y: 14, width: 90, height: 30)
        cancelButton.target = self
        cancelButton.action = #selector(cancel)
        cancelButton.keyEquivalent = "\u{1b}"
        for view in [status, bar, cancelButton] { panel.contentView?.addSubview(view) }
        panel.center()
    }

    func show() { panel.makeKeyAndOrderFront(nil) }
    func hide() { panel.orderOut(nil) }
    func close() { panel.close(); onCancel = nil }
    func update(_ message: String, completed: Int? = nil, total: Int = 1) {
        status.stringValue = message
        if let completed {
            bar.stopAnimation(nil)
            bar.isIndeterminate = false
            bar.maxValue = Double(max(1, total))
            bar.doubleValue = Double(completed)
        }
    }
    @objc func cancel() {
        guard !cancelled else { return }
        cancelled = true
        status.stringValue = "Cancelling…"
        cancelButton.isEnabled = false
        onCancel?()
    }
    func checkCancellation() throws {
        if cancelled { throw CancellationError() }
        try Task.checkCancellation()
    }
}

// An isolated, non-persistent view keeps export out of the live presentation
// and editor. Its HTML is the server's audience-safe standalone projection.
@MainActor
final class SlideExportRenderer: NSObject, WKNavigationDelegate {
    let view: WKWebView
    private let window: NSWindow
    private var loading: CheckedContinuation<Void, Error>?
    private var timeout: Task<Void, Never>?
    private var closed = false
    private var capture: CheckedContinuation<NSImage, Error>?
    private var captureTimeout: Task<Void, Never>?
    private var captureGeneration = 0

    override init() {
        let config = WKWebViewConfiguration()
        config.websiteDataStore = .nonPersistent()
        view = WKWebView(frame: NSRect(x: 0, y: 0, width: 1920, height: 1080), configuration: config)
        window = NSWindow(contentRect: view.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        super.init()
        window.isReleasedWhenClosed = false
        window.contentView = view
        window.ignoresMouseEvents = true
        view.navigationDelegate = self
    }

    func load(_ html: String) async throws {
        try checkOpen()
        window.orderBack(nil)
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            loading = continuation
            timeout = Task { [weak self] in
                do { try await Task.sleep(for: .seconds(30)) } catch { return }
                self?.finishLoading(NSError(domain: "sh.keynope.export", code: 1, userInfo: [NSLocalizedDescriptionKey: "The export view took too long to load."]))
            }
            view.loadHTMLString(html, baseURL: nil)
        }
    }

    private func finishLoading(_ error: Error? = nil) {
        timeout?.cancel(); timeout = nil
        guard let continuation = loading else { return }
        loading = nil
        if let error { continuation.resume(throwing: error) } else { continuation.resume() }
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) { finishLoading() }
    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) { finishLoading(error) }
    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) { finishLoading(error) }
    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        let error = NSError(domain: "sh.keynope.export", code: 2, userInfo: [NSLocalizedDescriptionKey: "The export renderer stopped unexpectedly."])
        finishLoading(error)
        finishCapture(.failure(error))
    }

    func pages() async throws -> [[String: Any]] {
        try checkOpen()
        let value = try await view.evaluateJavaScript("deck.pages.map((p,index)=>({index,label:String(p.slide+1)+(p.page?String.fromCharCode(97+p.page):'')})).filter(p=>!deck.pages[p.index].tabOnly)")
        try checkOpen()
        guard let pages = value as? [[String: Any]], !pages.isEmpty else {
            throw NSError(domain: "sh.keynope.export", code: 3, userInfo: [NSLocalizedDescriptionKey: "There are no presentation slides to export."])
        }
        return pages
    }

    func image(page: Int) async throws -> NSImage {
        try checkOpen()
        guard capture == nil else {
            throw NSError(domain: "sh.keynope.export", code: 6, userInfo: [NSLocalizedDescriptionKey: "A slide capture is already running."])
        }
        return try await withCheckedThrowingContinuation { continuation in
            capture = continuation
            captureGeneration += 1
            let generation = captureGeneration
            captureTimeout = Task { [weak self] in
                do { try await Task.sleep(for: .seconds(30)) } catch { return }
                guard let self, self.captureGeneration == generation else { return }
                self.finishCapture(.failure(NSError(domain: "sh.keynope.export", code: 7, userInfo: [NSLocalizedDescriptionKey: "The slide took too long to render. Try exporting again."])))
            }
            Task { [weak self] in
                guard let self, self.capture != nil, self.captureGeneration == generation else { return }
                do {
                    let value = try await self.view.callAsyncJavaScript("return await window.keynopePrepareSnapshot(index)", arguments: ["index": page], in: nil, contentWorld: .page)
                    guard self.capture != nil, self.captureGeneration == generation else { return }
                    guard let bounds = value as? [String: Any], let x = bounds["x"] as? Double, let y = bounds["y"] as? Double,
                          let width = bounds["width"] as? Double, let height = bounds["height"] as? Double,
                          [x,y,width,height].allSatisfy({ $0.isFinite }), width > 0, height > 0 else {
                        throw NSError(domain: "sh.keynope.export", code: 4, userInfo: [NSLocalizedDescriptionKey: "The slide did not return valid capture bounds."])
                    }
                    let config = WKSnapshotConfiguration()
                    config.rect = CGRect(x: x, y: y, width: width, height: height)
                    config.snapshotWidth = 1920
                    config.afterScreenUpdates = true
                    self.view.takeSnapshot(with: config) { [weak self] image, error in
                        guard let self, self.captureGeneration == generation else { return }
                        if let error { self.finishCapture(.failure(error)) }
                        else if let image { self.finishCapture(.success(image)) }
                        else { self.finishCapture(.failure(CocoaError(.fileReadUnknown))) }
                    }
                } catch {
                    if self.captureGeneration == generation { self.finishCapture(.failure(error)) }
                }
            }
        }
    }

    private func finishCapture(_ result: Result<NSImage, Error>) {
        captureTimeout?.cancel(); captureTimeout = nil
        guard let continuation = capture else { return }
        capture = nil
        continuation.resume(with: result)
    }

    private func checkOpen() throws {
        if closed { throw CancellationError() }
        try Task.checkCancellation()
    }

    func close() {
        guard !closed else { return }
        closed = true
        finishLoading(CancellationError())
        finishCapture(.failure(CancellationError()))
        view.stopLoading()
        view.navigationDelegate = nil
        window.close()
    }
}
