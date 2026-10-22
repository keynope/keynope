import Cocoa
import WebKit
import PDFKit

// App-level automation is independent of the editor engine and its current
// document. In particular, do not forward its file bookmark to this helper.
@MainActor
final class AutomationService {
    static let enabledKey = "keynope.mcp.enabled"
    private var process: Process?
    private var input: Pipe?
    private var managementURL: URL?
    private var managementToken = ""
    private var operation: Task<Void, Error>?
    private let workspaceDirectory: URL?
    private let preferences: UserDefaults
    private(set) var settings: [String: Any] = ["enabled": false]
    var enabled: Bool { process?.isRunning == true && settings["enabled"] as? Bool == true }

    init(workspaceDirectory: URL? = nil, preferences: UserDefaults = .standard) {
        self.workspaceDirectory = workspaceDirectory
        self.preferences = preferences
    }

    private func start() async throws {
        if process?.isRunning == true { return }
        stop()
        let directory = try workspaceDirectory ?? FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
            .appendingPathComponent("Keynope/Automation", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let child = Process(), output = Pipe(), parentInput = Pipe()
        child.executableURL = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/keynope-engine")
        child.arguments = ["--mcp-http", "--workspace", directory.path]
        var environment = ProcessInfo.processInfo.environment
        environment.removeValue(forKey: "KEYNOPE_SANDBOX_BOOKMARK")
        child.environment = environment
        child.standardInput = parentInput
        child.standardOutput = output
        child.standardError = FileHandle.standardError
        try child.run()
        process = child
        input = parentInput
        do {
            let handle = output.fileHandleForReading
            let data = try await withThrowingTaskGroup(of: Data.self) { group in
                group.addTask {
                    var bytes = Data()
                    for try await byte in handle.bytes {
                        if byte == 10 { return bytes }
                        guard bytes.count < 4096 else { throw CocoaError(.fileReadTooLarge) }
                        bytes.append(byte)
                    }
                    throw CocoaError(.fileReadCorruptFile)
                }
                group.addTask {
                    try await Task.sleep(nanoseconds: 10_000_000_000)
                    // Unblock the pipe reader too: task cancellation alone
                    // cannot interrupt a stalled native helper's output pipe.
                    if child.isRunning { child.terminate() }
                    throw CocoaError(.fileReadUnknown)
                }
                defer { group.cancelAll() }
                return try await group.next()!
            }
            guard let result = try JSONSerialization.jsonObject(with: data) as? [String: String],
                  let address = result["url"], let url = URL(string: address),
                  url.host == "127.0.0.1", let token = result["token"], !token.isEmpty else { throw CocoaError(.fileReadCorruptFile) }
            managementURL = url.appendingPathComponent("api/editor/mcp")
            managementToken = token
        } catch {
            stop()
            throw error
        }
    }

    func setEnabled(_ value: Bool) async throws {
        // Serialize startup restoration and a fast subsequent user toggle.
        let previous = operation
        let task = Task { @MainActor in
            if let previous { _ = try? await previous.value }
            try Task.checkCancellation()
            if value { try await self.start() }
            try Task.checkCancellation()
            if self.managementURL != nil { try await self.request(enabled: value) }
            else { self.settings = ["enabled": false] }
            self.preferences.set(value, forKey: Self.enabledKey)
        }
        operation = task
        try await task.value
    }

    func refresh() async throws {
        if let operation { _ = try? await operation.value }
        if process?.isRunning == true { try await request() }
        else { settings = ["enabled": false] }
    }

    private func request(enabled: Bool? = nil) async throws {
        guard let managementURL else { throw CocoaError(.fileReadUnknown) }
        var request = URLRequest(url: managementURL)
        request.timeoutInterval = 15
        request.setValue("keynope_session=" + managementToken, forHTTPHeaderField: "Cookie")
        if let enabled {
            request.httpMethod = "POST"
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: ["enabled": enabled])
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.httpCookieStorage = nil
        configuration.urlCache = nil
        let session = URLSession(configuration: configuration)
        defer { session.finishTasksAndInvalidate() }
        let (data, response) = try await session.data(for: request)
        guard (response as? HTTPURLResponse)?.statusCode == 200 else {
            throw NSError(domain: "sh.keynope.mcp", code: 1, userInfo: [NSLocalizedDescriptionKey: String(data: data, encoding: .utf8) ?? "Could not change automation settings."])
        }
        guard let value = try JSONSerialization.jsonObject(with: data) as? [String: Any] else { throw CocoaError(.fileReadCorruptFile) }
        settings = value
    }

    enum CopyFormat: Int { case codex, claude, configuration }
    func copyText(_ format: CopyFormat) throws -> String {
        guard enabled, let configuration = settings["configuration"] as? [String: Any],
              let servers = configuration["mcpServers"] as? [String: Any],
              let server = servers["keynope"] as? [String: Any], let url = server["url"] as? String else {
            throw NSError(domain: "sh.keynope.mcp", code: 2, userInfo: [NSLocalizedDescriptionKey: "Enable MCP access before copying connection details."])
        }
        func quote(_ value: String) -> String { "'" + value.replacingOccurrences(of: "'", with: "'\"'\"'") + "'" }
        switch format {
        case .codex:
            return "codex mcp add keynope --url " + quote(url)
        case .claude:
            return "claude mcp add --transport http keynope " + quote(url)
        case .configuration:
            return String(decoding: try JSONSerialization.data(withJSONObject: configuration, options: [.prettyPrinted, .sortedKeys]), as: UTF8.self)
        }
    }

    func stop() {
        try? input?.fileHandleForWriting.close()
        if process?.isRunning == true { process?.terminate() }
        input = nil
        process = nil
        managementURL = nil
        managementToken = ""
        settings = ["enabled": false]
    }
}

@MainActor
final class AutomationSettingsController: NSWindowController {
    private let service: AutomationService
    private let toggle = NSButton(checkboxWithTitle: "Enable MCP access", target: nil, action: nil)
    private let status = NSTextField(wrappingLabelWithString: "")
    private let workspace = NSTextField(wrappingLabelWithString: "")
    private var copyButtons: [NSButton] = []
    private var busy = false

