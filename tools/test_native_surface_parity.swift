import Cocoa
import WebKit

// Run the public synthetic corpus through the production OS WebView renderer.
// This is an isolated accessory process, not the user's Keynope editor.
@main struct NativeSurfaceParity {
    struct Failure: Error, CustomStringConvertible { let description: String }

    @MainActor static func main() {
        let app = NSApplication.shared
        app.setActivationPolicy(.accessory)
        let frontmost = NSWorkspace.shared.frontmostApplication?.processIdentifier
        let clipboard = NSPasteboard.general.changeCount
        Task { @MainActor in
            var records: [[String: Any]] = []
            var output: URL?
            do {
                guard CommandLine.arguments.count == 2 else { throw Failure(description: "Usage: native-surface-parity <generated corpus directory>") }
                let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
                let destination = root.appendingPathComponent("native", isDirectory: true)
                guard !FileManager.default.fileExists(atPath: destination.path) else { throw Failure(description: "Refusing to replace existing native evidence") }
                try FileManager.default.createDirectory(at: destination, withIntermediateDirectories: false)
                output = destination
                let source = try Data(contentsOf: root.appendingPathComponent("cases.json"))
                guard let cases = try JSONSerialization.jsonObject(with: source) as? [[String: Any]], cases.count == 21 else { throw Failure(description: "Expected seven fixtures at three authored page sizes") }
                let filter = ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_SURFACE_FILTER"]
                let selected = cases.filter { filter == nil || ($0["file"] as? String) == filter }
                guard !selected.isEmpty else { throw Failure(description: "Native fixture filter matched nothing") }
                let expectedPages = selected.reduce(0) { $0 + (($1["scenes"] as? [Any])?.count ?? 0) }
                let rules = try await networkRules()
                var pageCount = 0
                for item in selected {
                    guard let file = item["file"] as? String, file == URL(fileURLWithPath: file).lastPathComponent,
                          let scenes = item["scenes"] as? [[String: Any]], !scenes.isEmpty,
                          let width = scenes[0]["width"] as? Double, let height = scenes[0]["height"] as? Double,
                          width > 0, height > 0, width <= 2560, height <= 1920 else { throw Failure(description: "Invalid public fixture manifest") }
                    let input = root.appendingPathComponent(file)
                    let original = try Data(contentsOf: input)
                    guard let html = String(data: original, encoding: .utf8) else { throw Failure(description: "Non-UTF8 fixture") }
                    let renderer = SlideExportRenderer()
                    renderer.view.configuration.userContentController.add(rules)
                    do {
                        try renderer.setPageSize(width: width, height: height)
                        try await renderer.load(html)
                        let pages = try await renderer.pages()
                        guard pages.count == scenes.count else { throw Failure(description: "Wrong native page count: \(file)") }
                        for (index, scene) in scenes.enumerated() {
                            guard scene["width"] as? Double == width, scene["height"] as? Double == height else { throw Failure(description: "Mixed physical dimensions: \(file)") }
                            let name = "\(file.dropLast(5))-\(index + 1)"
                            let actual = try await renderer.imageData(page: index)
                            let mounted = try await renderer.view.callAsyncJavaScript("""
                                const old=modernPageView.element, styles=old.style.cssText;
                                window.publicProofOriginal={old,display:old.style.display};
                                old.style.display='none';
                                window.publicProofView=KeynopeScene.mount(old.parentElement,scene);
                                publicProofView.element.style.cssText=styles;
                                publicProofView.element.id='native-public-proof';
                                await publicProofView.snapshotAt(0); await document.fonts.ready;
                                const rect=publicProofView.element.getBoundingClientRect();
                                if(Math.abs(rect.width-scene.width)>.01||Math.abs(rect.height-scene.height)>.01)throw Error('Native scene frame mismatch');
                                if(performance.getEntriesByType('resource').some(r=>/^https?:/.test(r.name)))throw Error('External resource in public proof');
                                return true;
                                """, arguments: ["scene": scene], in: nil, contentWorld: .page)
                            guard mounted as? Bool == true else { throw Failure(description: "Cold mount did not complete") }
                            let reference = try await snapshot(renderer.view, width: width, height: height)
                            _ = try await renderer.view.evaluateJavaScript("publicProofView.element.style.opacity='0'")
                            let erased = try await snapshot(renderer.view, width: width, height: height)
                            let comparison = try compare(actual, reference, erased, width: Int(width), height: Int(height))
                            try png(actual).write(to: destination.appendingPathComponent(name + ".png"))
                            records.append(["file": file, "page": index + 1, "width": width, "height": height,
                                            "objects": (scene["objects"] as? [Any])?.count ?? 0,
                                            "comparison": comparison])
                            try report(destination, passed: false, records: records)
                            guard comparison["changed"] == 0, (comparison["erasedDifference"] ?? 0) > 0 else {
                                try png(reference).write(to: destination.appendingPathComponent(name + "-reference.png"))
                                try png(erased).write(to: destination.appendingPathComponent(name + "-erased.png"))
                                let diagnostic = try await renderer.view.callAsyncJavaScript("""
                                    publicProofView.element.style.opacity='1';
                                    const info=n=>{const s=getComputedStyle(n),r=n.getBoundingClientRect();return {tag:n.tagName,id:n.id,cls:n.className?.baseVal??n.className,rect:{x:r.x,y:r.y,width:r.width,height:r.height},display:s.display,visibility:s.visibility,opacity:s.opacity,filter:s.filter,transform:s.transform,html:n.outerHTML.slice(0,4000)}};
                                    return {index:pageIndex,scenes:[...document.querySelectorAll('.keynope-modern-scene')].map(info),media:[...publicProofView.element.querySelectorAll('svg,image,img')].map(info),resources:performance.getEntriesByType('resource').map(r=>r.name)};
                                    """, arguments: [:], in: nil, contentWorld: .page)
                                if let diagnostic { try JSONSerialization.data(withJSONObject: diagnostic, options: [.prettyPrinted, .sortedKeys]).write(to: destination.appendingPathComponent(name + "-diagnostic.json")) }
                                throw Failure(description: "Native standalone/cold scene pixels differ: \(name): \(comparison)")
                            }
                            _ = try await renderer.view.evaluateJavaScript("publicProofView.dispose();publicProofOriginal.old.style.display=publicProofOriginal.display")
                            pageCount += 1
                            print("PASS \(name): native pixels match; erased control rejects")
                        }
                        guard try Data(contentsOf: input) == original else { throw Failure(description: "Native check modified fixture: \(file)") }
                        renderer.close()
                    } catch {
                        renderer.close()
                        throw error
                    }
                }
                guard pageCount == expectedPages, filter != nil || pageCount == 60, try Data(contentsOf: root.appendingPathComponent("cases.json")) == source else { throw Failure(description: "Incomplete or changed native corpus") }
                guard !app.isActive, NSWorkspace.shared.frontmostApplication?.processIdentifier == frontmost,
                      NSPasteboard.general.changeCount == clipboard else { throw Failure(description: "Native test changed active app or clipboard") }
                try report(destination, passed: true, records: records)
                print("PASS: \(pageCount) native OS WebView pages (filter=\(filter ?? "none")), exact cold-scene parity and erased controls; no app activation")
                app.terminate(nil)
            } catch {
                if let output { try? report(output, passed: false, records: records, error: String(describing: error)) }
                print("FAIL: \(error)")
                exit(1)
            }
        }
        app.run()
    }

