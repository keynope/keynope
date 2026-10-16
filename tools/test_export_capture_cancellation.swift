import Cocoa

// Isolated accessory-process regression: no working app activation, network,
// filesystem destination or generated PDF. Readiness deliberately never settles.
@main struct ExportCaptureCancellationCheck {
    @MainActor static func main() {
        let app = NSApplication.shared
        app.setActivationPolicy(.accessory)
        Task { @MainActor in
            do {
                for vector in [false, true] {
                    try await checkCancellation(vector: vector)
                }
                print("PASS: task cancellation interrupts raster/vector readiness and closes its isolated renderer")
                app.terminate(nil)
            } catch {
                print("FAIL: \(error)")
                exit(1)
            }
        }
        app.run()
    }

    @MainActor private static func checkCancellation(vector: Bool) async throws {
        let renderer = SlideExportRenderer()
        defer { renderer.close() }
        try await renderer.load("<html><script>window.captureEntered=false;window.keynopePrepareSnapshot=()=>{window.captureEntered=true;return new Promise(()=>{});};</script></html>")
        var finished = false
        let pending = Task { @MainActor in
            defer { finished = true }
            do {
                if vector { _ = try await renderer.pdfData(page: 0) }
                else { _ = try await renderer.imageData(page: 0) }
                return false
            } catch is CancellationError { return true }
            catch { return false }
        }
        for _ in 0..<100 {
            if try await renderer.view.evaluateJavaScript("window.captureEntered") as? Bool == true { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        guard try await renderer.view.evaluateJavaScript("window.captureEntered") as? Bool == true else {
            renderer.close()
            _ = await pending.value
            throw failure("Capture did not enter its readiness promise")
        }
        // Rejecting a simultaneous request must not complete or steal the
        // first request's continuation or cancellation ownership.
        do {
            _ = try await renderer.imageData(page: 0)
            throw failure("Concurrent capture was accepted")
        } catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 6 {}
        guard !finished else { throw failure("Concurrent request interrupted the original") }
        pending.cancel()
        for _ in 0..<100 {
            if finished { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        guard finished else {
            renderer.close()
            _ = await pending.value
            throw failure("Cancelled capture waited for timeout instead of completing within one second")
        }
        guard await pending.value else { throw failure("Cancelled capture did not throw CancellationError") }
        do {
            _ = try await renderer.imageData(page: 0)
            throw failure("Cancelled renderer was left reusable with a stalled page")
        } catch is CancellationError {}
        print("PASS: \(vector ? "vector" : "raster") readiness task cancellation")
    }

    private static func failure(_ message: String) -> NSError {
        NSError(domain: "sh.keynope.capture-cancellation-test", code: 1,
                userInfo: [NSLocalizedDescriptionKey: message])
    }
}