    init(service: AutomationService) {
        self.service = service
        let panel = NSPanel(contentRect: NSRect(x: 0, y: 0, width: 560, height: 440), styleMask: [.titled, .closable], backing: .buffered, defer: false)
        panel.title = "Automation (MCP)"
        panel.isReleasedWhenClosed = false
        super.init(window: panel)
        let title = NSTextField(labelWithString: "Deck Automation")
        title.font = .systemFont(ofSize: 17, weight: .semibold)
        let description = NSTextField(wrappingLabelWithString: "Let trusted AI clients build decks in a separate automation workspace. Enabling MCP does not share the presentation you have open or grant access to its folder.")
        description.textColor = .secondaryLabelColor
        toggle.target = self
        toggle.action = #selector(changeEnabled)
        let stack = NSStackView(views: [title, description, toggle])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 14
        for (index, name) in ["Copy Codex Command", "Copy Claude Command", "Copy MCP Configuration"].enumerated() {
            let button = NSButton(title: name, target: self, action: #selector(copyConnection(_:)))
            button.tag = index
            copyButtons.append(button)
            stack.addArrangedSubview(button)
        }
        status.font = .systemFont(ofSize: 12)
        status.textColor = .secondaryLabelColor
        workspace.font = .systemFont(ofSize: 11)
        workspace.textColor = .secondaryLabelColor
        workspace.isSelectable = true
        stack.addArrangedSubview(workspace)
        stack.addArrangedSubview(status)
        guard let content = panel.contentView else { return }
        content.addSubview(stack)
        stack.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            stack.leadingAnchor.constraint(equalTo: content.leadingAnchor, constant: 24),
            stack.trailingAnchor.constraint(equalTo: content.trailingAnchor, constant: -24),
            stack.topAnchor.constraint(equalTo: content.topAnchor, constant: 24),
            stack.bottomAnchor.constraint(lessThanOrEqualTo: content.bottomAnchor, constant: -24),
            description.widthAnchor.constraint(equalTo: stack.widthAnchor),
            workspace.widthAnchor.constraint(equalTo: stack.widthAnchor),
            status.widthAnchor.constraint(equalTo: stack.widthAnchor)
        ])
        panel.center()
        update()
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }
    override func showWindow(_ sender: Any?) {
        super.showWindow(sender)
        Task { @MainActor in
            do { try await service.refresh(); update() }
            catch { update(); status.stringValue = error.localizedDescription }
        }
    }

