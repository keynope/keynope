import Cocoa
import PDFKit

// Runs only production export views in a non-activating accessory process.
// No user app, panels, preferences, recent documents or cloud services.
@main struct TextAlignmentPDFCheck {
    @MainActor static func main() {
        let app = NSApplication.shared
        app.setActivationPolicy(.accessory)
        Task { @MainActor in
            do {
                let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
                let sizes = CommandLine.arguments.count > 2 ? Array(CommandLine.arguments.dropFirst(2)) : ["1920x1080", "1080x1920", "2560x1080"]
                for size in sizes {
                    try await capture(root.appendingPathComponent(size, isDirectory: true))
                }
                app.terminate(nil)
            } catch { print("FAIL: \(error)"); exit(1) }
        }
        app.run()
    }

    @MainActor private static func capture(_ root: URL) async throws {
        let renderer = SlideExportRenderer(), exact = SlideExportRenderer()
        defer { renderer.close(); exact.close() }
        try await renderer.load(String(contentsOf: root.appendingPathComponent("quality.html"), encoding: .utf8))
        try await exact.load(String(contentsOf: root.appendingPathComponent("raster.html"), encoding: .utf8))
        let pages = try await renderer.pages()
        try check(pages.count == 15, "Missing alignment pages")
        let encoder = SlidePDFEncoder(title: "Text and shape-label alignment")
        var records: [[String: Any]] = []
        for (index, info) in pages.enumerated() {
            guard let width = info["width"] as? Double, let height = info["height"] as? Double,
                  info["quality"] as? Bool == true else { throw CocoaError(.fileReadCorruptFile) }
            try renderer.setPageSize(width: width, height: height)
            try exact.setPageSize(width: width, height: height)
            let pdf = try await renderer.pdfData(page: index)
            guard let page = PDFDocument(data: pdf)?.page(at: 0) else { throw CocoaError(.fileReadCorruptFile) }
            let bounds = page.bounds(for: .mediaBox), text = page.string ?? ""
            try check(abs(bounds.width-width) < 0.1 && abs(bounds.height-height) < 0.1, "PDF changed physical dimensions")
            let compact = text.replacingOccurrences(of: "\\s+", with: "", options: .regularExpression)
            // PDFKit may interleave adjacent wrapped owners in reading order.
            // Count every token here; the independent Poppler check below
            // assigns them to physical object frames, not string order.
            for token in ["Alignment", "sample", "Second", "line"] {
                try check(compact.components(separatedBy: token).count == 13, "PDF lost selectable \(token) on \(root.lastPathComponent) page \(index+1): \(text)")
            }
            try check(!text.contains("PRIVATE"), "Private metadata leaked into PDF")
            let image = try await exact.imageData(page: index)
            guard let bitmap = NSBitmapImageRep(data: image), let png = bitmap.representation(using: .png, properties: [:]) else { throw CocoaError(.fileReadCorruptFile) }
            try png.write(to: root.appendingPathComponent("native-exact-\(index+1).png"))
            try await encoder.append(pdfData: pdf, expectedAspectRatio: width/height)
            records.append(["page": index+1, "width": width, "height": height, "selectableText": text])
            print("Captured \(root.lastPathComponent) \(index+1)/15: twelve selectable aligned text/label boxes")
        }
        let complete = try await encoder.finish()
        guard let document = PDFDocument(data: complete), document.pageCount == 15 else { throw CocoaError(.fileReadCorruptFile) }
        for (index, record) in records.enumerated() {
            try check(document.page(at: index)?.string == record["selectableText"] as? String, "Assembly changed selectable content")
        }
        try complete.write(to: root.appendingPathComponent("native-alignment.pdf"))
        try JSONSerialization.data(withJSONObject: ["passed": true, "pages": records], options: .prettyPrinted).write(to: root.appendingPathComponent("native-report.json"))
        print("PASS \(root.lastPathComponent): 15 native quality PDF pages, 180 selectable text/label boxes and assembly")
    }

    private static func check(_ value: Bool, _ message: String) throws {
        if !value { throw NSError(domain: "sh.keynope.alignment-pdf-test", code: 1, userInfo: [NSLocalizedDescriptionKey: message]) }
    }
}
