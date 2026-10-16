import Cocoa
import WebKit
import PDFKit
import ImageIO

// Recover a picture's alpha without asking WebKit for a transparent snapshot
// (which can silently return an opaque white matte). All decode/pixel/PNG work
// belongs to this actor, not AppKit's UI actor. Only owned bytes cross actors.
actor SlidePDFMediaSampler {
    static let maximumPixels = 16_000_000

    func recover(black: Data, white: Data, pixelLimit: Int) throws -> Data {
        try Task.checkCancellation()
        guard pixelLimit > 0 else { throw CocoaError(.fileReadCorruptFile) }
        return try autoreleasepool {
            func decode(_ data: Data) throws -> NSBitmapImageRep {
                guard data.count <= 128 * 1024 * 1024,
                      let source = CGImageSourceCreateWithData(data as CFData, nil), CGImageSourceGetCount(source) == 1,
                      let info = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
                      let w = info[kCGImagePropertyPixelWidth] as? NSNumber, let h = info[kCGImagePropertyPixelHeight] as? NSNumber,
                      w.doubleValue.isFinite, h.doubleValue.isFinite, w.doubleValue >= 1, h.doubleValue >= 1,
                      w.doubleValue <= 20000, h.doubleValue <= 20000,
                      w.doubleValue * h.doubleValue <= Double(min(pixelLimit, Self.maximumPixels)),
                      let bitmap = NSBitmapImageRep(data: data), bitmap.bitsPerSample == 8, !bitmap.isPlanar,
                      (3...4).contains(bitmap.samplesPerPixel), bitmap.colorSpace.colorSpaceModel == .rgb,
                      bitmap.pixelsWide == w.intValue, bitmap.pixelsHigh == h.intValue,
                      bitmap.bytesPerRow >= bitmap.pixelsWide * bitmap.samplesPerPixel else {
                    throw CocoaError(.fileReadCorruptFile)
                }
                return bitmap
            }
            let b = try decode(black), w = try decode(white)
            guard b.pixelsWide == w.pixelsWide, b.pixelsHigh == w.pixelsHigh,
                  let bp = b.bitmapData, let wp = w.bitmapData,
                  let result = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: b.pixelsWide, pixelsHigh: b.pixelsHigh,
                    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB,
                    bitmapFormat: .alphaNonpremultiplied, bytesPerRow: b.pixelsWide * 4, bitsPerPixel: 32),
                  let out = result.bitmapData else { throw CocoaError(.fileReadCorruptFile) }
            let bi = b.hasAlpha && b.bitmapFormat.contains(.alphaFirst) ? 1 : 0
            let wi = w.hasAlpha && w.bitmapFormat.contains(.alphaFirst) ? 1 : 0
            // bitmapData is an unowned pointer. Keep both decoded owners alive
            // until the final read, including in optimized release builds.
            try withExtendedLifetime((b,w)) {
              for y in 0..<b.pixelsHigh {
                if y % 64 == 0 { try Task.checkCancellation() }
                for x in 0..<b.pixelsWide {
                    let bo = y * b.bytesPerRow + x * b.samplesPerPixel + bi
                    let wo = y * w.bytesPerRow + x * w.samplesPerPixel + wi
                    let ba = b.bitmapFormat.contains(.alphaFirst) ? bo-1 : bo+3
                    let wa = w.bitmapFormat.contains(.alphaFirst) ? wo-1 : wo+3
                    guard (!b.hasAlpha || bp[ba] == 255), (!w.hasAlpha || wp[wa] == 255) else {
                        throw CocoaError(.fileReadCorruptFile)
                    }
                    let d0 = Int(wp[wo]) - Int(bp[bo]), d1 = Int(wp[wo+1]) - Int(bp[bo+1]), d2 = Int(wp[wo+2]) - Int(bp[bo+2])
                    let median = d0 + d1 + d2 - min(d0, min(d1,d2)) - max(d0, max(d1,d2))
                    let alpha = max(0,min(255,255-median)), offset = y * result.bytesPerRow + x * 4
                    for c in 0..<3 { out[offset+c] = alpha == 0 ? 0 : UInt8(max(0,min(255,(Int(bp[bo+c])*255+alpha/2)/alpha))) }
                    out[offset+3] = UInt8(alpha)
                }
              }
            }
            try Task.checkCancellation()
            guard let png = result.representation(using: .png, properties: [:]), png.count <= 64 * 1024 * 1024 else {
                throw CocoaError(.fileWriteOutOfSpace)
            }
            try Task.checkCancellation()
            return png
        }
    }
}