    private func update() {
        if !busy { toggle.state = service.enabled ? .on : .off }
        toggle.isEnabled = !busy
        for button in copyButtons { button.isEnabled = service.enabled && !busy }
        workspace.stringValue = (service.settings["workspace"] as? String).map { "Automation workspace: " + $0 } ?? ""
        workspace.isHidden = workspace.stringValue.isEmpty
        status.stringValue = busy ? "Updating…" : service.enabled ? "MCP is enabled on this Mac. Any local app can connect without a token. Keep Keynope running." : "MCP is disabled. Enable access to copy connection details."
        // Accommodate wrapping and larger accessibility text without clipping.
        window?.contentMinSize = NSSize(width: 560, height: 440)
    }

    @objc private func changeEnabled() {
        let value = toggle.state == .on
        busy = true
        update()
        Task { @MainActor in
            do { try await service.setEnabled(value); busy = false; update() }
            catch { busy = false; update(); status.stringValue = error.localizedDescription }
        }
    }

    @objc private func copyConnection(_ sender: NSButton) {
        guard let format = AutomationService.CopyFormat(rawValue: sender.tag) else { return }
        do {
            let text = try service.copyText(format)
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(text, forType: .string)
            status.stringValue = format == .configuration ? "MCP configuration copied to clipboard." : "Command copied. Run it in your terminal to add Keynope."
        } catch { status.stringValue = error.localizedDescription }
    }
}

