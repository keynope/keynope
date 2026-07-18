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
final class PresenterDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, WKScriptMessageHandler, NSMenuDelegate, WKUIDelegate {
    private var presenterURL: URL
    private let appMode: Bool
    private let openDeckHandler: ((String) -> URL?)?
    private let newDeckHandler: (() -> URL?)?
    private let activeDeckURLHandler: (() -> URL?)?
    private let recentDeckPathsHandler: (() -> [String])?
    private let inputHandler: ((Data) -> Void)?
    private var statusItem: NSStatusItem?
    private var editorWindow: NSWindow?
    private var editorWebView: WKWebView?
    private var aboutWindow: NSWindow?
    private var window: NSWindow?
    private var webView: WKWebView?
    private var compositeView: PresenterCompositeView?
    private var noPresentationItem: NSMenuItem?
    private var externalDisplayItem: NSMenuItem?
    private var mainDisplayItem: NSMenuItem?
    private var shareRootItem: NSMenuItem?
    private var shareMenu: NSMenu?
    private var recentDecksMenu: NSMenu?
    private var shareableContent: SCShareableContent?
    private var loadingShareSources = false
    private let screenShareController = ScreenShareController()
    private var presentationMode: String = "none"
    private var presentationPaused = false
    private var terminatingAfterEditorClose = false
    private let presentationModeKey = "keynope.presenter.mode"