struct SlidePDFImageCheck: Hashable, Sendable {
    let width: Int
    let height: Int
}

// Count image drawing operations, not merely entries in a resource dictionary:
// an unused image or a soft mask cannot prove that a picture was painted.
// Form XObjects may nest; all traversal has shared depth/work limits.
private final class SlidePDFImageUsage {
    var images: [SlidePDFImageCheck: Int] = [:]
    var remaining = 4096
    var failed = false

    func scan(_ stream: CGPDFContentStreamRef, resources: CGPDFDictionaryRef, depth: Int = 0) {
        guard !failed, depth < 8, let table = CGPDFOperatorTableCreate() else { failed = true; return }
        let context = Context(usage: self, resources: resources, depth: depth)
        CGPDFOperatorTableSetCallback(table, "Do") { scanner, info in
            guard let info else { return }
            Unmanaged<Context>.fromOpaque(info).takeUnretainedValue().draw(scanner)
        }
        let scanner = CGPDFScannerCreate(stream, table, Unmanaged.passUnretained(context).toOpaque())
        if !withExtendedLifetime((context, table, stream), { CGPDFScannerScan(scanner) }) { failed = true }
    }

    private final class Context {
        let usage: SlidePDFImageUsage
        let resources: CGPDFDictionaryRef
        let depth: Int
        init(usage: SlidePDFImageUsage, resources: CGPDFDictionaryRef, depth: Int) {
            self.usage = usage; self.resources = resources; self.depth = depth
        }
        func draw(_ scanner: CGPDFScannerRef) {
            guard !usage.failed, usage.remaining > 0, !Task.isCancelled else {
                usage.failed = true; CGPDFScannerStop(scanner); return
            }
            usage.remaining -= 1
            var name: UnsafePointer<CChar>?
            var object: CGPDFStreamRef?
            let content = CGPDFScannerGetContentStream(scanner)
            guard CGPDFScannerPopName(scanner, &name), let name,
                  let resource = CGPDFContentStreamGetResource(content, "XObject", name),
                  CGPDFObjectGetValue(resource, .stream, &object), let object else {
                usage.failed = true; CGPDFScannerStop(scanner); return
            }
            guard let dictionary = CGPDFStreamGetDictionary(object) else { usage.failed = true; return }
            var subtype: UnsafePointer<CChar>?
            guard CGPDFDictionaryGetName(dictionary, "Subtype", &subtype), let subtype else {
                usage.failed = true; CGPDFScannerStop(scanner); return
            }
            switch String(cString: subtype) {
            case "Image":
                var width = 0, height = 0
                guard CGPDFDictionaryGetInteger(dictionary, "Width", &width), CGPDFDictionaryGetInteger(dictionary, "Height", &height),
                      width > 0, height > 0, width <= 20000, height <= 20000 else { usage.failed = true; return }
                usage.images[SlidePDFImageCheck(width: width, height: height), default: 0] += 1
            case "Form":
                var local: CGPDFDictionaryRef?
                _ = CGPDFDictionaryGetDictionary(dictionary, "Resources", &local)
                let nested = CGPDFContentStreamCreateWithStream(object, local ?? resources, content)
                usage.scan(nested, resources: local ?? resources, depth: depth + 1)
            default:
                break
            }
        }
    }
}