#if KEYNOPE_NATIVE_ACCEPTANCE
// Exercise the actual signed service and native settings without activating an
// editor, touching a user's deck/client settings, or changing their clipboard.
@MainActor
enum AutomationSettingsAcceptance {
    static func run() {
        NSApplication.shared.setActivationPolicy(.prohibited)
        Task { @MainActor in
            let root = URL(fileURLWithPath: NSTemporaryDirectory()).appendingPathComponent("keynope-mcp-settings-" + UUID().uuidString, isDirectory: true)
            let suite = "sh.keynope.mcp-test." + UUID().uuidString
            let preferences = UserDefaults(suiteName: suite)!
            let service = AutomationService(workspaceDirectory: root, preferences: preferences)
            defer { service.stop(); preferences.removePersistentDomain(forName: suite) }
            do {
                func check(_ condition: Bool, _ message: String) throws {
                    if !condition { throw NSError(domain: "Keynope MCP acceptance", code: 1, userInfo: [NSLocalizedDescriptionKey: message]) }
                }
                @MainActor func connection() throws -> URL {
                    let data = Data(try service.copyText(.configuration).utf8)
                    let configuration = try JSONSerialization.jsonObject(with: data) as! [String: Any]
                    let server = (configuration["mcpServers"] as! [String: [String: Any]])["keynope"]!
                    try check(server["headers"] == nil && server.count == 2, "configuration must contain only type and URL, no authentication")
                    return URL(string: server["url"] as! String)!
                }
                func rpc(_ method: String, _ params: [String: Any], url: URL) async throws -> (Int, [String: Any]) {
                    var request = URLRequest(url: url)
                    request.timeoutInterval = 10
                    request.httpMethod = "POST"
                    request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                    request.setValue("application/json, text/event-stream", forHTTPHeaderField: "Accept")
                    request.setValue("2025-06-18", forHTTPHeaderField: "MCP-Protocol-Version")
                    request.httpBody = try JSONSerialization.data(withJSONObject: ["jsonrpc": "2.0", "id": 1, "method": method, "params": params])
                    let (data, response) = try await URLSession.shared.data(for: request)
                    let code = (response as! HTTPURLResponse).statusCode
                    if code != 200 { return (code, [:]) }
                    let text = String(decoding: data, as: UTF8.self)
                    let payload = text.split(separator: "\n").first(where: { $0.hasPrefix("data: ") }).map { Data($0.dropFirst(6).utf8) } ?? data
                    return (code, try JSONSerialization.jsonObject(with: payload) as! [String: Any])
                }
                do { _ = try service.copyText(.configuration); throw CocoaError(.fileWriteUnknown) }
                catch let error as NSError { try check(error.domain == "sh.keynope.mcp", "copying disabled settings must fail without enabling") }
                try await service.setEnabled(true)
                try check(service.enabled && preferences.bool(forKey: AutomationService.enabledKey), "enabled preference")
                let url = try connection()
                let codex = try service.copyText(.codex), claude = try service.copyText(.claude)
                try check(codex == "codex mcp add keynope --url '" + url.absoluteString + "'", "token-free Codex command")
                try check(claude == "claude mcp add --transport http keynope '" + url.absoluteString + "'", "token-free Claude command")
                try await service.refresh()
                try check(try connection() == url, "copying/refresh must not change endpoint")
                let initResult = try await rpc("initialize", ["protocolVersion": "2025-06-18", "capabilities": [:], "clientInfo": ["name": "native acceptance", "version": "1"]], url: url)
                try check(initResult.0 == 200 && initResult.1["result"] != nil, "signed service initialize")
                let inspect = try await rpc("tools/call", ["name": "deck_inspect", "arguments": [:]], url: url)
                let result = inspect.1["result"] as! [String: Any]
                let deck = result["structuredContent"] as! [String: Any]
                try check(deck["attached"] as? Bool == false && (deck["workspace"] as? String) == root.path && (deck["path"] as? String)?.hasPrefix(root.path + "/") == true, "isolated workspace, not editor document")
                let settings = AutomationSettingsController(service: service)
                guard let view = settings.window?.contentView else { throw CocoaError(.fileReadCorruptFile) }
                view.layoutSubtreeIfNeeded()
                @MainActor func descendants(_ view: NSView) -> [NSView] { [view] + view.subviews.flatMap { descendants($0) } }
                let buttons = descendants(view).compactMap { $0 as? NSButton }
                for name in ["Enable MCP access", "Copy Codex Command", "Copy Claude Command", "Copy MCP Configuration"] {
                    try check(buttons.contains { $0.title == name && $0.isEnabled }, "missing enabled settings control: " + name)
                }
                try check(!view.hasAmbiguousLayout && buttons.allSatisfy { view.bounds.contains(view.convert($0.bounds, from: $0)) }, "settings controls fit the panel")
                if let image = view.bitmapImageRepForCachingDisplay(in: view.bounds) {
                    view.cacheDisplay(in: view.bounds, to: image)
                    try image.representation(using: .png, properties: [:])?.write(to: root.appendingPathComponent("settings.png"))
                }
                try await service.setEnabled(false)
                let denied = try await rpc("tools/list", [:], url: url)
                try check(!service.enabled && denied.0 == 403, "disable revokes access")
                try await service.setEnabled(true)
                try check(try connection() == url, "re-enable retains endpoint")
                let resumed = try await rpc("tools/list", [:], url: url)
                try check(resumed.0 == 200, "re-enabled MCP accepts local clients without authentication")
                try await service.setEnabled(false)
                settings.close()
                service.stop()
                preferences.removePersistentDomain(forName: suite)
                print("PASS: token-free signed sandbox MCP, isolated workspace, separate copy actions, stable reads, disable/re-enable, native panel layout. Evidence: " + root.path)
                exit(0)
            } catch {
                service.stop()
                preferences.removePersistentDomain(forName: suite)
                FileHandle.standardError.write(Data(("MCP settings acceptance failed: " + error.localizedDescription + "\n").utf8))
                exit(1)
            }
        }
        NSApplication.shared.run()
    }
}
#endif