    init(
        url: URL,
        appMode: Bool = false,
        openDeckHandler: ((String) -> URL?)? = nil,
        newDeckHandler: (() -> URL?)? = nil,
        activeDeckURLHandler: (() -> URL?)? = nil,
        recentDeckPathsHandler: (() -> [String])? = nil,
        inputHandler: ((Data) -> Void)? = nil
    ) {
        self.presenterURL = url
        self.appMode = appMode
        self.openDeckHandler = openDeckHandler
        self.newDeckHandler = newDeckHandler
        self.activeDeckURLHandler = activeDeckURLHandler
        self.recentDeckPathsHandler = recentDeckPathsHandler
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
        let aboutItem = applicationMenu.addItem(withTitle: "About Keynope", action: #selector(showAbout), keyEquivalent: "")
        aboutItem.target = self
        applicationMenu.addItem(NSMenuItem.separator())
        applicationMenu.addItem(withTitle: "Quit Keynope", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        applicationItem.submenu = applicationMenu
        mainMenu.addItem(applicationItem)

        let fileItem = NSMenuItem()
        let fileMenu = NSMenu(title: "File")
        let newItem = fileMenu.addItem(withTitle: "New Presentation…", action: #selector(newDeck), keyEquivalent: "n")
        newItem.target = self
        let openItem = fileMenu.addItem(withTitle: "Open…", action: #selector(openDeck), keyEquivalent: "o")
        openItem.target = self
        let recentItem = NSMenuItem(title: "Open Recent", action: nil, keyEquivalent: "")
        let recentMenu = NSMenu(title: "Open Recent")
        recentMenu.delegate = self
        recentItem.submenu = recentMenu
        recentDecksMenu = recentMenu
        fileMenu.addItem(recentItem)
        fileMenu.addItem(NSMenuItem.separator())
        let libraryItem = fileMenu.addItem(withTitle: "Show Presentation in Finder", action: #selector(showPresentationsLibrary), keyEquivalent: "")
        libraryItem.target = self
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

    private var versionedAppName: String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
        guard let version, !version.isEmpty else { return "Keynope" }
        return "Keynope \(version)"
    }

    private func updateEditorWindowTitle(deckPath: String? = nil) {
        let path = deckPath ?? activeDeckURLHandler?()?.path
        guard let path, !path.isEmpty else {
            editorWindow?.title = versionedAppName
            return
        }
        editorWindow?.title = "\(versionedAppName) — \(URL(fileURLWithPath: path).lastPathComponent)"
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
        newWindow.title = versionedAppName
        newWindow.contentView = view
        newWindow.isReleasedWhenClosed = false
        newWindow.delegate = self
        newWindow.minSize = NSSize(width: 960, height: 640)
        newWindow.setFrameAutosaveName("KeynopeEditorWindow")
        newWindow.center()
        newWindow.makeKeyAndOrderFront(nil)
        editorWindow = newWindow
        editorWebView = view
        updateEditorWindowTitle()
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

    @objc private func showAbout() {
        if let aboutWindow {
            NSApp.activate(ignoringOtherApps: true)
            aboutWindow.makeKeyAndOrderFront(nil)
            return
        }
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 520, height: 440),
            styleMask: [.titled, .closable],
            backing: .buffered,
            defer: false
        )
        panel.title = "About Keynope"
        panel.titleVisibility = .hidden
        panel.titlebarAppearsTransparent = true
        panel.isMovableByWindowBackground = true
        panel.isReleasedWhenClosed = false

        let background = NSVisualEffectView()
        background.material = .underWindowBackground
        background.blendingMode = .behindWindow
        background.state = .active
        panel.contentView = background

        let iconView = NSImageView(image: NSApp.applicationIconImage)
        iconView.imageScaling = .scaleProportionallyUpOrDown
        iconView.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            iconView.widthAnchor.constraint(equalToConstant: 104),
            iconView.heightAnchor.constraint(equalToConstant: 104)
        ])

        let nameLabel = aboutLabel("Keynope", size: 28, weight: .semibold, color: .labelColor)
        let versionLabel = aboutLabel("Version \(version)", size: 12, weight: .medium, color: .secondaryLabelColor)
        let versionBadge = NSView()
        versionBadge.wantsLayer = true
        versionBadge.layer?.backgroundColor = NSColor.controlBackgroundColor.cgColor
        versionBadge.layer?.cornerRadius = 10
        versionBadge.addSubview(versionLabel)
        versionBadge.translatesAutoresizingMaskIntoConstraints = false
        versionLabel.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            versionBadge.widthAnchor.constraint(greaterThanOrEqualToConstant: 104),
            versionBadge.heightAnchor.constraint(equalToConstant: 22),
            versionLabel.centerXAnchor.constraint(equalTo: versionBadge.centerXAnchor),
            versionLabel.centerYAnchor.constraint(equalTo: versionBadge.centerYAnchor, constant: -1.5),
            versionLabel.leadingAnchor.constraint(greaterThanOrEqualTo: versionBadge.leadingAnchor, constant: 10),
            versionLabel.trailingAnchor.constraint(lessThanOrEqualTo: versionBadge.trailingAnchor, constant: -10)
        ])

        let projectTitle = aboutLabel("PROJECT", size: 10, weight: .semibold, color: .tertiaryLabelColor)
        let projectLinks = NSStackView(views: [
            aboutLinkButton(
                "keynope.sh",
                url: "https://keynope.sh/",
                icon: NSImage(systemSymbolName: "globe", accessibilityDescription: "Website")
            ),
            aboutLinkButton("GitHub", url: "https://github.com/keynope/", icon: githubMarkImage())
        ])
        projectLinks.orientation = .horizontal
        projectLinks.alignment = .centerY
        projectLinks.distribution = .fillEqually
        projectLinks.spacing = 10

        let separator = NSBox()
        separator.boxType = .separator

        let copyright = aboutLabel("(c) 2026 Dennis Vink", size: 12, weight: .medium, color: .secondaryLabelColor)
        let authorLinks = NSStackView(views: [
            aboutLinkButton("drvink.com", url: "https://drvink.com"),
            aboutLinkButton("LinkedIn", url: "https://linkedin.com/in/drvink/")
        ])
        authorLinks.orientation = .horizontal
        authorLinks.alignment = .centerY
        authorLinks.distribution = .fillEqually
        authorLinks.spacing = 10

        let stack = NSStackView(views: [
            iconView,
            nameLabel,
            versionBadge,
            projectTitle,
            projectLinks,
            separator,
            copyright,
            authorLinks
        ])
        stack.orientation = .vertical
        stack.alignment = .centerX
        stack.spacing = 8
        stack.setCustomSpacing(16, after: versionBadge)
        stack.setCustomSpacing(10, after: projectLinks)
        stack.setCustomSpacing(12, after: separator)
        stack.translatesAutoresizingMaskIntoConstraints = false
        background.addSubview(stack)
        NSLayoutConstraint.activate([
            stack.topAnchor.constraint(equalTo: background.topAnchor, constant: 25),
            stack.leadingAnchor.constraint(equalTo: background.leadingAnchor, constant: 42),
            stack.trailingAnchor.constraint(equalTo: background.trailingAnchor, constant: -42),
            stack.bottomAnchor.constraint(lessThanOrEqualTo: background.bottomAnchor, constant: -24),
            projectLinks.widthAnchor.constraint(equalTo: stack.widthAnchor),
            separator.widthAnchor.constraint(equalTo: stack.widthAnchor),
            authorLinks.widthAnchor.constraint(equalTo: stack.widthAnchor)
        ])

        panel.center()
        aboutWindow = panel
        NSApp.activate(ignoringOtherApps: true)
        panel.makeKeyAndOrderFront(nil)
    }

    private func aboutLabel(_ text: String, size: CGFloat, weight: NSFont.Weight, color: NSColor) -> NSTextField {
        let label = NSTextField(labelWithString: text)
        label.alignment = .center
        label.font = NSFont.systemFont(ofSize: size, weight: weight)
        label.textColor = color
        return label
    }

