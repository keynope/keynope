import Cocoa
import PDFKit

@main struct QualityPDFCheck {
    @MainActor static func main() {
        let application = NSApplication.shared
        application.setActivationPolicy(.accessory)
        Task { @MainActor in
            let renderer = SlideExportRenderer()
            let fallback = SlideExportRenderer()
            do {
                let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
                try await renderer.load(
                    String(contentsOf: root.appendingPathComponent("quality-proof.html"), encoding: .utf8))
                try await fallback.load(
                    String(contentsOf: root.appendingPathComponent("raster-proof.html"), encoding: .utf8))
                let pages = try await renderer.pages()
                try check(pages.count == 4, "Tabs must not become export pages")
                let quality = SlidePDFEncoder(title: "Quality proof")
                let raster = SlidePDFEncoder(title: "Raster proof")
                let expected = ["Modern quality proof", "C64 FONT PROOF", "Selectable cell"]
                for (position, info) in pages.enumerated() {
                    let page = info["index"] as! Int
                    try renderer.setPageSize(width: 1920, height: 1080)
                    if info["quality"] as? Bool == true {
                        let data = try await renderer.pdfData(page: page)
                        try data.write(to: root.appendingPathComponent("quality-page-\(position).pdf"))
                        guard let pdf = PDFDocument(data: data), pdf.pageCount == 1, let captured = pdf.page(at: 0)
                        else { throw CocoaError(.fileReadCorruptFile) }
                        let text = captured.string ?? ""
                        try check(
                            text.replacingOccurrences(of: "\\s+", with: "", options: .regularExpression).contains(
                                expected[position].replacingOccurrences(of: " ", with: "")),
                            "Page \(position) lost selectable text: \(text)")
                        try check(!text.contains("View"), "Accessibility controls leaked into PDF")
                        try await quality.append(pdfData: data, expectedAspectRatio: 16.0 / 9.0)
                    }
                    let image = try await fallback.imageData(page: page)
                    if info["quality"] as? Bool != true {
                        guard let bitmap = NSBitmapImageRep(data: image) else { throw CocoaError(.fileReadCorruptFile) }
                        var colored = 0
                        for y in stride(from: 0, to: bitmap.pixelsHigh, by: 8) {
                            for x in stride(from: 0, to: bitmap.pixelsWide, by: 8) {
                                if let color = bitmap.colorAt(x: x, y: y)?.usingColorSpace(.deviceRGB),
                                    abs(color.redComponent - color.greenComponent) > 0.1,
                                    max(color.redComponent, color.greenComponent) > 0.3, color.blueComponent > 0.2
                                {
                                    colored += 1
                                }
                            }
                        }
                        try check(colored > 20, "Retro image is missing from exact fallback")
                        try await quality.append(imageData: image)
                    }
                    try await raster.append(imageData: image)
                    print(
                        "Page \(position + 1): \(info["quality"] as? Bool == true ? "selectable quality PDF" : "exact raster fallback") and raster comparison"
                    )
                }
                try await quality.finish().write(to: root.appendingPathComponent("quality-proof.pdf"))
                try await raster.finish().write(to: root.appendingPathComponent("raster-proof.pdf"))
                _ = try await renderer.view.evaluateJavaScript(
                    "const info=keynopePDFManifest.pages[0];info.width=1600;info.height=1200;const s=info.page.scene;s.width=1600;s.height=1200;for(const o of s.objects){o.bounds.x*=1600/1920;o.bounds.width*=1600/1920;o.bounds.y*=1200/1080;o.bounds.height*=1200/1080;}"
                )
                try renderer.setPageSize(width: 1600, height: 1200)
                let customData = try await renderer.pdfData(page: 0)
                let custom = SlidePDFEncoder(title: "Custom aspect proof")
                try await custom.append(pdfData: customData, expectedAspectRatio: 4.0 / 3.0)
                try await custom.finish().write(to: root.appendingPathComponent("custom-aspect-proof.pdf"))
                let cancelled = SlideExportRenderer()
                cancelled.close()
                do {
                    _ = try await cancelled.pdfData(page: 0)
                    throw CocoaError(.fileWriteUnknown)
                } catch is CancellationError {}
                let stopped = await Task {
                    withUnsafeCurrentTask { $0?.cancel() }
                    do {
                        _ = try await quality.finish()
                        return false
                    } catch is CancellationError { return true } catch { return false }
                }.value
                try check(stopped, "Cancelled encoding returned PDF bytes")
                let stalled = SlideExportRenderer()
                try await stalled.load(
                    "<html><script>window.keynopePrepareSnapshot=()=>new Promise(()=>{});</script></html>")
                let pending = Task { @MainActor in
                    do {
                        _ = try await stalled.pdfData(page: 0)
                        return false
                    } catch is CancellationError { return true } catch { return false }
                }
                try await Task.sleep(for: .milliseconds(50))
                stalled.close()
                try check(await pending.value, "Closing a stalled capture did not cancel it")
                renderer.close()
                fallback.close()
                print("PASS: native quality PDF backend proof")
                exit(0)
            } catch {
                renderer.close()
                fallback.close()
                print("FAIL: \(error)")
                exit(1)
            }
        }
        application.run()
    }
    private static func check(_ condition: Bool, _ message: String) throws {
        if !condition {
            throw NSError(domain: "sh.keynope.pdf-test", code: 1, userInfo: [NSLocalizedDescriptionKey: message])
        }
    }
}
