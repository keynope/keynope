import Cocoa
import PDFKit

// Isolated accessory process: never opens/activates the user's editor or panels.
@main struct PageSizePDFCheck {
    @MainActor static func main() {
        let app = NSApplication.shared
        app.setActivationPolicy(.accessory)
        Task { @MainActor in
            do {
                let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
                for (width, height) in [(1440, 1080), (1080, 1920), (2560, 1080)] {
                    let dir = root.appendingPathComponent("\(width)x\(height)")
                    let quality = SlideExportRenderer(), exact = SlideExportRenderer()
                    defer { quality.close(); exact.close() }
                    try await quality.load(String(contentsOf: dir.appendingPathComponent("quality.html"), encoding: .utf8))
                    try await exact.load(String(contentsOf: dir.appendingPathComponent("exact.html"), encoding: .utf8))
                    let pages = try await quality.pages()
                    try check(pages.count == 2, "Private tab leaked into native page list")
                    let imagePages = try await exact.pages()
                    try check(imagePages.count == 2 && imagePages.allSatisfy { ($0["width"] as? Double) == Double(width) && ($0["height"] as? Double) == Double(height) }, "PNG page list lost custom dimensions")
                    let combined = SlidePDFEncoder(title: "Custom page-size proof")
                    try quality.setPageSize(width: Double(width), height: Double(height))
                    let data = try await quality.pdfData(page: 0)
                    try checkPage(data, width: width, height: height, selectable: true)
                    try data.write(to: dir.appendingPathComponent("native-quality.pdf"))
                    try await combined.append(pdfData: data, expectedAspectRatio: Double(width) / Double(height))
                    try exact.setPageSize(width: Double(width), height: Double(height))
                    let image = try await exact.imageData(page: 1)
                    guard let bitmap = NSBitmapImageRep(data: image), let png = bitmap.representation(using: .png, properties: [:]) else {
                        throw CocoaError(.fileReadCorruptFile)
                    }
                    let scale = Double(bitmap.pixelsWide) / Double(width)
                    try check((1...2).contains(scale) && abs(Double(bitmap.pixelsHigh) - Double(height) * scale) <= 1, "Snapshot density still assumes 1920 pixels: \(bitmap.pixelsWide)x\(bitmap.pixelsHigh)")
                    try png.write(to: dir.appendingPathComponent("native-exact.png"))
                    let raster = SlidePDFEncoder(title: "Exact page-size proof")
                    try await raster.append(imageData: image)
                    let rasterData = try await raster.finish()
                    try checkPage(rasterData, width: width, height: height, selectable: false)
                    try rasterData.write(to: dir.appendingPathComponent("native-exact.pdf"))
                    try await combined.append(imageData: image)
                    let complete = try await combined.finish()
                    try check(PDFDocument(data: complete)?.pageCount == 2, "PDF assembly lost a page")
                    try complete.write(to: dir.appendingPathComponent("native-combined.pdf"))
                    print("PASS native \(width)x\(height): quality text, exact image density, media boxes, two-page assembly")
                }
                app.terminate(nil)
            } catch { print("FAIL: \(error)"); exit(1) }
        }
        app.run()
    }

    private static func check(_ value: Bool, _ message: String) throws {
        if !value { throw NSError(domain: "sh.keynope.page-size-pdf", code: 1, userInfo: [NSLocalizedDescriptionKey: message]) }
    }

    private static func checkPage(_ data: Data, width: Int, height: Int, selectable: Bool) throws {
        guard let doc = PDFDocument(data: data), doc.pageCount == 1, let page = doc.page(at: 0) else { throw CocoaError(.fileReadCorruptFile) }
        let bounds = page.bounds(for: .mediaBox)
        try check(abs(bounds.width - Double(width)) < 1 && abs(bounds.height - Double(height)) < 1, "Wrong physical media box: \(bounds)")
        if selectable {
            let text = (page.string ?? "").replacingOccurrences(of: "\\s+", with: "", options: .regularExpression)
            try check(text.contains("Page\(width)x\(height)"), "Custom page lost selectable title")
            try check(!text.contains("PRIVATE"), "Private data in PDF")
        }
    }
}