    private func aboutLinkButton(_ title: String, url: String, icon: NSImage? = nil) -> NSButton {
        let button = NSButton(title: title, target: self, action: #selector(openAboutLink(_:)))
        button.bezelStyle = .rounded
        button.controlSize = .large
        button.font = NSFont.systemFont(ofSize: 12, weight: .medium)
        button.identifier = NSUserInterfaceItemIdentifier(url)
        button.toolTip = url
        if let icon {
            icon.size = NSSize(width: 16, height: 16)
            button.image = icon
            button.imagePosition = .imageLeading
        }
        return button
    }

    @objc private func openAboutLink(_ sender: NSButton) {
        guard let value = sender.identifier?.rawValue, let url = URL(string: value) else {
            return
        }
        NSWorkspace.shared.open(url)
    }

    private func githubMarkImage() -> NSImage? {
        let svg = """
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
          <circle cx="12" cy="12" r="12" fill="#ffffff"/>
          <path fill="#181717" d="M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.1.79-.25.79-.56v-2.02c-3.2.7-3.88-1.36-3.88-1.36-.52-1.33-1.28-1.68-1.28-1.68-1.05-.72.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.77 2.7 1.26 3.36.96.1-.75.4-1.26.73-1.55-2.56-.29-5.25-1.28-5.25-5.69 0-1.26.45-2.29 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.16 1.18A10.9 10.9 0 0 1 12 6.16c.98 0 1.95.13 2.87.39 2.19-1.49 3.15-1.18 3.15-1.18.63 1.59.23 2.76.11 3.05.74.8 1.19 1.83 1.19 3.09 0 4.42-2.7 5.39-5.27 5.68.42.36.78 1.06.78 2.14v3.02c0 .31.21.67.79.56A11.51 11.51 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5Z"/>
        </svg>
        """
        return NSImage(data: Data(svg.utf8))
    }

    @objc private func openDeck() {
        let panel = NSOpenPanel()
        panel.title = "Open a Keynope Deck"
        panel.prompt = "Open"
        panel.allowedContentTypes = ["md", "markdown"].compactMap { UTType(filenameExtension: $0) }
        panel.allowsMultipleSelection = false
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.directoryURL = try? presentationsLibraryURL()
        guard panel.runModal() == .OK, let path = panel.url?.path else {
            return
        }
        replaceDeck(path: path)
    }

    @objc private func openRecentDeck(_ sender: NSMenuItem) {
        guard let path = sender.representedObject as? String else { return }
        replaceDeck(path: path)
    }

    private func refreshRecentDecksMenu() {
        guard let menu = recentDecksMenu else { return }
        menu.removeAllItems()
        let paths = recentDeckPathsHandler?() ?? []
        if paths.isEmpty {
            let item = NSMenuItem(title: "No Recent Presentations", action: nil, keyEquivalent: "")
            item.isEnabled = false
            menu.addItem(item)
            return
        }
        for path in paths {
            let url = URL(fileURLWithPath: path)
            let item = NSMenuItem(title: url.lastPathComponent, action: #selector(openRecentDeck(_:)), keyEquivalent: "")
            item.target = self
            item.representedObject = path
            item.toolTip = path
            menu.addItem(item)
        }
    }

    @objc private func showPresentationsLibrary() {
        do {
            if let deckURL = activeDeckURLHandler?() {
                NSWorkspace.shared.activateFileViewerSelecting([deckURL])
            } else {
                let library = try presentationsLibraryURL()
                NSWorkspace.shared.activateFileViewerSelecting([library])
            }
        } catch {
            let alert = NSAlert(error: error)
            alert.messageText = "Could Not Open Presentations Library"
            alert.runModal()
        }
    }

    private func presentationsLibraryURL() throws -> URL {
        let documents = try FileManager.default.url(
            for: .documentDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        )
        let library = documents.appendingPathComponent("Keynope Presentations", isDirectory: true)
        try FileManager.default.createDirectory(at: library, withIntermediateDirectories: true)
        return library
    }

    @objc private func newDeck() {
        guard let newURL = newDeckHandler?() else {
            return
        }
        noPresentation()
        presenterURL = newURL
        editorWebView?.load(URLRequest(url: editorURL()))
        updateEditorWindowTitle()
    }

    private func replaceDeck(path: String) {
        guard let newURL = openDeckHandler?(path) else {
            return
        }
        noPresentation()
        presenterURL = newURL
        editorWebView?.load(URLRequest(url: editorURL()))
        updateEditorWindowTitle(deckPath: path)
        showEditorWindow()
    }

    private var hasExternalDisplay: Bool {
        guard let main = NSScreen.main else {
            return NSScreen.screens.count > 1
        }
        return NSScreen.screens.contains { $0 != main }
    }

    private func updateDisplayState() {
        let externalDisplayAvailable = hasExternalDisplay
        if externalDisplayAvailable {
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
        publishExternalDisplayState(externalDisplayAvailable)
        publishPresentationState()
    }

    private func publishExternalDisplayState(_ available: Bool? = nil) {
        let value = available ?? hasExternalDisplay
        editorWebView?.evaluateJavaScript("window.keynopeSetExternalDisplayAvailable?.(\(value ? "true" : "false"))")
    }

    private func publishPresentationState() {
        editorWebView?.evaluateJavaScript(
            "window.keynopeSetPresentationState?.('\(presentationMode)', \(presentationPaused ? "true" : "false"))"
        )
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
        presentationMode = preferExternal ? "external" : "main"
        presentationPaused = false
        if remember {
            UserDefaults.standard.set(presentationMode, forKey: presentationModeKey)
        }
        reportPresentationMode()

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
        if preferExternal {
            newWindow.orderFrontRegardless()
            editorWindow?.makeKeyAndOrderFront(nil)
        } else {
            newWindow.makeKeyAndOrderFront(nil)
            newWindow.makeFirstResponder(view)
            NSApp.activate(ignoringOtherApps: true)
            newWindow.orderFrontRegardless()
        }

        window?.close()
        window = newWindow
        webView = view
        compositeView = composite
        screenShareController.previewView = composite.captureView
        updateDisplayState()
        refreshShareMenu()
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
        guard let url = components.url,
              let body = try? JSONSerialization.data(withJSONObject: [
                  "mode": presentationMode,
                  "paused": presentationPaused
              ]) else {
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

    private func pausePresentation() {
        guard presentationMode != "none", !presentationPaused else {
            return
        }
        presentationPaused = true
        reportPresentationMode()
        publishPresentationState()
    }

    private func resumePresentation() {
        guard presentationMode != "none", presentationPaused else {
            return
        }
        presentationPaused = false
        reportPresentationMode()
        publishPresentationState()
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
        presentationPaused = false
        if remember {
            UserDefaults.standard.set(presentationMode, forKey: presentationModeKey)
        }
        updatePresentationMenuState()
        refreshShareMenu()
        reportPresentationMode()
        publishPresentationState()
    }

    @objc private func screenParametersChanged() {
        updateDisplayState()
    }

    @objc private func quit() {
        NSApp.terminate(nil)
    }

    func menuNeedsUpdate(_ menu: NSMenu) {
        if menu === recentDecksMenu {
            refreshRecentDecksMenu()
            return
        }
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
            } else if action == "query-display-state" {
                publishExternalDisplayState()
                publishPresentationState()
            } else if action == "pause" {
                pausePresentation()
            } else if action == "resume" {
                resumePresentation()
            } else if action == "show-about" {
                showAbout()
            }
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return appMode
    }

    func windowWillClose(_ notification: Notification) {
        guard appMode,
              !terminatingAfterEditorClose,
              let closingWindow = notification.object as? NSWindow,
              closingWindow === editorWindow else {
            return
        }
        terminatingAfterEditorClose = true
        NSApp.terminate(nil)
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
    private static let defaultDeckColumns = 245
    private static let defaultDeckRows = 56
    private static let engineViewportColumns = 100
    private static let engineViewportRows = 32

    private struct DeckAuthorization {
        let path: String
        let helperBookmark: Data?
    }

    private struct RecentDeck: Codable {
        let path: String
        let lastOpened: TimeInterval
    }

    private static var delegate: PresenterDelegate?
    private static var parentMonitor: Timer?
    private static var engineProcess: Process?
    private static var engineInput: Pipe?
    private static var activeDeckPath: String?
    private static var activeSandboxURL: URL?
    private static let deckDirectoryBookmarkPrefix = "keynope.deck-directory."
    private static let deckFileBookmarkPrefix = "keynope.deck-file."
    private static let recentDecksKey = "keynope.recent-decks"
    private static let maximumRecentDecks = 12

    static func main() {
        let args = ProcessInfo.processInfo.arguments
        let appMode = args.contains("--app") || Bundle.main.bundleIdentifier == "sh.keynope.app"
        let url: URL
        if appMode {
            guard let engineURL = initialAppEngineURL(arguments: args) else {
                exit(0)
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
            newDeckHandler: appMode ? {
                guard let path = createNewDeck() else { return nil }
                return startEngine(deckPath: path)
            } : nil,
            activeDeckURLHandler: appMode ? {
                activeDeckPath.map { URL(fileURLWithPath: $0) }
            } : nil,
            recentDeckPathsHandler: appMode ? {
                recentDecks().map(\.path)
            } : nil,
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
        stopAccessingDeckDirectory()
    }

    private static func initialAppEngineURL(arguments: [String]) -> URL? {
        if let index = arguments.firstIndex(of: "--app"), index + 1 < arguments.count {
            return startEngine(deckPath: arguments[index + 1])
        }
        for recent in recentDecks() {
            if let engineURL = startEngine(deckPath: recent.path) {
                return engineURL
            }
        }
        do {
            return startEngine(deckPath: try welcomeDeckPath())
        } catch {
            showFileError(title: "Could Not Create Welcome Presentation", error: error)
            return nil
        }
    }

    private static func recentDecks() -> [RecentDeck] {
        guard let data = UserDefaults.standard.data(forKey: recentDecksKey),
              let records = try? JSONDecoder().decode([RecentDeck].self, from: data) else {
            return []
        }
        var seen = Set<String>()
        return records
            .sorted { $0.lastOpened > $1.lastOpened }
            .filter { seen.insert(URL(fileURLWithPath: $0.path).standardizedFileURL.path).inserted }
            .prefix(maximumRecentDecks)
            .map { $0 }
    }

    private static func recordRecentDeck(_ path: String) {
        let normalized = URL(fileURLWithPath: path).standardizedFileURL.path
        var records = recentDecks().filter { URL(fileURLWithPath: $0.path).standardizedFileURL.path != normalized }
        records.insert(RecentDeck(path: normalized, lastOpened: Date().timeIntervalSince1970), at: 0)
        records = Array(records.prefix(maximumRecentDecks))
        if let data = try? JSONEncoder().encode(records) {
            UserDefaults.standard.set(data, forKey: recentDecksKey)
        }
    }

    private static func welcomeDeckPath() throws -> String {
        let project = try presentationsLibraryURL().appendingPathComponent("Welcome", isDirectory: true)
        try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
        let destination = project.appendingPathComponent("Welcome.md")
        if !FileManager.default.fileExists(atPath: destination.path) {
            guard let source = Bundle.main.url(forResource: "Welcome", withExtension: "md") else {
                throw NSError(
                    domain: "sh.keynope.app",
                    code: 5,
                    userInfo: [NSLocalizedDescriptionKey: "The bundled welcome presentation is missing."]
                )
            }
            try FileManager.default.copyItem(at: source, to: destination)
        }
        return destination.path
    }

    private static func createNewDeck() -> String? {
        let alert = NSAlert()
        alert.messageText = "New Presentation"
        alert.informativeText = "Create a presentation in your Keynope library."
        alert.addButton(withTitle: "Create")
        alert.addButton(withTitle: "Cancel")
        let nameField = NSTextField(string: "Untitled")
        nameField.placeholderString = "Presentation name"
        nameField.frame = NSRect(x: 0, y: 0, width: 320, height: 24)
        alert.accessoryView = nameField
        alert.window.initialFirstResponder = nameField
        guard alert.runModal() == .alertFirstButtonReturn else {
            return nil
        }
        let requestedName = safePresentationName(nameField.stringValue)
        let starter = "<!-- keynope width=\(defaultDeckColumns) height=\(defaultDeckRows) -->\n\n# Title\n\nText\n"
        do {
            let library = try presentationsLibraryURL()
            let project = library.appendingPathComponent(requestedName, isDirectory: true)
            if FileManager.default.fileExists(atPath: project.path) {
                let replace = NSAlert()
                replace.alertStyle = .warning
                replace.messageText = "“\(requestedName)” already exists"
                replace.informativeText = "Replacing it removes the existing presentation and assets in its Keynope project folder."
                replace.addButton(withTitle: "Replace")
                replace.addButton(withTitle: "Cancel")
                guard replace.runModal() == .alertFirstButtonReturn else { return nil }
                try FileManager.default.removeItem(at: project)
            }
            try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
            let url = project.appendingPathComponent(requestedName).appendingPathExtension("md")
            try Data(starter.utf8).write(to: url, options: .atomic)
            return url.path
        } catch {
            showFileError(title: "Could Not Create Presentation", error: error)
            return nil
        }
    }

    private static func startEngine(deckPath: String) -> URL? {
        let authorization: DeckAuthorization
        do {
            authorization = try authorizeDeck(for: deckPath)
        } catch {
            if (error as? CocoaError)?.code != .userCancelled {
                showFileError(title: "Could Not Access Presentation", error: error)
            }
            return nil
        }
        let previousProcess = engineProcess
        let previousInput = engineInput
        let engineURL = Bundle.main.bundleURL
            .appendingPathComponent("Contents/Helpers/keynope-engine")
        let process = Process()
        process.executableURL = engineURL
        process.arguments = ["--app", authorization.path]
        var environment = ProcessInfo.processInfo.environment
        environment["COLUMNS"] = String(engineViewportColumns)
        environment["LINES"] = String(engineViewportRows)
        if let bookmark = authorization.helperBookmark {
            environment["KEYNOPE_SANDBOX_BOOKMARK"] = bookmark.base64EncodedString()
        } else {
            environment.removeValue(forKey: "KEYNOPE_SANDBOX_BOOKMARK")
        }
        process.environment = environment
        let output = Pipe()
        let errors = Pipe()
        let input = Pipe()
        process.standardOutput = output
        process.standardError = errors
        process.standardInput = input
        do {
            try process.run()
        } catch {
            showFileError(title: "Could Not Start Presentation", error: error)
            return nil
        }
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
                    engineProcess = process
                    engineInput = input
                    activeDeckPath = authorization.path
                    recordRecentDeck(authorization.path)
                    if let previousProcess, previousProcess.isRunning {
                        try? previousInput?.fileHandleForWriting.close()
                        previousProcess.terminate()
                    }
                    return url
                }
            }
        }
        if process.isRunning { process.terminate() }
        let errorData = errors.fileHandleForReading.readDataToEndOfFile()
        let detail = String(data: errorData, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines)
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Could Not Open Presentation"
        alert.informativeText = detail?.isEmpty == false ? detail! : "The presentation engine did not start."
        alert.runModal()
        return nil
    }

    private static var isSandboxed: Bool {
        ProcessInfo.processInfo.environment["APP_SANDBOX_CONTAINER_ID"] != nil
    }

    private static func authorizeDeck(for deckPath: String) throws -> DeckAuthorization {
        guard isSandboxed else {
            return DeckAuthorization(path: deckPath, helperBookmark: nil)
        }
        let deckURL = URL(fileURLWithPath: deckPath).standardizedFileURL
        if try contains(deckURL, in: presentationsLibraryURL()) || contains(deckURL, in: downloadsDirectoryURL()) {
            stopAccessingDeckDirectory()
            return DeckAuthorization(path: deckURL.path, helperBookmark: nil)
        }
        let directory = deckURL.deletingLastPathComponent().standardizedFileURL
        let directoryBookmarkKey = deckDirectoryBookmarkPrefix + bookmarkKeySuffix(for: directory)
        if try restoredBookmark(for: directory, key: directoryBookmarkKey) != nil {
            return DeckAuthorization(path: deckURL.path, helperBookmark: try bookmarkForEngineHelper())
        }
        _ = try authorizeDeckFile(deckURL)
        let assets = try localAssetURLs(in: deckURL)
        if !assets.isEmpty {
            switch externalAssetsChoice(for: deckURL) {
            case .alertFirstButtonReturn:
                let selected = try selectDeckDirectory(deckURL)
                stopAccessingDeckDirectory()
                activeSandboxURL = selected
                let imported = try importPresentation(deckURL, assets: assets)
                stopAccessingDeckDirectory()
                return DeckAuthorization(path: imported.path, helperBookmark: nil)
            case .alertSecondButtonReturn:
                _ = try authorizeDeckDirectory(deckURL)
                return DeckAuthorization(path: deckURL.path, helperBookmark: try bookmarkForEngineHelper())
            default:
                throw CocoaError(.userCancelled)
            }
        }
        return DeckAuthorization(path: deckURL.path, helperBookmark: try bookmarkForEngineHelper())
    }

    // Persistent security-scoped bookmarks are tied to this app's signing identity.
    // The engine is a separately signed process, so hand it an ephemeral bookmark
    // created for inter-process sharing while this app actively holds access.
    private static func bookmarkForEngineHelper() throws -> Data {
        guard let activeSandboxURL else {
            throw NSError(
                domain: "sh.keynope.app",
                code: 4,
                userInfo: [NSLocalizedDescriptionKey: "Presentation access was not active."]
            )
        }
        return try activeSandboxURL.bookmarkData(
            options: [],
            includingResourceValuesForKeys: nil,
            relativeTo: nil
        )
    }

    private static func authorizeDeckFile(_ deckURL: URL) throws -> Data {
        let bookmarkKey = deckFileBookmarkPrefix + bookmarkKeySuffix(for: deckURL)
        if let activeSandboxURL, activeSandboxURL.standardizedFileURL == deckURL.standardizedFileURL {
            return try activeSandboxURL.bookmarkData(options: .withSecurityScope, includingResourceValuesForKeys: nil, relativeTo: nil)
        }
        if let bookmark = try restoredBookmark(for: deckURL, key: bookmarkKey) {
            return bookmark
        }
        let selectedURL: URL
        if (try? Data(contentsOf: deckURL)) != nil {
            selectedURL = deckURL
        } else {
            let panel = NSOpenPanel()
            panel.title = "Open a Keynope Deck"
            panel.message = "Select “\(deckURL.lastPathComponent)” to allow Keynope to open it."
            panel.prompt = "Open"
            panel.directoryURL = deckURL.deletingLastPathComponent()
            panel.allowedContentTypes = ["md", "markdown"].compactMap { UTType(filenameExtension: $0) }
            panel.canChooseFiles = true
            panel.canChooseDirectories = false
            panel.allowsMultipleSelection = false
            guard panel.runModal() == .OK, let selected = panel.url?.standardizedFileURL else {
                throw CocoaError(.userCancelled)
            }
            guard selected == deckURL.standardizedFileURL else {
                throw NSError(
                    domain: "sh.keynope.app",
                    code: 3,
                    userInfo: [NSLocalizedDescriptionKey: "Select \(deckURL.lastPathComponent)."]
                )
            }
            selectedURL = selected
        }
        let bookmark = try selectedURL.bookmarkData(options: .withSecurityScope, includingResourceValuesForKeys: nil, relativeTo: nil)
        UserDefaults.standard.set(bookmark, forKey: bookmarkKey)
        stopAccessingDeckDirectory()
        activeSandboxURL = selectedURL
        return bookmark
    }

    private static func authorizeDeckDirectory(_ deckURL: URL) throws -> Data {
        let directory = deckURL.deletingLastPathComponent().standardizedFileURL
        if let activeSandboxURL, activeSandboxURL.standardizedFileURL == directory {
            return try activeSandboxURL.bookmarkData(options: .withSecurityScope, includingResourceValuesForKeys: nil, relativeTo: nil)
        }
        let bookmarkKey = deckDirectoryBookmarkPrefix + Data(directory.path.utf8).base64EncodedString()
        if let bookmark = try restoredBookmark(for: directory, key: bookmarkKey) {
            return bookmark
        }

        let selected = try selectDeckDirectory(deckURL)
        let saved = try selected.bookmarkData(options: .withSecurityScope, includingResourceValuesForKeys: nil, relativeTo: nil)
        UserDefaults.standard.set(saved, forKey: bookmarkKey)
        stopAccessingDeckDirectory()
        activeSandboxURL = selected
        return saved
    }

    private static func selectDeckDirectory(_ deckURL: URL) throws -> URL {
        let directory = deckURL.deletingLastPathComponent().standardizedFileURL
        let panel = NSOpenPanel()
        panel.title = "Allow Presentation Folder Access"
        panel.message = "Select “\(directory.lastPathComponent)” to let Keynope use this presentation and its assets."
        panel.prompt = "Allow Access"
        panel.directoryURL = directory.deletingLastPathComponent()
        panel.canChooseFiles = false
        panel.canChooseDirectories = true
        panel.allowsMultipleSelection = false
        guard panel.runModal() == .OK, let selected = panel.url?.standardizedFileURL else {
            throw CocoaError(.userCancelled)
        }
        guard selected == directory else {
            throw NSError(
                domain: "sh.keynope.app",
                code: 1,
                userInfo: [NSLocalizedDescriptionKey: "Select the folder containing \(deckURL.lastPathComponent)."]
            )
        }
        return selected
    }

    private static func restoredBookmark(for expectedURL: URL, key: String) throws -> Data? {
        guard let saved = UserDefaults.standard.data(forKey: key) else { return nil }
        do {
            var stale = false
            let resolved = try URL(
                resolvingBookmarkData: saved,
                options: [.withSecurityScope, .withoutUI],
                relativeTo: nil,
                bookmarkDataIsStale: &stale
            )
            guard resolved.standardizedFileURL == expectedURL.standardizedFileURL,
                  resolved.startAccessingSecurityScopedResource() else {
                UserDefaults.standard.removeObject(forKey: key)
                return nil
            }
            stopAccessingDeckDirectory()
            activeSandboxURL = resolved
            let bookmark = stale
                ? try resolved.bookmarkData(options: .withSecurityScope, includingResourceValuesForKeys: nil, relativeTo: nil)
                : saved
            if stale { UserDefaults.standard.set(bookmark, forKey: key) }
            return bookmark
        } catch {
            UserDefaults.standard.removeObject(forKey: key)
            return nil
        }
    }

    private static func presentationsLibraryURL() throws -> URL {
        let documents = try FileManager.default.url(
            for: .documentDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        )
        let library = documents.appendingPathComponent("Keynope Presentations", isDirectory: true)
        try FileManager.default.createDirectory(at: library, withIntermediateDirectories: true)
        return library
    }

    private static func downloadsDirectoryURL() -> URL {
        FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask)[0]
    }

    private static func contains(_ child: URL, in parent: URL) -> Bool {
        let childPath = child.resolvingSymlinksInPath().standardizedFileURL.path
        let parentPath = parent.resolvingSymlinksInPath().standardizedFileURL.path
        return childPath == parentPath || childPath.hasPrefix(parentPath + "/")
    }

    private static func bookmarkKeySuffix(for url: URL) -> String {
        Data(url.standardizedFileURL.path.utf8).base64EncodedString()
    }

    private static func safePresentationName(_ input: String) -> String {
        let forbidden = CharacterSet(charactersIn: "/:")
        let cleaned = input.components(separatedBy: forbidden).joined(separator: "-").trimmingCharacters(in: .whitespacesAndNewlines)
        return cleaned.isEmpty ? "Untitled" : cleaned
    }

    private static func uniqueProjectURL(named name: String, in library: URL) -> URL {
        var candidate = library.appendingPathComponent(name, isDirectory: true)
        var suffix = 2
        while FileManager.default.fileExists(atPath: candidate.path) {
            candidate = library.appendingPathComponent("\(name) \(suffix)", isDirectory: true)
            suffix += 1
        }
        return candidate
    }

    private static func externalAssetsChoice(for deckURL: URL) -> NSApplication.ModalResponse {
        let alert = NSAlert()
        alert.messageText = "This presentation uses local assets"
        alert.informativeText = "Import the presentation into Keynope, or allow access to “\(deckURL.deletingLastPathComponent().lastPathComponent)”."
        alert.addButton(withTitle: "Import into Keynope")
        alert.addButton(withTitle: "Allow Folder")
        alert.addButton(withTitle: "Cancel")
        return alert.runModal()
    }

    private static func localAssetURLs(in deckURL: URL) throws -> [URL] {
        let data = try Data(contentsOf: deckURL)
        guard let text = String(data: data, encoding: .utf8) else { return [] }
        let base = deckURL.deletingLastPathComponent()
        var paths: [String] = []
        let imagePattern = try NSRegularExpression(pattern: #"!\[[^\]]*\]\(([^)]+)\)"#)
        let range = NSRange(text.startIndex..., in: text)
        for match in imagePattern.matches(in: text, range: range) {
            guard let sourceRange = Range(match.range(at: 1), in: text) else { continue }
            paths.append(String(text[sourceRange]).components(separatedBy: "?")[0])
        }
        let masterPattern = try NSRegularExpression(pattern: #"keynope-masters\s+version=[0-9]+\s+base64:([A-Za-z0-9+/=]+)"#)
        for match in masterPattern.matches(in: text, range: range) {
            guard let payloadRange = Range(match.range(at: 1), in: text),
                  let payload = Data(base64Encoded: String(text[payloadRange])),
                  let object = try? JSONSerialization.jsonObject(with: payload) else { continue }
            collectAssetPaths(from: object, into: &paths)
        }
        var seen = Set<String>()
        return paths.compactMap { raw in
            guard !raw.isEmpty, !raw.contains("://") else { return nil }
            let expanded = NSString(string: raw).expandingTildeInPath
            let url = expanded.hasPrefix("/")
                ? URL(fileURLWithPath: expanded)
                : base.appendingPathComponent(expanded)
            let normalized = url.standardizedFileURL
            guard FileManager.default.fileExists(atPath: normalized.path) else { return nil }
            guard seen.insert(normalized.path).inserted else { return nil }
            return normalized
        }
    }

    private static func collectAssetPaths(from object: Any, into paths: inout [String]) {
        if let dictionary = object as? [String: Any] {
            if let path = dictionary["path"] as? String { paths.append(path) }
            for value in dictionary.values { collectAssetPaths(from: value, into: &paths) }
        } else if let array = object as? [Any] {
            for value in array { collectAssetPaths(from: value, into: &paths) }
        }
    }

    private static func importPresentation(_ deckURL: URL, assets: [URL]) throws -> URL {
        let sourceDirectory = deckURL.deletingLastPathComponent().standardizedFileURL
        let library = try presentationsLibraryURL()
        let name = safePresentationName(deckURL.deletingPathExtension().lastPathComponent)
        let project = uniqueProjectURL(named: name, in: library)
        try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
        do {
            let importedDeck = project.appendingPathComponent(deckURL.lastPathComponent)
            try FileManager.default.copyItem(at: deckURL, to: importedDeck)
            for asset in assets {
                guard contains(asset, in: sourceDirectory) else {
                    throw NSError(
                        domain: "sh.keynope.app",
                        code: 2,
                        userInfo: [NSLocalizedDescriptionKey: "The asset \(asset.lastPathComponent) is outside the presentation folder and cannot be imported automatically."]
                    )
                }
                let relative = String(asset.path.dropFirst(sourceDirectory.path.count)).trimmingCharacters(in: CharacterSet(charactersIn: "/"))
                let destination = project.appendingPathComponent(relative)
                try FileManager.default.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true)
                if !FileManager.default.fileExists(atPath: destination.path) {
                    try FileManager.default.copyItem(at: asset, to: destination)
                }
            }
            return importedDeck
        } catch {
            try? FileManager.default.removeItem(at: project)
            throw error
        }
    }

    private static func stopAccessingDeckDirectory() {
        activeSandboxURL?.stopAccessingSecurityScopedResource()
        activeSandboxURL = nil
    }

    private static func showFileError(title: String, error: Error) {
        let alert = NSAlert(error: error)
        alert.alertStyle = .warning
        alert.messageText = title
        alert.runModal()
    }
}
