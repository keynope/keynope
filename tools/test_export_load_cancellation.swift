import Cocoa

// Run through test_export_load_cancellation.cjs. Its loopback image response
// deliberately stalls navigation; no external network or user app is involved.
@main struct ExportLoadCancellationCheck {
    @MainActor static func main() {
        NSApplication.shared.setActivationPolicy(.accessory)
        Task { @MainActor in
            do {
                guard CommandLine.arguments.count == 2,
                      let base = URL(string: CommandLine.arguments[1]) else {
                    throw failure("Missing owned loopback fixture")
                }
                try await checkLoading(base)
                print("PASS: navigation cancellation, busy-request ownership and stale-navigation rejection")
                NSApplication.shared.terminate(nil)
            } catch {
                print("FAIL: \(error)")
                exit(1)
            }
        }
        NSApplication.shared.run()
    }

    @MainActor private static func checkLoading(_ base: URL) async throws {
        let renderer = SlideExportRenderer()
        defer { renderer.close() }
        let html = "<html><img src='\(base.appendingPathComponent("stall.png").absoluteString)'></html>"
        var finished = false
        let pending = Task { @MainActor in
            defer { finished = true }
            do { try await renderer.load(html); return false }
            catch is CancellationError { return true }
            catch { return false }
        }
        var received = false
        for _ in 0..<200 {
            let (_, response) = try await URLSession.shared.data(from: base.appendingPathComponent("started"))
            if (response as? HTTPURLResponse)?.statusCode == 200 { received = true; break }
            try await Task.sleep(for: .milliseconds(10))
        }
        guard received, !finished else {
            renderer.close(); _ = await pending.value
            throw failure("Navigation did not stall on its owned image")
        }
        // Task cancellation alone must settle a loading continuation.
        pending.cancel()
        for _ in 0..<100 {
            if finished { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        guard finished else {
            renderer.close(); _ = await pending.value
            throw failure("Cancelled navigation waited for timeout")
        }
        guard await pending.value else { throw failure("Cancelled load did not throw CancellationError") }

        let busy = SlideExportRenderer()
        defer { busy.close() }
        var firstFinished = false
        let first = Task { @MainActor in
            defer { firstFinished = true }
            do { try await busy.load(html); return false }
            catch is CancellationError { return true }
            catch { return false }
        }
        // Main actor suspension gives the first load ownership before retries.
        try await Task.sleep(for: .milliseconds(100))
        do {
            try await busy.load("<html>Must not replace pending navigation</html>")
            throw failure("Concurrent load stole the first continuation")
        } catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 6 {}
        do {
            _ = try await busy.imageData(page: 0)
            throw failure("Capture was allowed during navigation")
        } catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 6 {}
        guard !firstFinished else { throw failure("Rejected request interrupted the pending load") }
        busy.close()
        guard await first.value else { throw failure("Close failed to settle original load") }

        let ready = SlideExportRenderer()
        defer { ready.close() }
        try await ready.load("<html><script>window.exportMarker='first';</script></html>")
        guard let oldNavigation = ready.view.loadHTMLString("<html>Unowned navigation</html>", baseURL: nil) else {
            throw failure("Missing stale-navigation identity")
        }
        // A new load owns a different WKNavigation identity. Simulate an old
        // delegate completion while that real navigation is still loading.
        var nextFinished = false
        let next = Task { @MainActor in
            defer { nextFinished = true }
            do { try await ready.load(html); return false }
            catch is CancellationError { return true }
            catch { return false }
        }
        try await Task.sleep(for: .milliseconds(100))
        ready.webView(ready.view, didFinish: oldNavigation)
        let staleFailure = NSError(domain: "sh.keynope.stale-navigation", code: 1)
        ready.webView(ready.view, didFail: oldNavigation, withError: staleFailure)
        ready.webView(ready.view, didFailProvisionalNavigation: oldNavigation, withError: staleFailure)
        try await Task.sleep(for: .milliseconds(100))
        guard !nextFinished else { throw failure("Stale navigation completed the current load") }
        next.cancel()
        guard await next.value else { throw failure("Stale-callback test did not cancel cleanly") }
    }

    private static func failure(_ message: String) -> NSError {
        NSError(domain: "sh.keynope.load-cancellation-test", code: 1,
                userInfo: [NSLocalizedDescriptionKey: message])
    }
}
