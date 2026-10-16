import Cocoa
import PDFKit

// Production native renderer/encoder in an accessory process. No Keynope
// window, file picker, activation, recents or customer document is touched.
@main struct StructuredPDFCheck {
  @MainActor static func main() {
    let application = NSApplication.shared
    application.setActivationPolicy(.accessory)
    Task { @MainActor in
      let renderer = SlideExportRenderer()
      let fallback = SlideExportRenderer()
      do {
        let root = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
        let expected = try JSONDecoder().decode(
          [String].self, from: Data(contentsOf: root.appendingPathComponent("expected.json")))
        try await renderer.load(
          String(contentsOf: root.appendingPathComponent("quality.html"), encoding: .utf8))
        try await fallback.load(
          String(contentsOf: root.appendingPathComponent("raster.html"), encoding: .utf8))
        let pages = try await renderer.pages()
        guard pages.count == expected.count else { throw CocoaError(.fileReadCorruptFile) }
        let quality = SlidePDFEncoder(title: "Structured quality proof")
        let raster = SlidePDFEncoder(title: "Structured raster proof")
        var checks: [[String: Any]] = []
        var captures: [Data] = []
        for (position, info) in pages.enumerated() {
          guard info["quality"] as? Bool == true else { throw CocoaError(.fileReadCorruptFile) }
          let page = info["index"] as! Int
          try renderer.setPageSize(width: 1920, height: 1080)
          try fallback.setPageSize(width: 1920, height: 1080)
          let data = try await renderer.pdfData(page: page)
          if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_PDF_PAGES"] == "1" {
            try data.write(
              to: URL(fileURLWithPath: "/tmp/keynope-structured-page-\(position + 1).pdf"))
          }
          guard let doc = PDFDocument(data: data), doc.pageCount == 1,
            let captured = doc.page(at: 0)
          else { throw CocoaError(.fileReadCorruptFile) }
          let text = captured.string ?? ""
          guard text.contains(expected[position]), !text.contains("View chart data"),
            !text.contains("PRIVATE STRUCTURED")
          else {
            throw NSError(
              domain: "sh.keynope.structured-pdf", code: 1,
              userInfo: [
                NSLocalizedDescriptionKey: "Text/privacy failure on page \(position + 1): \(text)"
              ])
          }
          let required: [String]
          switch position {
          case 0: required = ["Merged perimeter proof", "Top neighbour", "Bottom neighbour"]
          case 1: required = ["Workshop", "Owner", "Complete", "Ready", "progress"]
          case 2: required = ["1:1", "20:20"]
          case 9: required = ["Clear neighbour", "Clean lower row"]
          default:
            required = [
              "Quarterly workshop outcomes", "Discovery", "Prototyping", "Validation", "Delivery",
            ]
          }
          let compact = text.replacingOccurrences(of: "\\s+", with: "", options: .regularExpression)
          for item in required {
            guard compact.contains(item.replacingOccurrences(of: " ", with: "")) else {
              throw NSError(
                domain: "sh.keynope.structured-pdf", code: 2,
                userInfo: [
                  NSLocalizedDescriptionKey:
                    "Missing structured text on page \(position + 1): \(item)"
                ])
            }
          }
          let labelValues =
            try await renderer.view.callAsyncJavaScript(
              """
              const s=keynopePDFManifest.pages[index].page.scene;
              const o=s.objects.find(o=>o.chart); if(!o)return [];
              const a=(o.rotation||0)*Math.PI/180,c=Math.cos(a),sn=Math.sin(a);
              const cx=o.bounds.width/2,cy=o.bounds.height/2;
              return o.chart.labels.filter(l=>l.fullText===o.chart.data.title||['Team A','Team B','Discovery','Prototyping','Validation','Delivery'].includes(l.fullText)).map(l=>{
                const b=l.bounds;
                const points=[[b.x,b.y],[b.x+b.width,b.y],[b.x,b.y+b.height],[b.x+b.width,b.y+b.height]].map(([x,y])=>[o.bounds.x+cx+(x-cx)*c-(y-cy)*sn,o.bounds.y+cy+(x-cx)*sn+(y-cy)*c]);
                const xs=points.map(p=>p[0]),ys=points.map(p=>p[1]);
                return {text:l.fullText,x:Math.min(...xs),y:Math.min(...ys),width:Math.max(...xs)-Math.min(...xs),height:Math.max(...ys)-Math.min(...ys)};
              });
              """,
              arguments: ["index": page], in: nil, contentWorld: .page) as? [[String: Any]] ?? []
          try verifyLabels(captured, labels: labelValues, page: position + 1)
          let image = try await fallback.imageData(page: page)
          guard let bitmap = NSBitmapImageRep(data: image),
            let png = bitmap.representation(using: .png, properties: [:])
          else { throw CocoaError(.fileReadCorruptFile) }
          try png.write(to: root.appendingPathComponent("native-raster-\(position + 1).png"))
          try await quality.append(pdfData: data, expectedAspectRatio: 16.0 / 9.0)
          try await raster.append(imageData: image)
          checks.append([
            "page": position + 1, "text": text,
            "bounds": [
              captured.bounds(for: .mediaBox).width, captured.bounds(for: .mediaBox).height,
            ], "labels": labelValues,
          ])
          captures.append(data)
        }
        let complete = try await quality.finish()
        guard let assembled = PDFDocument(data: complete), assembled.pageCount == expected.count
        else { throw CocoaError(.fileReadCorruptFile) }
        for i in checks.indices {
          guard let page = assembled.page(at: i),
            let source = PDFDocument(data: captures[i])?.page(at: 0), page.string == source.string
          else {
            throw NSError(
              domain: "sh.keynope.structured-pdf", code: 3,
              userInfo: [
                NSLocalizedDescriptionKey: "PDF assembly changed selectable content on page \(i+1)"
              ])
          }
          try verifyLabels(page, labels: checks[i]["labels"] as? [[String: Any]] ?? [], page: i + 1)
        }
        try complete.write(to: root.appendingPathComponent("native-quality.pdf"))
        try await raster.finish().write(to: root.appendingPathComponent("native-raster.pdf"))
        let report: [String: Any] = ["passed": true, "pages": checks]
        try JSONSerialization.data(withJSONObject: report, options: .prettyPrinted).write(
          to: root.appendingPathComponent("native-report.json"))
        // Deliberately hide an already-described caption AFTER snapshot
        // readiness. Native quality capture must reject it, allowing the
        // normal exact-appearance fallback instead of saving incomplete art.
        _ = try await renderer.view.evaluateJavaScript(
          "window.__originalStructuredSnapshot=keynopePrepareSnapshot;window.keynopePrepareSnapshot=async index=>{const result=await __originalStructuredSnapshot(index);if(window.__breakCaption){window.__breakCaption=false;const node=document.querySelector('.keynope-chart-label');node.style.visibility='hidden';}if(window.__moveCaption){window.__moveCaption=false;const node=document.querySelector('.keynope-chart-label');node.style.transform='translateY(300px)';}return result;};true"
        )
        for flag in ["__breakCaption", "__moveCaption"] {
          _ = try await renderer.view.evaluateJavaScript("window.\(flag)=true")
          do {
            _ = try await renderer.pdfData(page: 3)
            throw CocoaError(.fileWriteUnknown)
          } catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 10
          {}
          _ = try await renderer.imageData(page: 3)
        }
        print(
          "PASS: \(pages.count) structured landscape pages, selectable text/caption positions and exact raster captures."
        )
        renderer.close()
        fallback.close()
        application.terminate(nil)
      } catch {
        renderer.close()
        fallback.close()
        print("FAIL: \(error)")
        exit(1)
      }
    }
    application.run()
  }

  private static func verifyLabels(_ page: PDFPage, labels: [[String: Any]], page index: Int) throws
  {
    let text = page.string ?? ""
    let height = page.bounds(for: .mediaBox).height
    for label in labels {
      guard let value = label["text"] as? String, let range = text.range(of: value),
        let selected = page.selection(for: NSRange(range, in: text)),
        let x = label["x"] as? Double, let y = label["y"] as? Double,
        let w = label["width"] as? Double, let h = label["height"] as? Double
      else { throw CocoaError(.fileReadCorruptFile) }
      let actual = selected.bounds(for: page)
      let expected = CGRect(x: x - 2, y: height - y - h - 2, width: w + 4, height: h + 4)
      guard expected.contains(actual) else {
        throw NSError(
          domain: "sh.keynope.structured-pdf", code: 4,
          userInfo: [
            NSLocalizedDescriptionKey:
              "Chart label geometry changed on page \(index), \(value): \(actual), expected within \(expected)"
          ])
      }
    }
  }
}