// Launch the inherited-sandbox engine from the signed app, not directly from
// an unsandboxed MCP client. The app holds the workspace grant for its lifetime.
@MainActor
enum AutomationServer {
    static func run(arguments: [String]) {
        let app = NSApplication.shared
        app.setActivationPolicy(.prohibited)
        do {
            guard arguments.count == 3, arguments[0] == "--mcp", arguments[1] == "--workspace",
                  arguments[2].hasPrefix("/") else {
                throw NSError(domain: "sh.keynope.mcp", code: 2, userInfo: [NSLocalizedDescriptionKey: "Usage: Keynope --mcp --workspace /absolute/directory"])
            }
            let directory = URL(fileURLWithPath: arguments[2], isDirectory: true).standardizedFileURL.resolvingSymlinksInPath()
            let key = "keynope.mcp-workspace." + Data(directory.path.utf8).base64EncodedString()
            var scoped: URL?
            defer { scoped?.stopAccessingSecurityScopedResource() }
            if let bookmark = UserDefaults.standard.data(forKey: key) {
                var stale = false
                if let restored = try? URL(resolvingBookmarkData: bookmark, options: [.withSecurityScope, .withoutUI], relativeTo: nil, bookmarkDataIsStale: &stale),
                   restored.standardizedFileURL.resolvingSymlinksInPath() == directory,
                   restored.startAccessingSecurityScopedResource() {
                    scoped = restored
                    if stale, let refreshed = try? restored.bookmarkData(options: .withSecurityScope, includingResourceValuesForKeys: nil, relativeTo: nil) { UserDefaults.standard.set(refreshed, forKey: key) }
                }
            }
            // App-container/temp workspaces need no panel. For external folders
            // the first connection requests a real sandbox grant, never a bypass.
            if !canUse(directory) {
                let panel = NSOpenPanel()
                panel.title = "Allow Keynope Deck Automation"
                panel.message = "Select “\(directory.lastPathComponent)” to allow your MCP client to create, read and save presentations in this folder."
                panel.prompt = "Allow Folder"
                panel.directoryURL = directory
                panel.canChooseFiles = false
                panel.canChooseDirectories = true
                panel.allowsMultipleSelection = false
                guard panel.runModal() == .OK, let selected = panel.url?.standardizedFileURL.resolvingSymlinksInPath(), selected == directory else { throw CocoaError(.userCancelled) }
                let bookmark = try selected.bookmarkData(options: .withSecurityScope, includingResourceValuesForKeys: nil, relativeTo: nil)
                UserDefaults.standard.set(bookmark, forKey: key)
                scoped?.stopAccessingSecurityScopedResource()
                _ = selected.startAccessingSecurityScopedResource()
                scoped = selected
            }
            let process = Process()
            process.executableURL = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/keynope-engine")
            process.arguments = ["--mcp", "--workspace", directory.path]
            var environment = ProcessInfo.processInfo.environment
            // Cross-process bookmarks differ from persistent app-scoped ones.
            environment["KEYNOPE_SANDBOX_BOOKMARK"] = try directory.bookmarkData(options: [], includingResourceValuesForKeys: nil, relativeTo: nil).base64EncodedString()
            process.environment = environment
            process.standardInput = FileHandle.standardInput
            process.standardOutput = FileHandle.standardOutput
            process.standardError = FileHandle.standardError
            try process.run()
            process.waitUntilExit()
            exit(process.terminationReason == .exit ? process.terminationStatus : 1)
        } catch {
            FileHandle.standardError.write(Data(("Keynope MCP: \(error.localizedDescription)\n").utf8))
            exit(1)
        }
    }

    private static func canUse(_ directory: URL) -> Bool {
        let probe = directory.appendingPathComponent(".keynope-mcp-access-" + UUID().uuidString)
        do {
            _ = try FileManager.default.contentsOfDirectory(atPath: directory.path)
            try Data().write(to: probe, options: .withoutOverwriting)
            try FileManager.default.removeItem(at: probe)
            return true
        } catch { return false }
    }
}

// A hidden, non-persistent instance of the app's real export renderer, not a
// second renderer or a screenshot of the user's desktop. No app activation.
@MainActor
enum AutomationRenderer {
    struct Request: Decodable {
        let html: String
        let page: Int
        let width: Int
        let format: String
    }

