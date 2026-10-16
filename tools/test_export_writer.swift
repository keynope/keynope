import Foundation

@main struct ExportWriterCheck {
    @MainActor static func main() async {
        let manager = FileManager.default
        let root = CommandLine.arguments.count>1
            ? URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
            : manager.urls(for:.applicationSupportDirectory,in:.userDomainMask)[0].appendingPathComponent("ExportWriterProof",isDirectory:true)
        do {
            try manager.createDirectory(at: root, withIntermediateDirectories: true)
            let destination = root.appendingPathComponent("transaction.pdf")
            let original = Data("ORIGINAL DESTINATION".utf8)
            try original.write(to: destination)
            let payload = Data(repeating: 0x5a, count: 32 << 20)

            let staged = try SlideExportWriter(destination: destination)
            print("Checking replacement")
            try await staged.stage(payload)
            try check(try Data(contentsOf: destination) == original, "Staging replaced the destination")
            try staged.commit()
            try check(try Data(contentsOf: destination) == payload, "Commit did not replace the destination")
            try check(!manager.fileExists(atPath: staged.stagingDirectory.path), "Commit left its temporary directory")

            try original.write(to: destination)
            let cancelled = try SlideExportWriter(destination: destination)
            print("Checking mid-write cancellation")
            do {
                try await cancelled.stage(payload) { completed, _ in
                    if completed >= 1 << 20 { throw CancellationError() }
                }
                throw WriterFailure(message: "Cancelled staging returned success")
            } catch is CancellationError {}
            cancelled.discard()
            try check(try Data(contentsOf: destination) == original, "Cancelled staging changed existing data")
            try check(!manager.fileExists(atPath: cancelled.stagingDirectory.path), "Cancelled staging left temporary data")

            let late = try SlideExportWriter(destination: destination)
            print("Checking post-stage cancellation")
            let lateResult = await Task { @MainActor in
                do {
                    try await late.stage(payload)
                    withUnsafeCurrentTask { $0?.cancel() }
                    try late.commit()
                    return false
                } catch is CancellationError { return true } catch { return false }
            }.value
            try check(lateResult, "Cancellation after staging did not prevent replacement")
            late.discard()
            try check(try Data(contentsOf: destination) == original, "Late cancellation changed existing data")

            let conflict = try SlideExportWriter(destination: destination)
            print("Checking concurrent change")
            try await conflict.stage(payload)
            let other = Data("ANOTHER WRITER'S CONTENT".utf8)
            try other.write(to: destination)
            do { try conflict.commit(); throw WriterFailure(message: "Concurrent change was overwritten") }
            catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 10 {}
            conflict.discard()
            try check(try Data(contentsOf: destination) == other, "Conflict handling changed the other writer's data")

            let fresh = root.appendingPathComponent("fresh.pdf")
            try? manager.removeItem(at: fresh)
            let create = try SlideExportWriter(destination: fresh)
            print("Checking create")
            try check(!manager.fileExists(atPath:fresh.path),"Preparing a new export created a destination placeholder")
            try await create.stage(Data("NEW".utf8))
            try create.commit()
            try check(try Data(contentsOf: fresh) == Data("NEW".utf8), "New destination creation failed")

            let aborted=root.appendingPathComponent("aborted.pdf")
            try? manager.removeItem(at:aborted)
            let abortedWriter=try SlideExportWriter(destination:aborted)
            try await abortedWriter.stage(payload)
            abortedWriter.discard()
            try check(!manager.fileExists(atPath:aborted.path),"Discarding an uncommitted new export left output")

            let raced = root.appendingPathComponent("raced.pdf")
            try? manager.removeItem(at: raced)
            let noClobber = try SlideExportWriter(destination: raced)
            print("Checking creation race")
            try await noClobber.stage(payload)
            try other.write(to: raced)
            do { try noClobber.commit(); throw WriterFailure(message: "Newly created destination was clobbered") }
            catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 10 {}
            noClobber.discard()
            try check(try Data(contentsOf: raced) == other, "New-file race changed the destination")
            print("PASS: off-main staging, replace/create, early and late cancellation, cleanup and concurrent-change protection")
            if CommandLine.arguments.count > 2 {
                try await checkRequestCancellation(URL(string: CommandLine.arguments[2])!)
            }
        } catch { print("FAIL: \(error)"); exit(1) }
    }
    private struct WriterFailure: Error { let message: String }
    private static func checkRequestCancellation(_ base: URL) async throws {
        let request = Task { try await URLSession.shared.data(from: base.appendingPathComponent("export")) }
        defer { request.cancel() }
        func waitFor(_ path: String) async throws {
            for _ in 0..<200 {
                let (_, response) = try await URLSession.shared.data(from: base.appendingPathComponent(path))
                if (response as? HTTPURLResponse)?.statusCode == 200 { return }
                try await Task.sleep(for: .milliseconds(20))
            }
            throw WriterFailure(message: "Timed out waiting for request \(path)")
        }
        try await waitFor("started")
        request.cancel()
        do {
            _ = try await request.value
            throw WriterFailure(message: "Cancelled URLSession request returned success")
        } catch let error as URLError where error.code == .cancelled {}
        try await waitFor("cancelled")
        print("PASS: Swift task cancellation reaches the HTTP server request context")
    }
    private static func check(_ value: Bool, _ message: String) throws {
        if !value { throw WriterFailure(message: message) }
    }
}
