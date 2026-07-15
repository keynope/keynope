import Cocoa
import Darwin
@preconcurrency import ScreenCaptureKit
import UniformTypeIdentifiers
import WebKit

final class PresenterWindow: NSWindow {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { true }
}

@MainActor
final class PresenterDelegate: NSObject, NSApplicationDelegate, WKScriptMessageHandler, NSMenuDelegate, WKUIDelegate {
    private var presenterURL: URL
    private let appMode: Bool
    private let openDeckHandler: ((String) -> URL?)?
    private let inputHandler: ((Data) -> Void)?
    private var statusItem: NSStatusItem?
    private var editorWindow: NSWindow?
    private var editorWebView: WKWebView?
    private var window: NSWindow?
    private var webView: WKWebView?
    private var compositeView: PresenterCompositeView?
    private var noPresentationItem: NSMenuItem?
    private var externalDisplayItem: NSMenuItem?
    private var mainDisplayItem: NSMenuItem?
    private var shareRootItem: NSMenuItem?
    private var shareMenu: NSMenu?
    private var shareableContent: SCShareableContent?
    private var loadingShareSources = false
    private let screenShareController = ScreenShareController()
    private var presentationMode: String = "none"
    private let presentationModeKey = "keynope.presenter.mode"

    init(
        url: URL,
        appMode: Bool = false,
        openDeckHandler: ((String) -> URL?)? = nil,
        inputHandler: ((Data) -> Void)? = nil
    ) {
        self.presenterURL = url
        self.appMode = appMode
        self.openDeckHandler = openDeckHandler
        self.inputHandler = inputHandler
        super.init()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(appMode ? .regular : .accessory)
        createStatusItem()
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(screenParametersChanged),
            name: NSApplication.didChangeScreenParametersNotification,
            object: nil
        )
        updateDisplayState()
        restorePresentationMode()
        screenShareController.onStopped = { [weak self] error in
            self?.refreshShareMenu()
            self?.showCaptureError(error)
        }
        if appMode {
            createMainMenu()
            showEditorWindow()
        }
    }

    private func restorePresentationMode() {
        let savedMode = UserDefaults.standard.string(forKey: presentationModeKey) ?? "none"
        switch savedMode {
        case "external":
            if hasExternalDisplay {
                showPresentation(preferExternal: true, remember: false)
            } else {
                noPresentation(remember: false)
            }
        case "main":
            showPresentation(preferExternal: false, remember: false)
        default:
            noPresentation(remember: false)
        }
    }

    private func createStatusItem() {
        let item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        let image = statusImage()
        if let image {
            image.isTemplate = true
            image.size = NSSize(width: 18, height: 18)
            item.button?.image = image
        } else {
            item.button?.title = "K"
        }
        item.button?.toolTip = "Keynope Presenter"

        let menu = NSMenu()
        if appMode {
            menu.addItem(NSMenuItem(title: "Show Keynope", action: #selector(showEditor), keyEquivalent: "k"))
            menu.addItem(NSMenuItem.separator())
        }
        let noneItem = NSMenuItem(title: "No Presentation", action: #selector(noPresentationSelected), keyEquivalent: "n")
        menu.addItem(noneItem)
        noPresentationItem = noneItem
        let externalItem = NSMenuItem(title: "Show on External Display", action: #selector(showExternal), keyEquivalent: "e")
        menu.addItem(externalItem)
        externalDisplayItem = externalItem
        let mainItem = NSMenuItem(title: "Show on Main Display", action: #selector(showMain), keyEquivalent: "m")
        menu.addItem(mainItem)
        mainDisplayItem = mainItem
        menu.addItem(NSMenuItem.separator())
        let shareItem = NSMenuItem(title: "Share", action: nil, keyEquivalent: "")
        let shareSubmenu = NSMenu(title: "Share")
        shareSubmenu.delegate = self
        shareItem.submenu = shareSubmenu
        menu.addItem(shareItem)
        shareRootItem = shareItem
        shareMenu = shareSubmenu
        refreshShareMenu()
        menu.addItem(NSMenuItem.separator())
        menu.addItem(NSMenuItem(title: "Reload", action: #selector(reload), keyEquivalent: "r"))
        menu.addItem(NSMenuItem.separator())
        menu.addItem(NSMenuItem(title: appMode ? "Quit Keynope" : "Quit Keynope Presenter", action: #selector(quit), keyEquivalent: "q"))
        for item in menu.items {
            item.target = self
        }
        item.menu = menu
        statusItem = item
    }

    private func createMainMenu() {
        let mainMenu = NSMenu()
        let applicationItem = NSMenuItem()
        let applicationMenu = NSMenu()
        applicationMenu.addItem(withTitle: "About Keynope", action: #selector(NSApplication.orderFrontStandardAboutPanel(_:)), keyEquivalent: "")
        applicationMenu.addItem(NSMenuItem.separator())
        applicationMenu.addItem(withTitle: "Quit Keynope", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        applicationItem.submenu = applicationMenu
        mainMenu.addItem(applicationItem)

        let fileItem = NSMenuItem()
        let fileMenu = NSMenu(title: "File")
        let openItem = fileMenu.addItem(withTitle: "Open…", action: #selector(openDeck), keyEquivalent: "o")
        openItem.target = self
        fileMenu.addItem(NSMenuItem.separator())
        fileMenu.addItem(withTitle: "Close", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        fileItem.submenu = fileMenu
        mainMenu.addItem(fileItem)

        let editItem = NSMenuItem()
        let editMenu = NSMenu(title: "Edit")
        editMenu.addItem(withTitle: "Undo", action: Selector(("undo:")), keyEquivalent: "z")
        let redoItem = editMenu.addItem(withTitle: "Redo", action: Selector(("redo:")), keyEquivalent: "Z")
        redoItem.keyEquivalentModifierMask = [.command, .shift]
        editMenu.addItem(NSMenuItem.separator())
        editMenu.addItem(withTitle: "Cut", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "Copy", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "Paste", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu
        mainMenu.addItem(editItem)

        let windowItem = NSMenuItem()
        let windowMenu = NSMenu(title: "Window")
        let showItem = windowMenu.addItem(withTitle: "Show Keynope", action: #selector(showEditor), keyEquivalent: "0")
        showItem.target = self
        windowItem.submenu = windowMenu
        mainMenu.addItem(windowItem)
        NSApp.mainMenu = mainMenu
        NSApp.windowsMenu = windowMenu
    }

    private func editorURL() -> URL {
        presenterURLForSurface("app")
    }

    private func showEditorWindow() {
        if let editorWindow {
            editorWindow.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            return
        }
        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "keynopeInput")
        config.userContentController.add(self, name: "keynopePresenter")
        let visibleFrame = NSScreen.main?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1440, height: 900)
        let windowSize = NSSize(
            width: min(1440, max(1100, visibleFrame.width * 0.92)),
            height: min(960, max(720, visibleFrame.height * 0.92))
        )
        let initialFrame = NSRect(origin: .zero, size: windowSize)
        let view = WKWebView(frame: initialFrame, configuration: config)
        view.uiDelegate = self
        view.autoresizingMask = [.width, .height]
        view.load(URLRequest(url: editorURL()))
        let newWindow = PresenterWindow(
            contentRect: initialFrame,
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered,
            defer: false
        )
        newWindow.title = "Keynope"
        newWindow.contentView = view
        newWindow.isReleasedWhenClosed = false
        newWindow.minSize = NSSize(width: 960, height: 640)
        newWindow.setFrameAutosaveName("KeynopeEditorWindow")
        newWindow.center()
        newWindow.makeKeyAndOrderFront(nil)
        editorWindow = newWindow
        editorWebView = view
        NSApp.activate(ignoringOtherApps: true)
    }

    func webView(
        _ webView: WKWebView,
        runOpenPanelWith parameters: WKOpenPanelParameters,
        initiatedByFrame frame: WKFrameInfo,
        completionHandler: @escaping @MainActor @Sendable ([URL]?) -> Void
    ) {
        let panel = NSOpenPanel()
        panel.title = "Import an Image"
        panel.prompt = "Import"
        panel.allowedContentTypes = [.image]
        panel.allowsMultipleSelection = parameters.allowsMultipleSelection
        panel.canChooseDirectories = parameters.allowsDirectories
        panel.canChooseFiles = true
        if let hostWindow = editorWindow ?? webView.window {
            panel.beginSheetModal(for: hostWindow) { response in
                completionHandler(response == .OK ? panel.urls : nil)
            }
        } else {
            completionHandler(panel.runModal() == .OK ? panel.urls : nil)
        }
    }

    @objc private func showEditor() {
        showEditorWindow()
    }

    @objc private func openDeck() {
        let panel = NSOpenPanel()
        panel.title = "Open a Keynope Deck"
        panel.prompt = "Open"
        panel.allowedContentTypes = ["md", "markdown"].compactMap { UTType(filenameExtension: $0) }
        panel.allowsMultipleSelection = false
        guard panel.runModal() == .OK, let path = panel.url?.path else {
            return
        }
        replaceDeck(path: path)
    }

    private func replaceDeck(path: String) {
        guard let newURL = openDeckHandler?(path) else {
            let alert = NSAlert()
            alert.alertStyle = .warning
            alert.messageText = "Could Not Open Deck"
            alert.informativeText = path
            alert.runModal()
            return
        }
        noPresentation()
        presenterURL = newURL
        editorWebView?.load(URLRequest(url: editorURL()))
        editorWindow?.title = "Keynope — \(URL(fileURLWithPath: path).lastPathComponent)"
        showEditorWindow()
    }

    private var hasExternalDisplay: Bool {
        guard let main = NSScreen.main else {
            return NSScreen.screens.count > 1
        }
        return NSScreen.screens.contains { $0 != main }
    }

    private func updateDisplayState() {
        if hasExternalDisplay {
            externalDisplayItem?.title = "Show on External Display"
            externalDisplayItem?.isEnabled = true
            statusItem?.button?.toolTip = "Keynope Presenter"
        } else {
            externalDisplayItem?.title = "No External Display Connected"
            externalDisplayItem?.isEnabled = false
            statusItem?.button?.toolTip = "Keynope Presenter - no external display connected"
            if presentationMode == "external" {
                noPresentation(remember: false)
            }
        }
        updatePresentationMenuState()
    }

    private func updatePresentationMenuState() {
        noPresentationItem?.state = presentationMode == "none" ? .on : .off
        externalDisplayItem?.state = presentationMode == "external" ? .on : .off
        mainDisplayItem?.state = presentationMode == "main" ? .on : .off
        shareRootItem?.isEnabled = presentationMode != "none"
    }

    private func statusImage() -> NSImage? {
        if let data = Data(base64Encoded: keynopeMenuIconPNGBase64) {
            return NSImage(data: data)
        }
        return nil
    }

    private func targetScreen(preferExternal: Bool) -> NSScreen {
        if preferExternal, let main = NSScreen.main {
            if let external = NSScreen.screens.first(where: { $0 != main }) {
                return external
            }
        }
        return NSScreen.main ?? NSScreen.screens.first!
    }

    private func showPresentation(preferExternal: Bool, remember: Bool = true) {
        if preferExternal && !hasExternalDisplay {
            noPresentation()
            return
        }
        let screen = targetScreen(preferExternal: preferExternal)
        let frame = screen.frame
        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "keynopePresenter")
        let composite = PresenterCompositeView(frame: NSRect(origin: .zero, size: frame.size), configuration: config)
        let view = composite.webView
        view.autoresizingMask = [.width, .height]
        view.load(URLRequest(url: presenterURLForSurface(preferExternal ? "external" : "main")))

        let newWindow = PresenterWindow(
            contentRect: frame,
            styleMask: [.borderless],
            backing: .buffered,
            defer: false,
            screen: screen
        )
        newWindow.title = "Keynope Presenter"
        newWindow.backgroundColor = .black
        newWindow.contentView = composite
        newWindow.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        newWindow.isReleasedWhenClosed = false
        newWindow.level = .normal
        newWindow.setFrame(frame, display: true)
        newWindow.makeKeyAndOrderFront(nil)
        newWindow.makeFirstResponder(view)
        if !preferExternal {
            NSApp.activate(ignoringOtherApps: true)
        }
        newWindow.orderFrontRegardless()

        window?.close()
        window = newWindow
        webView = view
        compositeView = composite
        screenShareController.previewView = composite.captureView
        presentationMode = preferExternal ? "external" : "main"
        if remember {
            UserDefaults.standard.set(presentationMode, forKey: presentationModeKey)
        }
        updateDisplayState()
        refreshShareMenu()
        reportPresentationMode()
    }

    private func presenterURLForSurface(_ surface: String) -> URL {
        guard var components = URLComponents(url: presenterURL, resolvingAgainstBaseURL: false) else {
            return presenterURL
        }
        var items = components.queryItems ?? []
        items.removeAll { $0.name == "keynopeSurface" }
        items.append(URLQueryItem(name: "keynopeSurface", value: surface))
        components.queryItems = items
        return components.url ?? presenterURL
    }

    private func reportPresentationMode() {
        guard var components = URLComponents(url: presenterURL, resolvingAgainstBaseURL: false) else {
            return
        }
        components.path = "/presenter-status"
        components.query = nil
        guard let url = components.url,
              let body = try? JSONSerialization.data(withJSONObject: ["mode": presentationMode]) else {
            return
        }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = body
        URLSession.shared.dataTask(with: request).resume()
    }

    @objc private func showExternal() {
        guard hasExternalDisplay else {
            updateDisplayState()
            return
        }
        showPresentation(preferExternal: true)
    }

    @objc private func showMain() {
        showPresentation(preferExternal: false)
    }

    @objc private func noPresentationSelected() {
        noPresentation()
    }

    @objc private func reload() {
        webView?.reload()
    }

    private func noPresentation(remember: Bool = true) {
        Task {
            await screenShareController.stop()
        }
        window?.close()
        window = nil
        webView = nil
        compositeView = nil
        screenShareController.previewView = nil
        presentationMode = "none"
        if remember {
            UserDefaults.standard.set(presentationMode, forKey: presentationModeKey)
        }
        updatePresentationMenuState()
        refreshShareMenu()
        reportPresentationMode()
    }

    @objc private func screenParametersChanged() {
        updateDisplayState()
    }

    @objc private func quit() {
        NSApp.terminate(nil)
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        guard menu === shareMenu else {
            return
        }
        refreshShareSources()
    }

    private func refreshShareSources() {
        guard presentationMode != "none", !loadingShareSources else {
            return
        }
        loadingShareSources = true
        if shareableContent == nil {
            refreshShareMenu(loading: true)
        }
        SCShareableContent.getExcludingDesktopWindows(false, onScreenWindowsOnly: true) { [weak self] content, error in
            let result = ShareableContentResult(content: content, error: error)
            Task { @MainActor [weak self] in
                guard let self else {
                    return
                }
                self.loadingShareSources = false
                self.shareableContent = result.content
                self.refreshShareMenu(error: result.error)
            }
        }
    }

    private func refreshShareMenu(loading: Bool = false, error: Error? = nil) {
        guard let menu = shareMenu else {
            return
        }
        menu.removeAllItems()
        let stopItem = NSMenuItem(title: "Nothing", action: #selector(stopSharingSelected), keyEquivalent: "")
        stopItem.target = self
        stopItem.state = screenShareController.activeTarget == nil ? .on : .off
        menu.addItem(stopItem)
        menu.addItem(NSMenuItem.separator())

        guard presentationMode != "none" else {
            let unavailable = NSMenuItem(title: "Start a presentation to share content", action: nil, keyEquivalent: "")
            unavailable.isEnabled = false
            menu.addItem(unavailable)
            return
        }
        if loading {
            let loadingItem = NSMenuItem(title: "Loading Sources…", action: nil, keyEquivalent: "")
            loadingItem.isEnabled = false
            menu.addItem(loadingItem)
            return
        }
        if let error {
            let permissionDenied = !CGPreflightScreenCaptureAccess()
            let errorItem = NSMenuItem(
                title: permissionDenied ? "Screen Recording Permission Required" : "Could Not Load Share Sources",
                action: nil,
                keyEquivalent: ""
            )
            errorItem.toolTip = error.localizedDescription
            errorItem.isEnabled = false
            menu.addItem(errorItem)
            if permissionDenied {
                let settingsItem = NSMenuItem(title: "Open Screen Recording Settings…", action: #selector(openScreenRecordingSettings), keyEquivalent: "")
                settingsItem.target = self
                menu.addItem(settingsItem)
            } else {
                let retryItem = NSMenuItem(title: "Try Again", action: #selector(loadShareSources), keyEquivalent: "")
                retryItem.target = self
                menu.addItem(retryItem)
            }
            return
        }
        guard let content = shareableContent else {
            let loadItem = NSMenuItem(title: "Load Share Sources", action: #selector(loadShareSources), keyEquivalent: "")
            loadItem.target = self
            menu.addItem(loadItem)
            return
        }

        menu.addItem(shareApplicationsItem(content))
        menu.addItem(shareWindowsItem(content))
        menu.addItem(shareDisplaysItem(content))
        menu.addItem(NSMenuItem.separator())
        let reloadItem = NSMenuItem(title: "Refresh Sources", action: #selector(loadShareSources), keyEquivalent: "")
        reloadItem.target = self
        menu.addItem(reloadItem)
    }

    private func shareApplicationsItem(_ content: SCShareableContent) -> NSMenuItem {
        let root = NSMenuItem(title: "Applications", action: nil, keyEquivalent: "")
        let submenu = NSMenu(title: "Applications")
        let ownPID = ProcessInfo.processInfo.processIdentifier
        let visiblePIDs = Set(content.windows.compactMap { $0.owningApplication?.processID })
        let applications = content.applications
            .filter { $0.processID != ownPID && visiblePIDs.contains($0.processID) }
            .sorted { $0.applicationName.localizedCaseInsensitiveCompare($1.applicationName) == .orderedAscending }
        for application in applications {
            let target = ShareTarget(kind: .application, identifier: Int64(application.processID), title: application.applicationName)
            let item = shareMenuItem(target)
            if let icon = NSRunningApplication(processIdentifier: application.processID)?.icon?.copy() as? NSImage {
                icon.size = NSSize(width: 16, height: 16)
                item.image = icon
            }
            submenu.addItem(item)
        }
        addEmptyShareMessage(to: submenu, when: applications.isEmpty)
        root.submenu = submenu
        return root
    }

    private func shareWindowsItem(_ content: SCShareableContent) -> NSMenuItem {
        let root = NSMenuItem(title: "Windows", action: nil, keyEquivalent: "")
        let submenu = NSMenu(title: "Windows")
        let ownPID = ProcessInfo.processInfo.processIdentifier
        let windows = content.windows
            .filter {
                $0.windowLayer == 0 && $0.frame.width >= 80 && $0.frame.height >= 60 &&
                    $0.owningApplication?.processID != ownPID
            }
            .sorted { windowTitle($0).localizedCaseInsensitiveCompare(windowTitle($1)) == .orderedAscending }
        for window in windows {
            let target = ShareTarget(kind: .window, identifier: Int64(window.windowID), title: windowTitle(window))
            submenu.addItem(shareMenuItem(target))
        }
        addEmptyShareMessage(to: submenu, when: windows.isEmpty)
        root.submenu = submenu
        return root
    }

    private func shareDisplaysItem(_ content: SCShareableContent) -> NSMenuItem {
        let root = NSMenuItem(title: "Screens", action: nil, keyEquivalent: "")
        let submenu = NSMenu(title: "Screens")
        let displays = content.displays.sorted { displayName($0).localizedCaseInsensitiveCompare(displayName($1)) == .orderedAscending }
        for display in displays {
            let target = ShareTarget(kind: .display, identifier: Int64(display.displayID), title: displayName(display))
            submenu.addItem(shareMenuItem(target))
        }
        addEmptyShareMessage(to: submenu, when: displays.isEmpty)
        root.submenu = submenu
        return root
    }

    private func shareMenuItem(_ target: ShareTarget) -> NSMenuItem {
        let item = NSMenuItem(title: target.title, action: #selector(shareTargetSelected(_:)), keyEquivalent: "")
        item.target = self
        item.representedObject = target
        item.state = screenShareController.activeTarget?.key == target.key ? .on : .off
        return item
    }

    private func addEmptyShareMessage(to menu: NSMenu, when empty: Bool) {
        guard empty else {
            return
        }
        let item = NSMenuItem(title: "No Sources Available", action: nil, keyEquivalent: "")
        item.isEnabled = false
        menu.addItem(item)
    }

    private func windowTitle(_ window: SCWindow) -> String {
        let application = window.owningApplication?.applicationName ?? "Application"
        let title = window.title?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return title.isEmpty ? application : "\(application) — \(title)"
    }

    private func displayName(_ display: SCDisplay) -> String {
        if let screen = NSScreen.screens.first(where: { screenDisplayID($0) == display.displayID }) {
            return screen.localizedName
        }
        return "Screen \(display.displayID)"
    }

    private func screenDisplayID(_ screen: NSScreen) -> CGDirectDisplayID? {
        (screen.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber)?.uint32Value
    }

    private var preferredShareDisplayID: CGDirectDisplayID? {
        let presenterDisplayID = window?.screen.flatMap(screenDisplayID)
        if let main = NSScreen.main, let mainID = screenDisplayID(main), mainID != presenterDisplayID {
            return mainID
        }
        return NSScreen.screens.compactMap(screenDisplayID).first(where: { $0 != presenterDisplayID }) ?? presenterDisplayID
    }

    @objc private func loadShareSources() {
        shareableContent = nil
        refreshShareSources()
    }

    @objc private func stopSharingSelected() {
        Task {
            await screenShareController.stop()
            await MainActor.run {
                self.refreshShareMenu()
            }
        }
    }

    @objc private func shareTargetSelected(_ sender: NSMenuItem) {
        guard let target = sender.representedObject as? ShareTarget, let content = shareableContent else {
            return
        }
        Task {
            do {
                try await screenShareController.start(
                    target: target,
                    content: content,
                    preferredDisplayID: preferredShareDisplayID
                )
                await MainActor.run {
                    self.refreshShareMenu()
                }
            } catch {
                await MainActor.run {
                    self.refreshShareMenu()
                    self.showCaptureError(error)
                }
            }
        }
    }

    @objc private func openScreenRecordingSettings() {
        guard let url = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture") else {
            return
        }
        NSWorkspace.shared.open(url)
    }

    private func showCaptureError(_ error: Error) {
        guard presentationMode != "none" else {
            return
        }
        NSApp.activate(ignoringOtherApps: true)
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Could Not Share Content"
        alert.informativeText = error.localizedDescription
        alert.addButton(withTitle: "OK")
        alert.runModal()
    }

    nonisolated func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        MainActor.assumeIsolated {
            if message.name == "keynopeInput", let input = message.body as? String, let data = input.data(using: .utf8) {
                inputHandler?(data)
                return
            }
            guard message.name == "keynopePresenter",
                  let body = message.body as? [String: Any],
                  let action = body["action"] as? String else {
                return
            }
            if action == "stop" {
                noPresentation()
            } else if action == "show-main" {
                showPresentation(preferExternal: false)
            } else if action == "show-external" {
                showPresentation(preferExternal: true)
            }
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return false
    }

    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if appMode {
            showEditorWindow()
        }
        return true
    }

    func application(_ sender: NSApplication, openFiles filenames: [String]) {
        guard appMode, let path = filenames.first else {
            sender.reply(toOpenOrPrint: .failure)
            return
        }
        replaceDeck(path: path)
        sender.reply(toOpenOrPrint: .success)
    }
}

@main
@MainActor
struct KeynopePresenterMain {
    private static var delegate: PresenterDelegate?
    private static var parentMonitor: Timer?
    private static var engineProcess: Process?
    private static var engineInput: Pipe?

    static func main() {
        let args = ProcessInfo.processInfo.arguments
        let appMode = args.contains("--app") || Bundle.main.bundleIdentifier == "sh.keynope.app"
        let url: URL
        if appMode {
            guard let deckPath = deckPathForApp(arguments: args), let engineURL = startEngine(deckPath: deckPath) else {
                exit(1)
            }
            url = engineURL
        } else {
            guard args.count >= 2, let presenterURL = URL(string: args[1]) else {
                fputs("usage: KeynopePresenter <url> [--parent-pid pid]\n", stderr)
                exit(2)
            }
            url = presenterURL
        }
        let parentPID: pid_t? = {
            guard let index = args.firstIndex(of: "--parent-pid"), index+1 < args.count else {
                return nil
            }
            return pid_t(args[index+1])
        }()

        let app = NSApplication.shared
        delegate = PresenterDelegate(
            url: url,
            appMode: appMode,
            openDeckHandler: appMode ? { path in startEngine(deckPath: path) } : nil,
            inputHandler: appMode ? { data in
                try? engineInput?.fileHandleForWriting.write(contentsOf: data)
            } : nil
        )
        app.delegate = delegate
        if let parentPID {
            parentMonitor = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { _ in
                if kill(parentPID, 0) != 0 && errno == ESRCH {
                    Task { @MainActor in
                        NSApp.terminate(nil)
                    }
                }
            }
        }
        app.run()
        if let engineProcess, engineProcess.isRunning {
            try? engineInput?.fileHandleForWriting.close()
            engineProcess.terminate()
            engineProcess.waitUntilExit()
        }
        engineInput = nil
    }

    private static func deckPathForApp(arguments: [String]) -> String? {
        if let index = arguments.firstIndex(of: "--app"), index + 1 < arguments.count {
            return arguments[index + 1]
        }
        let panel = NSOpenPanel()
        panel.title = "Open a Keynope Deck"
        panel.prompt = "Open"
        panel.allowedContentTypes = ["md", "markdown"].compactMap { UTType(filenameExtension: $0) }
        panel.allowsMultipleSelection = false
        return panel.runModal() == .OK ? panel.url?.path : nil
    }

    private static func startEngine(deckPath: String) -> URL? {
        if let engineProcess, engineProcess.isRunning {
            try? engineInput?.fileHandleForWriting.close()
            engineProcess.terminate()
            engineProcess.waitUntilExit()
        }
        engineProcess = nil
        engineInput = nil
        let engineURL = Bundle.main.bundleURL
            .appendingPathComponent("Contents/Helpers/keynope-engine")
        let process = Process()
        process.executableURL = engineURL
        process.arguments = ["--app", deckPath]
        var environment = ProcessInfo.processInfo.environment
        environment["COLUMNS"] = "100"
        environment["LINES"] = "32"
        process.environment = environment
        let output = Pipe()
        let input = Pipe()
        process.standardOutput = output
        process.standardError = FileHandle.standardError
        process.standardInput = input
        do {
            try process.run()
        } catch {
            fputs("could not start Keynope engine: \(error)\n", stderr)
            return nil
        }
        engineProcess = process
        engineInput = input
        var pending = Data()
        while process.isRunning {
            let data = output.fileHandleForReading.availableData
            if data.isEmpty { break }
            pending.append(data)
            while let newline = pending.firstIndex(of: 10) {
                let lineData = pending[..<newline]
                pending.removeSubrange(...newline)
                guard let line = String(data: lineData, encoding: .utf8) else { continue }
                if line.hasPrefix("KEYNOPE_URL="), let url = URL(string: String(line.dropFirst(12))) {
                    return url
                }
            }
        }
        process.terminate()
        engineProcess = nil
        fputs("Keynope engine did not provide an application URL\n", stderr)
        return nil
    }
}
