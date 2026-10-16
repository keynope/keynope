import Cocoa
import PDFKit
import WebKit

// Uses production capture/encoding, never the Keynope application, its menus,
// preferences or user decks. Accessory policy avoids foreground activation.
@main struct MediaPDFCheck {
    struct Case: Decodable { let title: String; let quality: Bool; let width: Int; let height: Int }

    @MainActor static func main() {
        let app = NSApplication.shared
        app.setActivationPolicy(.accessory)
        Task { @MainActor in
            let quality = SlideExportRenderer(), exact = SlideExportRenderer()
            defer { quality.close(); exact.close() }
            do {
                let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
                let expected = try JSONDecoder().decode([Case].self, from: Data(contentsOf: root.appendingPathComponent("expected.json")))
                try await quality.load(String(contentsOf: root.appendingPathComponent("quality.html"), encoding: .utf8))
                try await exact.load(String(contentsOf: root.appendingPathComponent("raster.html"), encoding: .utf8))
                let pages = try await quality.pages()
                try check(pages.count == expected.count, "Native media page count changed")
                let encoder = SlidePDFEncoder(title: "Owned media PDF proof")
                var report: [[String: Any]] = []
                var backingScales: [Int] = []
                for (index, info) in pages.enumerated() {
                    let width = expected[index].width, height = expected[index].height
                    try check(info["quality"] as? Bool == expected[index].quality, "Quality classification changed")
                    try quality.setPageSize(width: Double(width), height: Double(height))
                    try exact.setPageSize(width: Double(width), height: Double(height))
                    let image = try await exact.imageData(page: index)
                    guard let bitmap = NSBitmapImageRep(data: image) else { throw CocoaError(.fileReadCorruptFile) }
                    let backingScale = bitmap.pixelsWide / width
                    try check((1...2).contains(backingScale) && bitmap.pixelsWide == width * backingScale && bitmap.pixelsHigh == height * backingScale, "Unexpected snapshot backing scale")
                    backingScales.append(backingScale)
                    try png(image).write(to: root.appendingPathComponent("native-exact-\(index + 1).png"))
                    if expected[index].quality {
                        let data = try await quality.pdfData(page: index)
                        guard let pdf = PDFDocument(data: data), pdf.pageCount == 1, let page = pdf.page(at: 0) else { throw CocoaError(.fileReadCorruptFile) }
                        let text = page.string ?? ""
                        let compact = { (value: String) in value.replacingOccurrences(of: "\\s+", with: "", options: .regularExpression) }
                        try check(compact(text).contains(compact(expected[index].title)), "Native PDF lost selectable title on page \(index + 1)")
                        if expected[index].title == "INTERLEAVED PICTURES / VECTOR / TEXT" {
                            try check(compact(text).contains("TEXTBETWEENPICTURES"), "Picture sampling flattened or removed interleaved text")
                        }
                        try check(!text.contains("PRIVATE MEDIA"), "Private notes leaked into native PDF")
                        try await encoder.append(pdfData: data, expectedAspectRatio: Double(width) / Double(height))
                        try data.write(to: root.appendingPathComponent("native-quality-page-\(index + 1).pdf"))
                        try raster(page, width: width, height: height).write(to: root.appendingPathComponent("native-quality-\(index + 1).png"))
                        if ProcessInfo.processInfo.environment["KEYNOPE_MEDIA_PDF_DIAGNOSTIC"] == "1", [9,16].contains(index) {
                            let prefix = "diagnostic-\(index+1)"
                            let sources = try await quality.view.evaluateJavaScript("Array.from(document.querySelectorAll('.keynope-modern-scene>[data-keynope-pdf-media],.keynope-modern-scene>img'),n=>n.src||n.querySelector('image')?.getAttribute('href')||'')") as? [String] ?? []
                            for (position, source) in sources.enumerated() {
                                if let comma = source.firstIndex(of: ","), let image = Data(base64Encoded: String(source[source.index(after: comma)...])) {
                                    try image.write(to: root.appendingPathComponent("\(prefix)-installed-\(position).png"))
                                }
                            }
                            let state = try await quality.view.callAsyncJavaScript("""
                                return Array.from(document.querySelectorAll('.keynope-modern-scene>[data-keynope-pdf-media],.keynope-modern-scene>img'),n=>{const s=getComputedStyle(n),r=n.getBoundingClientRect();return {type:n.tagName,naturalWidth:n.naturalWidth,naturalHeight:n.naturalHeight,style:n.style.cssText,rect:{x:r.x,y:r.y,width:r.width,height:r.height},visibility:s.visibility,display:s.display}});
                                """, arguments: [:], in: nil, contentWorld: .page)
                            if let state { try JSONSerialization.data(withJSONObject: state, options: [.prettyPrinted,.sortedKeys]).write(to: root.appendingPathComponent(prefix+"-state.json")) }
                            let snapshot = WKSnapshotConfiguration()
                            snapshot.rect = CGRect(x:0,y:0,width:width,height:height)
                            snapshot.snapshotWidth = NSNumber(value:width)
                            snapshot.afterScreenUpdates = true
                            let sampled: Data = try await withCheckedThrowingContinuation { next in
                                quality.view.takeSnapshot(with: snapshot) { image,error in
                                    if let error { next.resume(throwing:error) }
                                    else if let data=image?.tiffRepresentation { next.resume(returning:data) }
                                    else { next.resume(throwing:CocoaError(.fileReadCorruptFile)) }
                                }
                            }
                            try png(sampled).write(to:root.appendingPathComponent(prefix+"-scene.png"))
                            let config = WKPDFConfiguration()
                            config.rect = snapshot.rect
                            let repeated = try await quality.view.pdf(configuration:config)
                            try repeated.write(to:root.appendingPathComponent(prefix+"-repeated.pdf"))
                        }
                    } else {
                        try await encoder.append(imageData: image)
                    }
                    report.append(["title": expected[index].title, "quality": expected[index].quality])
                    print("Captured \(index + 1)/\(pages.count): \(expected[index].title)")
                }
                let cancelledInput = try Data(contentsOf: root.appendingPathComponent("native-exact-1.png"))
                let cancelled = await Task {
                    withUnsafeCurrentTask { $0?.cancel() }
                    do { try await encoder.append(imageData: cancelledInput); return false }
                    catch is CancellationError { return true }
                    catch { return false }
                }.value
                try check(cancelled, "Cancelled raster append was accepted")
                var rejected = false
                do { try await encoder.append(imageData: Data("not an image".utf8)) }
                catch { rejected = true }
                try check(rejected, "Invalid raster append was accepted")
                let data = try await encoder.finish()
                guard let pdf = PDFDocument(data: data), pdf.pageCount == expected.count else { throw CocoaError(.fileReadCorruptFile) }
                for (index, item) in expected.enumerated() {
                    guard let page = pdf.page(at: index) else { throw CocoaError(.fileReadCorruptFile) }
                    let bounds = page.bounds(for: .mediaBox)
                    try check(abs(bounds.width - Double(item.width)) < 1 && abs(bounds.height - Double(item.height)) < 1, "Assembled media page dimensions changed")
                    let text = page.string ?? ""
                    try check(!text.contains("PRIVATE MEDIA"), "Private notes leaked into assembled PDF")
                    if item.quality {
                        let compact = { (value: String) in value.replacingOccurrences(of: "\\s+", with: "", options: .regularExpression) }
                        try check(compact(text).contains(compact(item.title)), "Assembly lost selectable media title")
                    }
                    // Compare embedded raster pages at their original Retina
                    // resolution, not a second, independently downsampled grid.
                    try raster(page, width: item.width, height: item.height, scale: item.quality ? 1 : backingScales[index]).write(to: root.appendingPathComponent("native-assembled-\(index + 1).png"))
                }
                try data.write(to: root.appendingPathComponent("native-media-proof.pdf"))
                try JSONSerialization.data(withJSONObject: ["passed": true, "pages": report], options: .prettyPrinted).write(to: root.appendingPathComponent("native-report.json"))
                print("PASS: \(pages.count) native media pages, selectable quality titles and exact fallback")
                app.terminate(nil)
            } catch { print("FAIL: \(error)"); exit(1) }
        }
        app.run()
    }