actor SlidePDFImageValidator {
    static let shared = SlidePDFImageValidator()
    func validate(_ data: Data, checks: [SlidePDFImageCheck]) throws {
        try Task.checkCancellation()
        guard !checks.isEmpty else { return }
        guard checks.count <= 256,
              checks.allSatisfy({ $0.width > 0 && $0.height > 0 && $0.width <= 20000 && $0.height <= 20000 }),
              let provider = CGDataProvider(data: data as CFData), let document = CGPDFDocument(provider), document.numberOfPages == 1,
              let page = document.page(at: 1) else { throw Self.missingImage() }
        var resources: CGPDFDictionaryRef?
        guard let dictionary = page.dictionary, CGPDFDictionaryGetDictionary(dictionary, "Resources", &resources), let resources else { throw Self.missingImage() }
        let usage = SlidePDFImageUsage()
        withExtendedLifetime((provider, document)) { usage.scan(CGPDFContentStreamCreateWithPage(page), resources: resources) }
        try Task.checkCancellation()
        var expected: [SlidePDFImageCheck: Int] = [:]
        for check in checks { expected[check, default: 0] += 1 }
        guard !usage.failed, expected.allSatisfy({ usage.images[$0.key, default: 0] >= $0.value }) else { throw Self.missingImage() }
    }
    private static func missingImage() -> NSError {
        NSError(domain: "sh.keynope.export", code: 12, userInfo: [NSLocalizedDescriptionKey: "The quality PDF did not preserve a sampled image. This slide needs exact-appearance capture."])
    }
}

private struct SlidePDFCaptionCheck: Sendable {
    let text: String
    let x, y, width, height: Double

    static func decode(_ value: Any?) throws -> [Self] {
        guard let value else { return [] }
        guard let source = value as? [[String: Any]] else { throw CocoaError(.fileReadCorruptFile) }
        guard source.count <= 4096 else { throw CocoaError(.fileReadCorruptFile) }
        return try source.map { check in
            guard let text = check["text"] as? String, !text.isEmpty, text.count <= 4096,
                  let x = check["x"] as? Double, let y = check["y"] as? Double,
                  let width = check["width"] as? Double, let height = check["height"] as? Double,
                  [x,y,width,height].allSatisfy({ $0.isFinite }), width > 0, height > 0, width <= 20000, height <= 20000 else {
                throw CocoaError(.fileReadCorruptFile)
            }
            return Self(text: text, x: x, y: y, width: width, height: height)
        }
    }
}