    static func run() {
        let app = NSApplication.shared
        app.setActivationPolicy(.prohibited)
        Task { @MainActor in
            let renderer = SlideExportRenderer()
            defer { renderer.close() }
            do {
                var requestData = Data()
                while let chunk = try FileHandle.standardInput.read(upToCount: 1_048_576), !chunk.isEmpty {
                    guard requestData.count + chunk.count <= 160 * 1024 * 1024 else { throw CocoaError(.fileReadTooLarge) }
                    requestData.append(chunk)
                }
                let request = try JSONDecoder().decode(Request.self, from: requestData)
                if request.format == "fonts" || request.format == "font" {
                    let result: Any
                    if request.format == "fonts" { result = PresenterDelegate.installedFontCatalogue() }
                    else { result = try PresenterDelegate.installedTypeface(request.html) }
                    let data = try JSONSerialization.data(withJSONObject: result)
                    try FileHandle.standardOutput.write(contentsOf: data)
                    exit(0)
                }
                guard (320...2048).contains(request.width), request.page >= 0,
                      ["png", "pdf"].contains(request.format) else { throw CocoaError(.fileReadCorruptFile) }
                guard request.html.utf8.count <= 128 * 1024 * 1024 else { throw CocoaError(.fileReadTooLarge) }
                let html = request.html
                // Automation HTML is self-contained. Prevent network requests
                // even if an authored resource somehow survived admission.
                let rules = """
                [{"trigger":{"url-filter":"^https?://"},"action":{"type":"block"}}]
                """
                let blocker = try await WKContentRuleListStore.default().compileContentRuleList(forIdentifier: "keynope-automation-offline", encodedContentRuleList: rules)
                if let blocker { renderer.view.configuration.userContentController.add(blocker) }
                try await renderer.load(html)
                let pages = try await renderer.pages()
                guard request.page < pages.count else { throw CocoaError(.validationMissingMandatoryProperty) }
                let result: Data
                if request.format == "pdf" {
                    let document = PDFDocument()
                    for index in pages.indices {
                        try size(renderer, page: pages[index])
                        let bytes = try await renderer.imageData(page: (pages[index]["index"] as? Int) ?? index)
                        guard let image = NSImage(data: bytes), let page = PDFPage(image: image) else { throw CocoaError(.fileReadCorruptFile) }
                        document.insert(page, at: document.pageCount)
                    }
                    guard let bytes = document.dataRepresentation() else { throw CocoaError(.fileWriteUnknown) }
                    result = bytes
                } else {
                    try size(renderer, page: pages[request.page])
                    let bytes = try await renderer.imageData(page: (pages[request.page]["index"] as? Int) ?? request.page)
                    guard let bitmap = NSBitmapImageRep(data: bytes), bitmap.pixelsWide > 0, bitmap.pixelsHigh > 0 else { throw CocoaError(.fileReadCorruptFile) }
                    let height = max(1, Int((Double(request.width) * Double(bitmap.pixelsHigh) / Double(bitmap.pixelsWide)).rounded()))
                    guard height <= 8192, request.width * height <= 16_000_000,
                          let scaled = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: request.width, pixelsHigh: height, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0),
                          let graphics = NSGraphicsContext(bitmapImageRep: scaled) else { throw CocoaError(.fileReadTooLarge) }
                    NSGraphicsContext.saveGraphicsState()
                    NSGraphicsContext.current = graphics
                    graphics.imageInterpolation = .high
                    NSImage(data: bytes)?.draw(in: NSRect(x: 0, y: 0, width: request.width, height: height))
                    NSGraphicsContext.restoreGraphicsState()
                    guard let png = scaled.representation(using: .png, properties: [:]) else { throw CocoaError(.fileWriteUnknown) }
                    result = png
                }
                try FileHandle.standardOutput.write(contentsOf: result)
                exit(0)
            } catch {
                FileHandle.standardError.write(Data(("Keynope MCP render: \(error.localizedDescription)\n").utf8))
                exit(1)
            }
        }
        app.run()
    }

    private static func size(_ renderer: SlideExportRenderer, page: [String: Any]) throws {
        let width = (page["width"] as? Double) ?? 1920
        let height = (page["height"] as? Double) ?? 1080
        guard width.isFinite, height.isFinite, width > 0, height > 0, width * height <= 16_000_000 else { throw CocoaError(.fileReadTooLarge) }
        try renderer.setPageSize(width: width, height: height)
    }
}
