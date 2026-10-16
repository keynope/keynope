import Foundation

// The bulk write happens on an actor, not AppKit's executor. The final commit
// is one same-volume replacement after the caller has checked cancellation.
private actor SlideExportStager {
    func write(_ data: Data, to url: URL, progress: @Sendable (Int, Int) async throws -> Void) async throws {
        try Task.checkCancellation()
        guard FileManager.default.createFile(atPath: url.path, contents: nil) else {
            throw CocoaError(.fileWriteUnknown)
        }
        let file = try FileHandle(forWritingTo: url)
        defer { try? file.close() }
        let chunk = 1 << 20
        var offset = 0
        while offset < data.count {
            try Task.checkCancellation()
            let end = min(data.count, offset + chunk)
            try file.write(contentsOf: data[offset..<end])
            offset = end
            try await progress(offset, data.count)
        }
        try file.synchronize()
        try Task.checkCancellation()
    }
}

@MainActor
final class SlideExportWriter {
    private enum State { case prepared, writing, staged, failed, committed, discarded }
    private struct Fingerprint: Equatable {
        let device, inode, size: UInt64
        let modified: Date
    }
    let destination: URL
    let stagingDirectory: URL
    private let stagingURL: URL
    private let original: Fingerprint?
    private let securityScope: Bool
    private let stager = SlideExportStager()
    private var state = State.prepared

    init(destination: URL) throws {
        let url = destination.standardizedFileURL
        let scope = url.startAccessingSecurityScopedResource()
        do {
            let fingerprint = try Self.fingerprint(url)
            // Foundation picks an owned replacement directory on the selected
            // file's volume, including sandboxed/user-selected destinations.
            let directory = try FileManager.default.url(
                for: .itemReplacementDirectory, in: .userDomainMask,
                appropriateFor: fingerprint == nil ? url.deletingLastPathComponent() : url, create: true)
            self.destination = url
            self.stagingDirectory = directory
            self.stagingURL = directory.appendingPathComponent("Presentation-export")
            self.original = fingerprint
            self.securityScope = scope
        } catch {
            if scope { url.stopAccessingSecurityScopedResource() }
            throw error
        }
    }

    deinit {
        try? FileManager.default.removeItem(at: stagingDirectory)
        if securityScope { destination.stopAccessingSecurityScopedResource() }
    }

    func stage(_ data: Data, progress: @escaping @Sendable (Int, Int) async throws -> Void = { _, _ in }) async throws {
        guard state == .prepared else { throw CocoaError(.fileWriteUnknown) }
        state = .writing
        do {
            try await stager.write(data, to: stagingURL, progress: progress)
            try Task.checkCancellation()
            state = .staged
        } catch {
            state = .failed
            throw error
        }
    }

    func commit() throws {
        try Task.checkCancellation()
        guard state == .staged else { throw CocoaError(.fileWriteUnknown) }
        guard try Self.fingerprint(destination) == original else {
            throw NSError(domain: "sh.keynope.export", code: 10, userInfo: [
                NSLocalizedDescriptionKey: "The destination changed while the presentation was exporting. Choose a new file or export again."
            ])
        }
        if original != nil {
            _ = try FileManager.default.replaceItemAt(destination, withItemAt: stagingURL)
        } else {
            try FileManager.default.moveItem(at: stagingURL, to: destination)
        }
        state = .committed
        try? FileManager.default.removeItem(at: stagingDirectory)
    }

    func discard() {
        // The caller waits for cooperative staging cancellation before cleanup;
        // never remove a file underneath the actor's in-flight FileHandle.
        guard state != .writing else { return }
        if state != .committed { state = .discarded }
        try? FileManager.default.removeItem(at: stagingDirectory)
    }

    private static func fingerprint(_ url: URL) throws -> Fingerprint? {
        let attributes: [FileAttributeKey: Any]
        do { attributes = try FileManager.default.attributesOfItem(atPath: url.path) }
        catch let error as CocoaError where error.code == .fileReadNoSuchFile { return nil }
        guard attributes[.type] as? FileAttributeType == .typeRegular,
              let device = attributes[.systemNumber] as? NSNumber,
              let inode = attributes[.systemFileNumber] as? NSNumber,
              let size = attributes[.size] as? NSNumber,
              let modified = attributes[.modificationDate] as? Date else {
            throw CocoaError(.fileWriteUnsupportedScheme)
        }
        return Fingerprint(device: device.uint64Value, inode: inode.uint64Value, size: size.uint64Value, modified: modified)
    }
}
