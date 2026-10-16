// Independent parent readback after a real sandboxed NSSavePanel export.
// No UI, grants, bookmark creation or synthetic panel confirmation here.
import Foundation
import PDFKit

do {
    guard CommandLine.arguments.count == 3 else {
        throw NSError(domain: "KeynopeSandboxReadback", code: 1, userInfo: [NSLocalizedDescriptionKey: "Expected output and container-proof directories"])
    }
    let output = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
    let proof = URL(fileURLWithPath: CommandLine.arguments[2], isDirectory: true)
    var checks: [String] = []
    func require(_ ok: Bool, _ message: String) throws {
        guard ok else { throw NSError(domain: "KeynopeSandboxReadback", code: 2, userInfo: [NSLocalizedDescriptionKey: message]) }
        checks.append(message)
    }
    for name in ["native-quality", "native-exact", "user-selected"] {
        let url = output.appendingPathComponent(name + ".pdf")
        let data = try Data(contentsOf: url)
        let expected = try Data(contentsOf: proof.appendingPathComponent("expected-" + name + ".pdf"))
        try require(!data.isEmpty && data == expected, name + ": actual external file matches committed bytes")
        guard let pdf = PDFDocument(data: data) else { throw CocoaError(.fileReadCorruptFile) }
        try require(pdf.pageCount == 2, name + ": two public pages, private tab excluded")
        try require((0..<pdf.pageCount).allSatisfy { index in
            guard let page = pdf.page(at: index) else { return false }
            let bounds = page.bounds(for: .mediaBox)
            return abs(bounds.width / bounds.height - 16.0 / 9.0) < 0.005
        }, name + ": landscape dimensions preserved")
        let text = (0..<pdf.pageCount).compactMap { pdf.page(at: $0)?.string }.joined()
        let normalized = text.replacingOccurrences(of: "\\s+", with: "", options: .regularExpression)
        try require(name == "native-exact" ? normalized.isEmpty : normalized.contains("NativePresentationAcceptance"), name + ": selected PDF quality is respected")
    }
    checks.append("cancelled sandboxed staging leaves user-selected.pdf byte-exact")
    let report: [String: Any] = ["passed": true, "checks": checks]
    let json = try JSONSerialization.data(withJSONObject: report, options: [.prettyPrinted, .sortedKeys])
    FileHandle.standardOutput.write(json)
    FileHandle.standardOutput.write(Data("\n".utf8))
} catch {
    FileHandle.standardError.write(Data((error.localizedDescription + "\n").utf8))
    exit(1)
}