    private static func check(_ value: Bool, _ message: String) throws {
        if !value { throw NSError(domain: "sh.keynope.media-pdf-test", code: 1, userInfo: [NSLocalizedDescriptionKey: message]) }
    }

    private static func png(_ data: Data) throws -> Data {
        guard let image = NSBitmapImageRep(data: data), let png = image.representation(using: .png, properties: [:]) else { throw CocoaError(.fileReadCorruptFile) }
        return png
    }

    @MainActor private static func raster(_ page: PDFPage, width designWidth: Int, height designHeight: Int, scale: Int = 1) throws -> Data {
        let width = designWidth * scale, height = designHeight * scale
        guard let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: width, pixelsHigh: height, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0), let graphics = NSGraphicsContext(bitmapImageRep: bitmap) else { throw CocoaError(.fileReadCorruptFile) }
        let context = graphics.cgContext
        context.setFillColor(NSColor.white.cgColor)
        context.fill(CGRect(x: 0, y: 0, width: width, height: height))
        let bounds = page.bounds(for: .mediaBox)
        context.scaleBy(x: CGFloat(width) / bounds.width, y: CGFloat(height) / bounds.height)
        context.translateBy(x: -bounds.minX, y: -bounds.minY)
        page.draw(with: .mediaBox, to: context)
        guard let png = bitmap.representation(using: .png, properties: [:]) else { throw CocoaError(.fileReadCorruptFile) }
        return png
    }
}