    @MainActor private static func networkRules() async throws -> WKContentRuleList {
        try await withCheckedThrowingContinuation { continuation in
            WKContentRuleListStore.default().compileContentRuleList(forIdentifier: "KeynopeNativeSurfaceOffline", encodedContentRuleList: """
                [{"trigger":{"url-filter":"^https?://"},"action":{"type":"block"}}]
                """
            ) { rules, error in
                if let error { continuation.resume(throwing: error) }
                else if let rules { continuation.resume(returning: rules) }
                else { continuation.resume(throwing: Failure(description: "No offline content rules")) }
            }
        }
    }

    @MainActor private static func snapshot(_ view: WKWebView, width: Double, height: Double) async throws -> Data {
        let config = WKSnapshotConfiguration()
        config.rect = CGRect(x: 0, y: 0, width: width, height: height)
        config.snapshotWidth = NSNumber(value: width)
        return try await withCheckedThrowingContinuation { continuation in
            view.takeSnapshot(with: config) { image, error in
                if let error { continuation.resume(throwing: error) }
                else if let data = image?.tiffRepresentation { continuation.resume(returning: data) }
                else { continuation.resume(throwing: Failure(description: "Empty native snapshot")) }
            }
        }
    }

    private static func png(_ data: Data) throws -> Data {
        guard let image = NSBitmapImageRep(data: data), let png = image.representation(using: .png, properties: [:]) else { throw Failure(description: "Invalid native bitmap") }
        return png
    }

    private static func compare(_ actual: Data, _ reference: Data, _ erased: Data, width: Int, height: Int) throws -> [String: Int] {
        guard let a = NSBitmapImageRep(data: actual), let b = NSBitmapImageRep(data: reference), let e = NSBitmapImageRep(data: erased),
              let ap = a.bitmapData, let bp = b.bitmapData, let ep = e.bitmapData else { throw Failure(description: "Missing native comparison pixels") }
        let scale = a.pixelsWide / width
        guard (1...2).contains(scale), a.pixelsWide == width * scale, a.pixelsHigh == height * scale,
              [a,b,e].allSatisfy({ $0.bitsPerSample == 8 && !$0.isPlanar && (3...4).contains($0.samplesPerPixel) && $0.pixelsWide == a.pixelsWide && $0.pixelsHigh == a.pixelsHigh && $0.samplesPerPixel == a.samplesPerPixel && $0.bitmapFormat == a.bitmapFormat && $0.colorSpace == a.colorSpace }) else { throw Failure(description: "Native comparison format/density mismatch") }
        var changed = 0, erasedDifference = 0, maximum = 0
        for y in 0..<a.pixelsHigh {
            for x in 0..<a.pixelsWide {
                let ai = y * a.bytesPerRow + x * a.samplesPerPixel
                let bi = y * b.bytesPerRow + x * b.samplesPerPixel
                let ei = y * e.bytesPerRow + x * e.samplesPerPixel
                var delta = 0, negative = false
                for channel in 0..<a.samplesPerPixel {
                    delta = max(delta, abs(Int(ap[ai+channel]) - Int(bp[bi+channel])))
                    negative = negative || bp[bi+channel] != ep[ei+channel]
                }
                if delta > 0 { changed += 1 }
                if negative { erasedDifference += 1 }
                maximum = max(maximum, delta)
            }
        }
        return ["backingScale": scale, "changed": changed, "maximumChannelDifference": maximum, "erasedDifference": erasedDifference]
    }

    private static func report(_ root: URL, passed: Bool, records: [[String: Any]], error: String? = nil) throws {
        var report: [String: Any] = ["passed": passed, "fullCorpus": passed && records.count == 60, "platform": ProcessInfo.processInfo.operatingSystemVersionString,
                                   "records": records, "pages": records.count]
        if let error { report["error"] = error }
        try JSONSerialization.data(withJSONObject: report, options: [.prettyPrinted, .sortedKeys]).write(to: root.appendingPathComponent("report.json"), options: .atomic)
    }
}