// PDFKit inspection stays off the UI actor, just like final encoding. It owns
// its PDF objects; only immutable bytes and caption coordinates cross actors.
private actor SlidePDFCaptionValidator {
    static let shared = SlidePDFCaptionValidator()

    func validate(_ data: Data, checks: [SlidePDFCaptionCheck]) throws {
        try Task.checkCancellation()
        guard !checks.isEmpty else { return }
        guard let document = PDFDocument(data: data), document.pageCount == 1,
              let page = document.page(at: 0) else { throw CocoaError(.fileReadCorruptFile) }
        let text = page.string ?? "", height = page.bounds(for: .mediaBox).height
        for check in checks {
            try Task.checkCancellation()
            let expected = CGRect(x:check.x-2, y:height-check.y-check.height-2, width:check.width+4, height:check.height+4)
            var pattern = check.text.split(whereSeparator: { $0.isWhitespace }).map { NSRegularExpression.escapedPattern(for: String($0)) }.joined(separator: "\\s+")
            if check.text.first.map({ $0.isLetter || $0.isNumber }) == true { pattern = "(?<![\\p{L}\\p{N}])" + pattern }
            if check.text.last.map({ $0.isLetter || $0.isNumber }) == true { pattern += "(?![\\p{L}\\p{N}])" }
            let expression = try NSRegularExpression(pattern: pattern)
            let matches = expression.matches(in: text, range: NSRange(text.startIndex..., in: text))
            let valid = matches.prefix(4096).contains { match in
                guard let selection = page.selection(for: match.range) else { return false }
                return expected.contains(selection.bounds(for: page))
            }
            if !valid {
                throw NSError(domain: "sh.keynope.export", code: 10, userInfo: [NSLocalizedDescriptionKey: "The quality PDF did not preserve a chart caption. This slide needs exact-appearance capture."])
            }
        }
        try Task.checkCancellation()
    }
}

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
        guard let image = NSImage(data: imageData),
              [image.size.width, image.size.height].allSatisfy({ $0.isFinite && $0 > 0 && $0 <= 20000 }),
              let source = CGImageSourceCreateWithData(imageData as CFData, nil),
              let pixels = CGImageSourceCreateImageAtIndex(source, 0, nil) else {
            throw CocoaError(.fileReadCorruptFile)
        }
        try Task.checkCancellation()
        // PDFPage(image:) redraws through NSImage and changes fine antialiased
        // raster colours (notably dense Braille), even without JPEG compression.
        // Keep the decoded image/profile/alpha directly in a bounded PDF page.
        let data = NSMutableData()
        var bounds = CGRect(origin: .zero, size: image.size)
        guard let consumer = CGDataConsumer(data: data),
              let context = CGContext(consumer: consumer, mediaBox: &bounds, nil) else {
            throw CocoaError(.fileWriteUnknown)
        }
        context.beginPDFPage(nil)
        context.draw(pixels, in: bounds)
        context.endPDFPage()
        context.closePDF()
        try Task.checkCancellation()
        try append(pdfData: data as Data, expectedAspectRatio: bounds.width / bounds.height)
    }

    // A WKWebView capture is one explicitly bounded slide, not a paginated
    // web document. Keep its text/vector resources when assembling the deck.
    func append(pdfData: Data, expectedAspectRatio: Double? = nil) throws {
        try Task.checkCancellation()
        guard let source = PDFDocument(data: pdfData), source.pageCount == 1,
              let page = source.page(at: 0) else { throw CocoaError(.fileReadCorruptFile) }
        let bounds = page.bounds(for: .mediaBox)
        guard [bounds.width, bounds.height].allSatisfy({ $0.isFinite && $0 > 0 && $0 <= 20000 }) else {
            throw CocoaError(.fileReadCorruptFile)
        }
        if let expectedAspectRatio {
            guard expectedAspectRatio.isFinite && expectedAspectRatio > 0,
                  abs(bounds.width / bounds.height - expectedAspectRatio) < 0.005 else {
                throw NSError(domain: "sh.keynope.export", code: 9, userInfo: [NSLocalizedDescriptionKey: "The PDF page did not preserve the slide's aspect ratio."])
            }
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

    func show() {
#if KEYNOPE_NATIVE_ACCEPTANCE
        if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_BACKGROUND"] == "1" { panel.orderBack(nil);return }
#endif
        panel.makeKeyAndOrderFront(nil)
    }
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

    func beginCommit() throws {
        try checkCancellation()
        // Bulk IO is already complete. Once atomic replacement begins,
        // cancellation cannot undo it; do not offer a misleading Cancel action.
        status.stringValue = "Finishing export…"
        cancelButton.isEnabled = false
    }
}

// An isolated, non-persistent view keeps export out of the live presentation
// and editor. Its HTML is the server's audience-safe standalone projection.
@MainActor
final class SlideExportRenderer: NSObject, WKNavigationDelegate {
    let view: WKWebView
    private let window: NSWindow
    private var loading: CheckedContinuation<Void, Error>?
    private var loadingNavigation: ObjectIdentifier?
    private var loadGeneration = 0
    private var timeout: Task<Void, Never>?
    private var closed = false
    // Continuations may resume outside the main actor. Keep AppKit objects on
    // the actor and pass only immutable, Sendable image bytes across it.
    private var capture: CheckedContinuation<Data, Error>?
    private var captureTimeout: Task<Void, Never>?
    private var captureTask: Task<Void, Never>?
    private var captureGeneration = 0
    private let mediaSampler = SlidePDFMediaSampler()
    private var mediaWorkRemaining = 256_000_000
    private var mediaBytesRemaining = 128 * 1024 * 1024
    private var mediaSnapshot: CheckedContinuation<Data, Error>?
    private var mediaSnapshotGeneration = 0

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
        guard loading == nil, capture == nil else {
            throw NSError(domain: "sh.keynope.export", code: 6, userInfo: [NSLocalizedDescriptionKey: "The export renderer is already busy."])
        }
        mediaWorkRemaining = 256_000_000
        mediaBytesRemaining = 128 * 1024 * 1024
        window.orderBack(nil)
        loadGeneration += 1
        let generation = loadGeneration
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
                loading = continuation
                timeout = Task { [weak self] in
                    do { try await Task.sleep(for: .seconds(30)) } catch { return }
                    guard let self, self.loadGeneration == generation else { return }
                    self.finishLoading(NSError(domain: "sh.keynope.export", code: 1, userInfo: [NSLocalizedDescriptionKey: "The export view took too long to load."]))
                }
                if let navigation = view.loadHTMLString(html, baseURL: nil) {
                    loadingNavigation = ObjectIdentifier(navigation)
                } else {
                    finishLoading(Self.navigationError("The export page could not be loaded."))
                }
            }
        } onCancel: { [weak self] in
            Task { @MainActor [weak self] in
                guard let self, self.loading != nil, self.loadGeneration == generation else { return }
                self.close()
            }
        }
        try checkOpen()
    }

    private func finishLoading(_ error: Error? = nil) {
        timeout?.cancel(); timeout = nil
        guard let continuation = loading else { return }
        loading = nil
        loadingNavigation = nil
        if let error { continuation.resume(throwing: error) } else { continuation.resume() }
    }

    nonisolated func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        let identity = navigation.map(ObjectIdentifier.init)
        Task { @MainActor [weak self] in
            guard let self, let identity, self.loadingNavigation == identity else { return }
            self.finishLoading()
        }
    }

    nonisolated func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        let message = error.localizedDescription
        let identity = navigation.map(ObjectIdentifier.init)
        Task { @MainActor [weak self] in
            guard let self, let identity, self.loadingNavigation == identity else { return }
            self.finishLoading(Self.navigationError(message))
        }
    }

    nonisolated func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        let message = error.localizedDescription
        let identity = navigation.map(ObjectIdentifier.init)
        Task { @MainActor [weak self] in
            guard let self, let identity, self.loadingNavigation == identity else { return }
            self.finishLoading(Self.navigationError(message))
        }
    }

    nonisolated func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        Task { @MainActor [weak self] in
            guard let self else { return }
            let error = NSError(domain: "sh.keynope.export", code: 2, userInfo: [NSLocalizedDescriptionKey: "The export renderer stopped unexpectedly."])
            self.finishLoading(error)
            self.finishCapture(.failure(error))
        }
    }

    private static func navigationError(_ message: String) -> NSError {
        NSError(domain: "sh.keynope.export", code: 8, userInfo: [NSLocalizedDescriptionKey: message])
    }

    func pages() async throws -> [[String: Any]] {
        try checkOpen()
        let value = try await view.evaluateJavaScript("window.keynopePDFManifest ? window.keynopePDFManifest.pages.map(({index,label,width,height,quality,reasons})=>({index,label,width,height,quality,reasons})) : deck.pages.map((p,index)=>({index,label:String(p.slide+1)+(p.page?String.fromCharCode(97+p.page):''),width:p.scene?.width||1920,height:p.scene?.height||1080})).filter(p=>!deck.pages[p.index].tabOnly)")
        try checkOpen()
        guard let pages = value as? [[String: Any]], !pages.isEmpty else {
            throw NSError(domain: "sh.keynope.export", code: 3, userInfo: [NSLocalizedDescriptionKey: "There are no presentation slides to export."])
        }
        return pages
    }

    func imageData(page: Int) async throws -> Data {
        try await captureData(page: page, vector: false)
    }

    func pdfData(page: Int) async throws -> Data {
        try await captureData(page: page, vector: true)
    }

    func setPageSize(width: Double, height: Double) throws {
        try checkOpen()
        guard [width,height].allSatisfy({ $0.isFinite && $0 > 0 && $0 <= 20000 }) else {
            throw CocoaError(.fileReadCorruptFile)
        }
        window.setContentSize(NSSize(width: width, height: height))
        view.setFrameSize(NSSize(width: width, height: height))
    }

    private func captureData(page: Int, vector: Bool) async throws -> Data {
        try checkOpen()
        guard capture == nil, loading == nil else {
            throw NSError(domain: "sh.keynope.export", code: 6, userInfo: [NSLocalizedDescriptionKey: "A slide capture is already running."])
        }
        let requestedGeneration = captureGeneration + 1
        let result: Data = try await withTaskCancellationHandler {
          try await withCheckedThrowingContinuation { continuation in
            capture = continuation
            captureGeneration += 1
            let generation = captureGeneration
            captureTimeout = Task { [weak self] in
                do { try await Task.sleep(for: .seconds(30)) } catch { return }
                guard let self, self.captureGeneration == generation else { return }
                self.finishCapture(.failure(NSError(domain: "sh.keynope.export", code: 7, userInfo: [NSLocalizedDescriptionKey: "The slide took too long to render. Try exporting again."])))
            }
            captureTask = Task { [weak self] in
                guard let self, self.capture != nil, self.captureGeneration == generation else { return }
                do {
                    let value = try await self.view.callAsyncJavaScript("return await window.keynopePrepareSnapshot(index)", arguments: ["index": page], in: nil, contentWorld: .page)
                    guard self.capture != nil, self.captureGeneration == generation else { return }
                    guard let bounds = value as? [String: Any], let x = bounds["x"] as? Double, let y = bounds["y"] as? Double,
                          let width = bounds["width"] as? Double, let height = bounds["height"] as? Double,
                          [x,y,width,height].allSatisfy({ $0.isFinite }), width > 0, height > 0 else {
                        throw NSError(domain: "sh.keynope.export", code: 4, userInfo: [NSLocalizedDescriptionKey: "The slide did not return valid capture bounds."])
                    }
                    if vector {
                        let images = try await self.samplePDFMedia(generation: generation, page: CGRect(x:x,y:y,width:width,height:height))
                        try self.checkCapture(generation)
                        let textChecks = try SlidePDFCaptionCheck.decode(bounds["pdfTextChecks"])
                        self.captureQualityPDF(rect: CGRect(x:x,y:y,width:width,height:height), checks: textChecks, images: images, generation: generation)
                        return
                    }
                    let config = WKSnapshotConfiguration()
                    config.rect = CGRect(x: x, y: y, width: width, height: height)
                    // Snapshot density follows the authored page, not an
                    // implicit widescreen width. Otherwise portrait exports
                    // are resampled and ultrawide/custom pages lose detail.
                    config.snapshotWidth = NSNumber(value: width)
                    config.afterScreenUpdates = true
                    self.view.takeSnapshot(with: config) { [weak self] image, error in
                        guard let self, self.captureGeneration == generation else { return }
                        if let error { self.finishCapture(.failure(error)) }
                        else if let image, let data = image.tiffRepresentation { self.finishCapture(.success(data)) }
                        else { self.finishCapture(.failure(CocoaError(.fileReadUnknown))) }
                    }
                } catch {
                    if self.captureGeneration == generation { self.finishCapture(.failure(error)) }
                }
            }
          }
        } onCancel: { [weak self] in
            Task { @MainActor [weak self] in
                guard let self, self.capture != nil,
                      self.captureGeneration == requestedGeneration else { return }
                // Cancel the page-owned readiness promise as well as Swift's
                // continuation. A task cancellation must not wait for the
                // render timeout or leave an invisible renderer alive.
                self.close()
            }
        }
        try checkOpen()
        return result
    }

    private func finishCapture(_ result: Result<Data, Error>) {
        finishMediaSnapshot(.failure(CancellationError()))
        captureTimeout?.cancel(); captureTimeout = nil
        captureTask?.cancel(); captureTask = nil
        guard let continuation = capture else { return }
        capture = nil
        continuation.resume(with: result)
    }

    private func checkCapture(_ generation: Int) throws {
        try checkOpen()
        guard capture != nil, captureGeneration == generation else { throw CancellationError() }
    }

    private func captureQualityPDF(rect: CGRect, checks: [SlidePDFCaptionCheck], images: [SlidePDFImageCheck], generation: Int, retry: Bool = true) {
        let config = WKPDFConfiguration()
        config.rect = rect
        view.createPDF(configuration: config) { [weak self] result in
            guard let self, self.capture != nil, self.captureGeneration == generation else { return }
            do {
                let data = try result.get()
                self.captureTask = Task { [weak self] in
                    do {
                        try await SlidePDFImageValidator.shared.validate(data, checks: images)
                        try await SlidePDFCaptionValidator.shared.validate(data, checks: checks)
                        guard let self else { return }
                        try self.checkCapture(generation)
                        self.finishCapture(.success(data))
                    } catch {
                        guard let self, self.capture != nil, self.captureGeneration == generation else { return }
                        // Reuse the exact prepared DOM and decoded pictures. A
                        // first WebKit PDF can omit newly installed PNGs even
                        // though snapshots paint them. Retry once, validate it
                        // independently, then use the existing exact fallback.
                        let failure = error as NSError
                        if retry, !images.isEmpty, failure.domain == "sh.keynope.export", failure.code == 12 {
                            self.captureQualityPDF(rect: rect, checks: checks, images: images, generation: generation, retry: false)
                        } else { self.finishCapture(.failure(error)) }
                    }
                }
            } catch { self.finishCapture(.failure(error)) }
        }
    }

    private func samplePDFMedia(generation: Int, page: CGRect) async throws -> [SlidePDFImageCheck] {
        let value = try await view.callAsyncJavaScript("return typeof window.keynopePDFMediaPlan==='function' ? window.keynopePDFMediaPlan() : []", contentWorld: .page)
        try checkCapture(generation)
        guard let plan = value as? [[String: Any]], plan.count <= 256 else { throw CocoaError(.fileReadCorruptFile) }
        var identifiers = Set<String>()
        var images: [SlidePDFImageCheck] = []
        for item in plan {
            try checkCapture(generation)
            guard let id = item["id"] as? String, !id.isEmpty, id.utf8.count <= 512,
                  identifiers.insert(id).inserted,
                  let x = item["x"] as? Double, let y = item["y"] as? Double,
                  let width = item["width"] as? Double, let height = item["height"] as? Double,
                  [x,y,width,height].allSatisfy({ $0.isFinite }), x >= 0, y >= 0,
                  width > 0, height > 0, width <= 20000, height <= 20000,
                  x+width <= page.width, y+height <= page.height else { throw CocoaError(.fileReadCorruptFile) }
            let rect = CGRect(x:page.minX+x,y:page.minY+y,width:width,height:height)
            let scale = max(1, window.backingScaleFactor)
            let pixels = ceil(width * scale) * ceil(height * scale)
            guard pixels <= Double(SlidePDFMediaSampler.maximumPixels), pixels * 3 <= Double(mediaWorkRemaining) else {
                throw NSError(domain: "sh.keynope.export", code: 11, userInfo: [NSLocalizedDescriptionKey: "This slide exceeds the quality image sampling budget. Use exact-appearance output."])
            }
            mediaWorkRemaining -= Int(pixels) * 3
            let black = try await snapshotPDFMedia(id: id, matte: "#000000", rect:rect, generation: generation)
            let white = try await snapshotPDFMedia(id: id, matte: "#ffffff", rect:rect, generation: generation)
            let png = try await mediaSampler.recover(black: black, white: white, pixelLimit: Int(pixels))
            try checkCapture(generation)
            guard png.count <= mediaBytesRemaining else { throw CocoaError(.fileWriteOutOfSpace) }
            mediaBytesRemaining -= png.count
            guard let source = CGImageSourceCreateWithData(png as CFData, nil),
                  let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
                  let pixelWidth = properties[kCGImagePropertyPixelWidth] as? Int,
                  let pixelHeight = properties[kCGImagePropertyPixelHeight] as? Int else { throw CocoaError(.fileReadCorruptFile) }
            images.append(SlidePDFImageCheck(width: pixelWidth, height: pixelHeight))
            _ = try await view.callAsyncJavaScript("await window.keynopePDFMediaInstall(id,source);return true", arguments: ["id":id,"source":"data:image/png;base64," + png.base64EncodedString()], contentWorld: .page)
            try checkCapture(generation)
        }
        return images
    }

    private func snapshotPDFMedia(id: String, matte: String, rect: CGRect, generation: Int) async throws -> Data {
        try checkCapture(generation)
        let value = try await view.callAsyncJavaScript("return window.keynopePDFMediaMatte(id,matte)", arguments: ["id":id,"matte":matte], contentWorld: .page)
        try checkCapture(generation)
        guard let b = value as? [String: Any], let x = b["x"] as? Double, let y = b["y"] as? Double,
              let width = b["width"] as? Double, let height = b["height"] as? Double,
              [x,y,width,height].allSatisfy({ $0.isFinite }), x >= 0, y >= 0, width > 0, height > 0,
              x+width <= view.bounds.width+1, y+height <= view.bounds.height+1,
              abs(x-rect.minX) < 0.001, abs(y-rect.minY) < 0.001,
              abs(width-rect.width) < 0.001, abs(height-rect.height) < 0.001 else { throw CocoaError(.fileReadCorruptFile) }
        return try await takePDFMediaSnapshot(rect: rect, generation: generation)
    }

    private func takePDFMediaSnapshot(rect: CGRect, generation: Int) async throws -> Data {
        try checkCapture(generation)
        let config = WKSnapshotConfiguration()
        config.rect = rect
        config.snapshotWidth = NSNumber(value: rect.width)
        config.afterScreenUpdates = true
        mediaSnapshotGeneration += 1
        let phase = mediaSnapshotGeneration
        return try await withCheckedThrowingContinuation { continuation in
            mediaSnapshot = continuation
            view.takeSnapshot(with: config) { [weak self] image, error in
                guard let self, self.captureGeneration == generation, self.mediaSnapshotGeneration == phase else { return }
                if let error { self.finishMediaSnapshot(.failure(error)) }
                else if let data = image?.tiffRepresentation { self.finishMediaSnapshot(.success(data)) }
                else { self.finishMediaSnapshot(.failure(CocoaError(.fileReadUnknown))) }
            }
        }
    }

    private func finishMediaSnapshot(_ result: Result<Data, Error>) {
        guard let continuation = mediaSnapshot else { return }
        mediaSnapshot = nil
        mediaSnapshotGeneration += 1
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
        captureGeneration += 1
        view.stopLoading()
        view.navigationDelegate = nil
        // Invalidate a page-owned JS promise as well as our Swift continuation.
        // stopLoading alone need not settle an already-running async script.
        view.loadHTMLString("", baseURL: nil)
        window.close()
    }
}
