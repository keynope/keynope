import Cocoa

// Small owned PDFs distinguish drawn images from unused resources and masks.
// CoreGraphics only: no application, browser, Save panel or external services.
@main struct PDFImagePresenceCheck {
    struct Failure: Error { let message: String }

    static func main() async {
        do {
            let image = SlidePDFImageCheck(width: 2, height: 2)
            for (name, content, form, count) in [
                ("direct", "/Im Do", "", 1),
                ("shared resource drawn twice", "/Im Do /Im Do", "", 2),
                ("nested form", "/Fm Do", "/Im Do", 1),
                ("nested form drawn twice", "/Fm Do /Fm Do", "/Im Do", 2)
            ] {
                try await SlidePDFImageValidator.shared.validate(pdf(content: content, form: form), checks: Array(repeating: image, count: count))
                print("PASS: \(name)")
            }
            let negative: [(String, Data, [SlidePDFImageCheck])] = [
                ("unused resource", pdf(content: ""), [image]),
                ("undrawn nested resource", pdf(content: "/Fm Do", form: ""), [image]),
                ("wrong dimensions", pdf(content: "/Im Do"), [.init(width: 3, height: 2)]),
                ("missing repeated picture", pdf(content: "/Im Do"), [image, image]),
                ("missing resource", pdf(content: "/Absent Do"), [image]),
                ("recursive form", pdf(content: "/Fm Do", form: "/Fm Do"), [image]),
                ("operator budget", pdf(content: String(repeating: "/Im Do ", count: 4097)), [image]),
                ("check budget", pdf(content: "/Im Do"), Array(repeating: image, count: 257)),
                ("invalid dimensions", pdf(content: "/Im Do"), [.init(width: 0, height: 2)]),
                ("invalid PDF", Data("not PDF".utf8), [image])
            ]
            for (name, data, checks) in negative {
                do {
                    try await SlidePDFImageValidator.shared.validate(data, checks: checks)
                    throw Failure(message: "Accepted invalid image evidence: \(name)")
                } catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 12 {
                    print("PASS: rejected \(name)")
                }
            }
            let cancelled = await Task {
                withUnsafeCurrentTask { $0?.cancel() }
                do {
                    try await SlidePDFImageValidator.shared.validate(pdf(content: "/Im Do"), checks: [image])
                    return false
                } catch is CancellationError { return true }
                catch { return false }
            }.value
            guard cancelled else { throw Failure(message: "Cancelled validation was accepted") }
            print("PASS: cancellation")
            if CommandLine.arguments.count == 2 {
                let root = URL(fileURLWithPath: CommandLine.arguments[1])
                let expected = SlidePDFImageCheck(width: 3840, height: 2160)
                let bad = try Data(contentsOf: root.appendingPathComponent("native-quality-page-17.pdf"))
                let good = try Data(contentsOf: root.appendingPathComponent("diagnostic-17-repeated.pdf"))
                do {
                    try await SlidePDFImageValidator.shared.validate(bad, checks: [expected])
                    throw Failure(message: "Accepted actual missing-background PDF")
                } catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 12 {
                    print("PASS: rejected original native missing-background artifact")
                }
                try await SlidePDFImageValidator.shared.validate(good, checks: [expected])
                print("PASS: accepted same-DOM repeat with actual drawn background")
            }
            print("PASS: sampled-image PDF validation")
        } catch { print("FAIL: \(error)"); exit(1) }
    }

    private static func pdf(content: String, form: String = "") -> Data {
        func stream(_ dictionary: String, _ content: String) -> String {
            "<< \(dictionary) /Length \(content.utf8.count) >>\nstream\n\(content)\nendstream"
        }
        let resources = "/Resources << /XObject << /Im 4 0 R /Fm 5 0 R >> >>"
        let objects = [
            "<< /Type /Catalog /Pages 2 0 R >>",
            "<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
            "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 20 20] \(resources) /Contents 6 0 R >>",
            stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /ASCIIHexDecode", "ff000000ff000000ffffffff>"),
            stream("/Type /XObject /Subtype /Form /BBox [0 0 20 20] \(resources)", form),
            stream("", content)
        ]
        var data = Data("%PDF-1.4\n".utf8), offsets = [0]
        for (index, object) in objects.enumerated() {
            offsets.append(data.count)
            data.append(Data("\(index+1) 0 obj\n\(object)\nendobj\n".utf8))
        }
        let xref = data.count
        data.append(Data("xref\n0 \(offsets.count)\n0000000000 65535 f \n".utf8))
        for offset in offsets.dropFirst() { data.append(Data(String(format: "%010d 00000 n \n", offset).utf8)) }
        data.append(Data("trailer\n<< /Size \(offsets.count) /Root 1 0 R >>\nstartxref\n\(xref)\n%%EOF\n".utf8))
        return data
    }
}
