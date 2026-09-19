@preconcurrency import Foundation
import Cocoa
import Darwin
@preconcurrency import ScreenCaptureKit
import UniformTypeIdentifiers
import WebKit
import PDFKit

final class PresenterWindow: NSWindow {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { true }
}

@MainActor
final class PresenterDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, WKScriptMessageHandler, NSMenuDelegate, WKUIDelegate {
    private var presenterURL: URL
    private let appMode: Bool
    private let openDeckHandler: ((String) -> URL?)?
    private let selectedDeckURLHandler: ((URL) throws -> Void)?
    private let newDeckHandler: (() -> URL?)?
    private let activeDeckURLHandler: (() -> URL?)?
    private let recentDeckPathsHandler: (() -> [String])?
    private let didSaveDeckHandler: ((String) -> Void)?
    private let inputHandler: ((Data) -> Void)?
    private var statusItem: NSStatusItem?
    private var editorWindow: NSWindow?
    private var editorWebView: WKWebView?
    private var aboutWindow: NSWindow?
    private var licensesWindow: NSWindow?
    private var window: NSWindow?
    private var webView: WKWebView?
    private var compositeView: PresenterCompositeView?
    private var noPresentationItem: NSMenuItem?
    private var externalDisplayItem: NSMenuItem?
    private var mainDisplayItem: NSMenuItem?
    private var shareRootItem: NSMenuItem?
    private var shareMenu: NSMenu?
    private var recentDecksMenu: NSMenu?
    private var saveMenuItem: NSMenuItem?
    private var participantTabsMenuItem: NSMenuItem?
    private var activityQRMenuItem: NSMenuItem?
    private var shareableContent: SCShareableContent?
    private var loadingShareSources = false
    private let screenShareController = ScreenShareController()
    private var presentationMode: String = "none"
    private var presentationPaused = false
    private var presentationDisplayID: CGDirectDisplayID?
    private var participantPresentationTimer: Timer?
    private var presentationPreparationGeneration = 0
    private var terminatingAfterEditorClose = false
    private var documentDirty = true
    private var savingDocument = false
    private var exportingSlides = false
    private var slideExportTask: Task<Void, Never>?
    private var closeAfterSave = false
    private var terminateAfterSave = false
    private var discardingUnsavedChanges = false
    private var actionAfterSave: (() -> Void)?
    private let presentationModeKey = "keynope.presenter.mode"
#if KEYNOPE_NATIVE_ACCEPTANCE
    private struct NativePanelAcceptanceStep {
        let title: String
        let destination: URL?
        let response: NSApplication.ModalResponse
    }
    private struct NativeAcceptanceEditorState: Decodable {
        struct Diagnostic: Decodable { let code: String }
        let diagnostics: [Diagnostic]?
    }
    private struct NativeAcceptancePresenterState: Decodable {
        let slide: Int
        let page: Int
        let presenting: Bool
        let timerMode: String?
        let timerEndMs: Int64?
    }
    private var nativePanelAcceptanceStep: NativePanelAcceptanceStep?
    private var nativePanelAcceptanceFailure: String?
#endif

    init(
        url: URL,
        appMode: Bool = false,
        openDeckHandler: ((String) -> URL?)? = nil,
        selectedDeckURLHandler: ((URL) throws -> Void)? = nil,
        newDeckHandler: (() -> URL?)? = nil,
        activeDeckURLHandler: (() -> URL?)? = nil,
        recentDeckPathsHandler: (() -> [String])? = nil,
        didSaveDeckHandler: ((String) -> Void)? = nil,
        inputHandler: ((Data) -> Void)? = nil
    ) {
        self.presenterURL = url
        self.appMode = appMode
        self.openDeckHandler = openDeckHandler
        self.selectedDeckURLHandler = selectedDeckURLHandler
        self.newDeckHandler = newDeckHandler
        self.activeDeckURLHandler = activeDeckURLHandler
        self.recentDeckPathsHandler = recentDeckPathsHandler
        self.didSaveDeckHandler = didSaveDeckHandler
        self.inputHandler = inputHandler
        super.init()
    }

    private func updateParticipantPublicationTimer() {
        guard presentationMode != "none" else {
            participantPresentationTimer?.invalidate()
            participantPresentationTimer = nil
            return
        }
        guard participantPresentationTimer == nil else { return }
        participantPresentationTimer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self, self.presentationMode != "none" else { return }
                self.editorWebView?.evaluateJavaScript("window.keynopePublishParticipantPage?.()")
            }
        }
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
#if KEYNOPE_NATIVE_ACCEPTANCE
            if let root = ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_ACCEPTANCE_ROOT"], !root.isEmpty {
                Task { await runNativePanelAcceptance(root: URL(fileURLWithPath: root, isDirectory: true)) }
            }
#endif
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
        item.button?.setAccessibilityLabel("Keynope Presenter")
        item.button?.setAccessibilityHelp("Open presentation controls")
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
        let settingsItem = NSMenuItem(title: "Settings", action: nil, keyEquivalent: "")
        let settingsMenu = NSMenu(title: "Settings")
        settingsMenu.autoenablesItems = false
        let tabsItem = settingsMenu.addItem(withTitle: "Tabs…", action: #selector(showParticipantTabs), keyEquivalent: "")
        tabsItem.target = self
        tabsItem.isEnabled = false
        participantTabsMenuItem = tabsItem
        let qrItem = settingsMenu.addItem(withTitle: "Activity QR Codes", action: #selector(toggleActivityQR), keyEquivalent: "")
        qrItem.target = self
        qrItem.isEnabled = false
        qrItem.state = .on
        activityQRMenuItem = qrItem
        settingsItem.submenu = settingsMenu
        applicationMenu.addItem(settingsItem)
        applicationMenu.addItem(NSMenuItem.separator())
        let servicesItem = NSMenuItem(title: "Services", action: nil, keyEquivalent: "")
        let servicesMenu = NSMenu(title: "Services")
        servicesItem.submenu = servicesMenu
        applicationMenu.addItem(servicesItem)
        NSApp.servicesMenu = servicesMenu
        applicationMenu.addItem(NSMenuItem.separator())
        applicationMenu.addItem(withTitle: "Hide Keynope", action: #selector(NSApplication.hide(_:)), keyEquivalent: "h")
        let hideOthers = applicationMenu.addItem(withTitle: "Hide Others", action: #selector(NSApplication.hideOtherApplications(_:)), keyEquivalent: "h")
        hideOthers.keyEquivalentModifierMask = [.command, .option]
        applicationMenu.addItem(withTitle: "Show All", action: #selector(NSApplication.unhideAllApplications(_:)), keyEquivalent: "")
        applicationMenu.addItem(NSMenuItem.separator())
        applicationMenu.addItem(withTitle: "Quit Keynope", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        applicationItem.submenu = applicationMenu
        mainMenu.addItem(applicationItem)

        let fileItem = NSMenuItem()
        let fileMenu = NSMenu(title: "File")
        let newItem = fileMenu.addItem(withTitle: "New Presentation", action: #selector(newDeck), keyEquivalent: "n")
        newItem.target = self
        let openItem = fileMenu.addItem(withTitle: "Open…", action: #selector(openDeck), keyEquivalent: "o")
        openItem.target = self
        let saveItem = fileMenu.addItem(withTitle: "Save", action: #selector(saveDeck), keyEquivalent: "s")
        saveItem.target = self
        saveItem.isEnabled = documentDirty
        saveMenuItem = saveItem
        let saveAsItem = fileMenu.addItem(withTitle: "Save As…", action: #selector(saveDeckAs), keyEquivalent: "S")
        saveAsItem.keyEquivalentModifierMask = [.command, .shift]
        saveAsItem.target = self
        let saveCopyItem = fileMenu.addItem(withTitle: "Save a Copy…", action: #selector(saveDeckCopy), keyEquivalent: "")
        saveCopyItem.target = self
        let pdfItem = fileMenu.addItem(withTitle: "Export Presentation as PDF…", action: #selector(exportPDF), keyEquivalent: "")
        pdfItem.target = self
        let pngItem = fileMenu.addItem(withTitle: "Export Slide as PNG…", action: #selector(exportPNG), keyEquivalent: "")
        pngItem.target = self
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
        windowMenu.addItem(withTitle: "Minimize", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "Zoom", action: #selector(NSWindow.performZoom(_:)), keyEquivalent: "")
        windowMenu.addItem(NSMenuItem.separator())
        let showItem = windowMenu.addItem(withTitle: "Show Keynope", action: #selector(showEditor), keyEquivalent: "0")
        showItem.target = self
        windowMenu.addItem(NSMenuItem.separator())
        windowMenu.addItem(withTitle: "Bring All to Front", action: #selector(NSApplication.arrangeInFront(_:)), keyEquivalent: "")
        windowItem.submenu = windowMenu
        mainMenu.addItem(windowItem)
        NSApp.mainMenu = mainMenu
        NSApp.windowsMenu = windowMenu
    }

    private func editorURL() -> URL {
        presenterURLForSurface("app")
    }

    @objc private func showParticipantTabs() {
        guard participantTabsMenuItem?.isEnabled == true else { return }
        showEditor()
        editorWebView?.evaluateJavaScript("window.keynopeOpenParticipantTabs?.()")
    }

    @objc private func toggleActivityQR() {
        guard activityQRMenuItem?.isEnabled == true else { return }
        editorWebView?.evaluateJavaScript("window.keynopeToggleActivityQR?.()")
    }

    private var versionedAppName: String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String
        guard let version, !version.isEmpty else { return "Keynope" }
        return "Keynope \(version)"
    }

    private func updateEditorWindowTitle(deckPath: String? = nil) {
        let path = deckPath ?? activeDeckURLHandler?()?.path
        let dirtySuffix = documentDirty ? " *" : ""
        guard let path, !path.isEmpty else {
            editorWindow?.title = "\(versionedAppName) — Untitled\(dirtySuffix)"
            return
        }
        editorWindow?.title = "\(versionedAppName) — \(URL(fileURLWithPath: path).lastPathComponent)\(dirtySuffix)"
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
        if ProcessInfo.processInfo.environment["KEYNOPE_PERFORMANCE"] == "1" {
            config.userContentController.addUserScript(WKUserScript(
                source: "window.KEYNOPE_PERFORMANCE = true;",
                injectionTime: .atDocumentStart,
                forMainFrameOnly: true
            ))
        }
        let visibleFrame = NSScreen.main?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1440, height: 900)
        let windowSize = NSSize(
            width: min(1440, max(1100, visibleFrame.width * 0.92)),
            height: min(960, max(720, visibleFrame.height * 0.92))
        )
        let initialFrame = NSRect(origin: .zero, size: windowSize)
        let view = WKWebView(frame: initialFrame, configuration: config)
        view.uiDelegate = self
        view.autoresizingMask = [.width, .height]
        view.setAccessibilityLabel("Keynope presentation editor")
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

    #if compiler(>=6.0)
    func webView(
        _ webView: WKWebView,
        runOpenPanelWith parameters: WKOpenPanelParameters,
        initiatedByFrame frame: WKFrameInfo,
        completionHandler: @escaping @MainActor @Sendable ([URL]?) -> Void
    ) {
        presentImageOpenPanel(in: webView, parameters: parameters, completionHandler: completionHandler)
    }
    #else
    nonisolated func webView(
        _ webView: WKWebView,
        runOpenPanelWith parameters: WKOpenPanelParameters,
        initiatedByFrame frame: WKFrameInfo,
        completionHandler: @escaping ([URL]?) -> Void
    ) {
        MainActor.assumeIsolated {
            presentImageOpenPanel(in: webView, parameters: parameters, completionHandler: completionHandler)
        }
    }
    #endif

    private func presentImageOpenPanel(
        in webView: WKWebView,
        parameters: WKOpenPanelParameters,
        completionHandler: @escaping ([URL]?) -> Void
    ) {
        let panel = NSOpenPanel()
        panel.title = "Import a Keynope Asset"
        panel.prompt = "Import"
        panel.allowedContentTypes = [
            .image,
            UTType(filenameExtension: "json"),
            UTType(filenameExtension: "flf"),
            UTType(filenameExtension: "tlf")
        ].compactMap { $0 }
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
            contentRect: NSRect(x: 0, y: 0, width: 520, height: 478),
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

        let licensesButton = NSButton(title: "Licenses", target: self, action: #selector(showLicenses))
        licensesButton.bezelStyle = .rounded
        licensesButton.controlSize = .large
        licensesButton.font = NSFont.systemFont(ofSize: 12, weight: .medium)
        licensesButton.toolTip = "View Keynope and third-party licenses"

        let stack = NSStackView(views: [
            iconView,
            nameLabel,
            versionBadge,
            projectTitle,
            projectLinks,
            separator,
            copyright,
            authorLinks,
            licensesButton
        ])
        stack.orientation = .vertical
        stack.alignment = .centerX
        stack.spacing = 8
        stack.setCustomSpacing(16, after: versionBadge)
        stack.setCustomSpacing(10, after: projectLinks)
        stack.setCustomSpacing(12, after: separator)
        stack.setCustomSpacing(10, after: authorLinks)
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

    @objc private func showLicenses() {
        if let licensesWindow {
            NSApp.activate(ignoringOtherApps: true)
            licensesWindow.makeKeyAndOrderFront(nil)
            return
        }

        let panel = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 720, height: 590),
            styleMask: [.titled, .closable, .resizable],
            backing: .buffered,
            defer: false
        )
        panel.title = "Keynope Licenses"
        panel.isReleasedWhenClosed = false
        panel.minSize = NSSize(width: 520, height: 360)

        let textView = NSTextView(frame: NSRect(x: 0, y: 0, width: 720, height: 590))
        textView.isEditable = false
        textView.isSelectable = true
        textView.isRichText = false
        textView.font = NSFont.monospacedSystemFont(ofSize: 12, weight: .regular)
        textView.textColor = .labelColor
        textView.backgroundColor = .textBackgroundColor
        textView.textContainerInset = NSSize(width: 18, height: 18)
        textView.minSize = .zero
        textView.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        textView.isVerticallyResizable = true
        textView.isHorizontallyResizable = false
        textView.autoresizingMask = [.width]
        textView.textContainer?.containerSize = NSSize(width: 720, height: CGFloat.greatestFiniteMagnitude)
        textView.textContainer?.widthTracksTextView = true
        textView.string = bundledLicenseDocuments()

        let scrollView = NSScrollView(frame: .zero)
        scrollView.drawsBackground = true
        scrollView.hasVerticalScroller = true
        scrollView.hasHorizontalScroller = false
        scrollView.autohidesScrollers = true
        scrollView.documentView = textView
        scrollView.translatesAutoresizingMaskIntoConstraints = false

        let content = NSView()
        content.addSubview(scrollView)
        NSLayoutConstraint.activate([
            scrollView.topAnchor.constraint(equalTo: content.topAnchor),
            scrollView.leadingAnchor.constraint(equalTo: content.leadingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: content.trailingAnchor),
            scrollView.bottomAnchor.constraint(equalTo: content.bottomAnchor)
        ])
        panel.contentView = content
        panel.center()
        licensesWindow = panel
        NSApp.activate(ignoringOtherApps: true)
        panel.makeKeyAndOrderFront(nil)
    }

    private func bundledLicenseDocuments() -> String {
        let documents = [
            ("KEYNOPE", "Keynope-LICENSE"),
            ("KEYNOPE EMOJI GLYPHS NOTICE", "NOTICE"),
            ("SIL OPEN FONT LICENSE 1.1", "OFL"),
            ("GO FONT FAMILY", "GO-FONT-LICENSE"),
            ("NOTO REGIONAL FLAGS", "NOTO-REGION-FLAGS-LICENSE"),
            ("UNICODE DATA FILES", "UNICODE-LICENSE")
        ]
        guard let directory = Bundle.main.resourceURL?.appendingPathComponent("EmojiLicenses", isDirectory: true) else {
            return "License documents are unavailable."
        }
        var sections: [String] = []
        for (title, name) in documents {
            let url = directory.appendingPathComponent(name).appendingPathExtension("txt")
            guard let text = try? String(contentsOf: url, encoding: .utf8) else { continue }
            sections.append("===== \(title) =====\n\n\(text.trimmingCharacters(in: .whitespacesAndNewlines))")
        }
        return sections.isEmpty ? "License documents are unavailable." : sections.joined(separator: "\n\n") + "\n"
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
        guard runDocumentPanel(panel) == .OK, let url = panel.url?.standardizedFileURL else {
            return
        }
        _ = requestDocumentReplacement { [weak self] in
            self?.openSelectedDeck(url)
        }
    }

    @objc private func openRecentDeck(_ sender: NSMenuItem) {
        guard let path = sender.representedObject as? String else { return }
        _ = requestDocumentReplacement { [weak self] in
            self?.replaceDeck(path: path)
        }
    }

    private func openSelectedDeck(_ url: URL) {
        do {
            try selectedDeckURLHandler?(url)
            replaceDeck(path: url.path)
        } catch {
            let alert = NSAlert(error: error)
            alert.messageText = "Could Not Open Presentation"
            alert.runModal()
        }
    }

    @discardableResult
    private func requestDocumentReplacement(_ replacement: @escaping () -> Void) -> Bool {
        guard !savingDocument else { return false }
        guard documentDirty else {
            replacement()
            return true
        }
        switch unsavedChangesAlert().runModal() {
        case .alertFirstButtonReturn:
            actionAfterSave = replacement
            saveDocument(forceSaveAs: false)
            return true
        case .alertSecondButtonReturn:
            replacement()
            return true
        default:
            return false
        }
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
        _ = requestDocumentReplacement { [weak self] in
            self?.openNewDeck()
        }
    }

    private func openNewDeck() {
        guard let newURL = newDeckHandler?() else {
            return
        }
        noPresentation()
        presenterURL = newURL
        editorWebView?.load(URLRequest(url: editorURL()))
        participantTabsMenuItem?.isEnabled = false
        activityQRMenuItem?.isEnabled = false
        documentDirty = true
        saveMenuItem?.isEnabled = true
        updateEditorWindowTitle()
    }

    private struct SaveDocumentResponse: Decodable {
        let content: String
        let version: Int
    }

    private struct ExportDocumentResponse: Decodable {
        let content: String
    }

    private func editorEndpoint(_ path: String) -> URL? {
        var components = URLComponents(url: presenterURL, resolvingAgainstBaseURL: false)
        components?.path = path
        return components?.url
    }

    @objc private func saveDeck() {
        saveDocument(forceSaveAs: false)
    }

    @objc private func saveDeckAs() {
        saveDocument(forceSaveAs: true)
    }

    @objc private func saveDeckCopy() {
        saveDocument(forceSaveAs: true, copyOnly: true)
    }

    @objc private func exportPDF() { exportSlides(pdf: true) }
    @objc private func exportPNG() { exportSlides(pdf: false) }

    private func configureDocumentDestinationPanel(
        _ panel: NSSavePanel,
        extension fileExtension: String,
        copySuffix: Bool = false
    ) {
        if let deck = activeDeckURLHandler?() {
            let base = deck.deletingPathExtension().lastPathComponent
            panel.directoryURL = deck.deletingLastPathComponent()
            panel.nameFieldStringValue = base + (copySuffix ? " copy" : "") + "." + fileExtension
        } else {
            panel.directoryURL = try? presentationsLibraryURL()
            panel.nameFieldStringValue = "Untitled" + (copySuffix ? " copy" : "") + "." + fileExtension
        }
    }

    private func runDocumentPanel(_ panel: NSSavePanel) -> NSApplication.ModalResponse {
#if KEYNOPE_NATIVE_ACCEPTANCE
        if let step = nativePanelAcceptanceStep {
            nativePanelAcceptanceStep = nil
            if panel.title != step.title {
                nativePanelAcceptanceFailure = "Expected panel ‘\(step.title)’ but Keynope presented ‘\(panel.title ?? "")’"
                return .cancel
            }
            if let destination = step.destination {
                if panel is NSOpenPanel {
                    panel.directoryURL = destination
                } else {
                    panel.directoryURL = destination.deletingLastPathComponent()
                    panel.nameFieldStringValue = destination.lastPathComponent
                }
            }
            let timer = Timer(timeInterval: 0.15, repeats: false) { _ in
                MainActor.assumeIsolated {
                    NSApp.stopModal(withCode: step.response)
                    panel.orderOut(nil)
                }
            }
            RunLoop.main.add(timer, forMode: .common)
            return panel.runModal()
        }
#endif
        return panel.runModal()
    }

    private func exportSlides(pdf: Bool) {
        guard !exportingSlides, let endpoint = editorEndpoint("/api/editor/export-document") else { return }
        exportingSlides = true
        slideExportTask = Task {
            let renderer = SlideExportRenderer()
            let progress = SlideExportProgress()
            progress.onCancel = { [weak self, weak renderer] in
                self?.slideExportTask?.cancel()
                renderer?.close()
            }
            progress.show()
            defer { progress.close(); renderer.close(); exportingSlides = false; slideExportTask = nil }
            do {
                _ = try await editorWebView?.callAsyncJavaScript("await window.keynopePrepareDocumentSave?.();", arguments: [:], in: nil, contentWorld: .page)
                var request = URLRequest(url: endpoint)
                request.httpMethod = "POST"
                request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                request.httpBody = try JSONSerialization.data(withJSONObject: ["path": "Snapshot.html"])
                let (data, response) = try await URLSession.shared.data(for: request)
                try progress.checkCancellation()
                guard (response as? HTTPURLResponse)?.statusCode == 200 else {
                    throw NSError(domain: "sh.keynope.export", code: 5, userInfo: [NSLocalizedDescriptionKey: "The presentation could not be prepared for export."])
                }
                let document = try JSONDecoder().decode(ExportDocumentResponse.self, from: data)
                try await renderer.load(document.content)
                try progress.checkCancellation()
                let pages = try await renderer.pages()
                try progress.checkCancellation()
                progress.hide()
                let panel = NSSavePanel()
                panel.title = pdf ? "Export Presentation as PDF" : "Export Slide as PNG"
                panel.prompt = "Export"
                panel.allowedContentTypes = [pdf ? .pdf : .png]
                panel.canCreateDirectories = true
                let name = activeDeckURLHandler?()?.deletingPathExtension().lastPathComponent ?? "Untitled"
                configureDocumentDestinationPanel(panel, extension: pdf ? "pdf" : "png")
                let selector = NSPopUpButton(frame: NSRect(x: 0, y: 0, width: 240, height: 28))
                if !pdf {
                    for page in pages { selector.addItem(withTitle: "Slide " + (page["label"] as? String ?? "")) }
                    selector.setAccessibilityLabel("Slide to export")
                    panel.accessoryView = selector
                }
                panel.message = "Static slide artwork only. Activity results and generated join codes are excluded. Animations use their first frame."
                guard runDocumentPanel(panel) == .OK, let destination = panel.url else { return }
                progress.show()
                try progress.checkCancellation()
                if pdf {
                    let output = SlidePDFEncoder(title: name)
                    for (position, page) in pages.enumerated() {
                        try progress.checkCancellation()
                        progress.update("Rendering slide \(position + 1) of \(pages.count)…", completed: position, total: pages.count)
                        guard let index = page["index"] as? Int else { continue }
                        let bytes = try await renderer.imageData(page: index)
                        try progress.checkCancellation()
                        try await output.append(imageData: bytes)
                    }
                    progress.update("Writing PDF…", completed: pages.count, total: pages.count)
                    await Task.yield()
                    try progress.checkCancellation()
                    let bytes = try await output.finish()
                    try progress.checkCancellation()
                    try bytes.write(to: destination, options: .atomic)
                } else {
                    progress.update("Rendering selected slide…", completed: 0)
                    guard let index = pages[selector.indexOfSelectedItem]["index"] as? Int else { throw CocoaError(.fileWriteUnknown) }
                    let tiff = try await renderer.imageData(page: index)
                    try progress.checkCancellation()
                    guard let bitmap = NSBitmapImageRep(data: tiff),
                          let bytes = bitmap.representation(using: .png, properties: [:]) else { throw CocoaError(.fileWriteUnknown) }
                    try bytes.write(to: destination, options: .atomic)
                }
                _ = try? await editorWebView?.evaluateJavaScript("window.keynopeDidExport?.()")
            } catch {
                if progress.cancelled || Task.isCancelled || error is CancellationError { return }
                progress.hide()
                let alert = NSAlert(error: error)
                alert.messageText = "Could Not Export Presentation"
                alert.runModal()
            }
        }
    }

    private func exportHTML() {
        let panel = NSSavePanel()
        panel.title = "Export Keynope Presentation to HTML"
        panel.prompt = "Export"
        panel.allowedContentTypes = [UTType.html]
        panel.canCreateDirectories = true
        configureDocumentDestinationPanel(panel, extension: "html")
        guard runDocumentPanel(panel) == .OK,
              let destination = panel.url?.standardizedFileURL,
              let endpoint = editorEndpoint("/api/editor/export-document") else { return }
        Task {
            do {
                let existing = (try? String(contentsOf: destination, encoding: .utf8)) ?? ""
                _ = try await editorWebView?.callAsyncJavaScript("await window.keynopePrepareDocumentSave?.();", arguments: [:], in: nil, contentWorld: .page)
                var request = URLRequest(url: endpoint)
                request.httpMethod = "POST"
                request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                request.httpBody = try JSONSerialization.data(withJSONObject: ["path": destination.path, "existing": existing])
                let (data, response) = try await URLSession.shared.data(for: request)
                guard (response as? HTTPURLResponse)?.statusCode == 200 else {
                    throw NSError(domain: "sh.keynope.app", code: 22, userInfo: [NSLocalizedDescriptionKey: String(data: data, encoding: .utf8) ?? "The presentation could not be exported."])
                }
                let document = try JSONDecoder().decode(ExportDocumentResponse.self, from: data)
                try Data(document.content.utf8).write(to: destination, options: .atomic)
                await MainActor.run {
                    self.editorWebView?.evaluateJavaScript("window.keynopeDidExport && window.keynopeDidExport()")
                }
            } catch {
                await MainActor.run {
                    let alert = NSAlert(error: error)
                    alert.messageText = "Could Not Export Presentation"
                    alert.runModal()
                }
            }
        }
    }

    private func saveDocument(forceSaveAs: Bool, copyOnly: Bool = false) {
        guard !savingDocument else { return }
        var destination = forceSaveAs ? nil : activeDeckURLHandler?()
        if destination == nil {
            let panel = NSSavePanel()
            panel.title = copyOnly ? "Save a Copy of Presentation" : "Save Keynope Presentation"
            panel.prompt = "Save"
            panel.allowedContentTypes = [UTType(filenameExtension: "md")].compactMap { $0 }
            panel.canCreateDirectories = true
            configureDocumentDestinationPanel(panel, extension: "md", copySuffix: copyOnly)
            guard runDocumentPanel(panel) == .OK else {
                cancelDeferredSaveContinuation()
                return
            }
            destination = panel.url
        }
        if copyOnly, let original = activeDeckURLHandler?(), let destination,
           original.resolvingSymlinksInPath().standardizedFileURL == destination.resolvingSymlinksInPath().standardizedFileURL {
            let alert = NSAlert()
            alert.messageText = "Choose a Different File"
            alert.informativeText = "Save a Copy keeps the original presentation intact. Choose another filename or location."
            alert.runModal()
            return
        }
        guard let destination = destination?.standardizedFileURL,
              let documentURL = editorEndpoint("/api/editor/document"),
              let actionURL = editorEndpoint("/api/editor/action") else {
            cancelDeferredSaveContinuation()
            return
        }
        savingDocument = true
        saveMenuItem?.isEnabled = false
        Task {
            do {
                let activityResultsPending: Bool
                if let webView = self.editorWebView {
                    _ = try await webView.callAsyncJavaScript("await window.keynopePrepareDocumentSave?.();", arguments: [:], in: nil, contentWorld: .page)
                    // A failed auxiliary result write must never veto saving the
                    // deck itself, including when an older page is still open.
                    let flushed = try await webView.callAsyncJavaScript("try { return (await window.keynopeFlushActivityResults?.()) !== false; } catch (error) { console.warn('Activity results pending:', error); return false; }", arguments: [:], in: nil, contentWorld: .page)
                    activityResultsPending = (flushed as? Bool) == false
                } else {
                    activityResultsPending = false
                }
                var request = URLRequest(url: documentURL)
                request.httpMethod = "POST"
                request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                request.httpBody = try JSONSerialization.data(withJSONObject: ["path": destination.path])
                let (data, response) = try await URLSession.shared.data(for: request)
                guard (response as? HTTPURLResponse)?.statusCode == 200 else {
                    throw NSError(domain: "sh.keynope.app", code: 20, userInfo: [NSLocalizedDescriptionKey: String(data: data, encoding: .utf8) ?? "The presentation could not be prepared for saving."])
                }
                let document = try JSONDecoder().decode(SaveDocumentResponse.self, from: data)
                try Data(document.content.utf8).write(to: destination, options: .atomic)
                if copyOnly {
                    self.savingDocument = false
                    self.saveMenuItem?.isEnabled = self.documentDirty
                    _ = try? await self.editorWebView?.evaluateJavaScript("window.showEngagementToast?.('Copy saved')")
                    return
                }
                var confirm = URLRequest(url: actionURL)
                confirm.httpMethod = "POST"
                confirm.setValue("application/json", forHTTPHeaderField: "Content-Type")
                confirm.httpBody = try JSONSerialization.data(withJSONObject: ["action": "confirm-save", "path": destination.path, "value": document.version])
                let (confirmData, confirmResponse) = try await URLSession.shared.data(for: confirm)
                guard (confirmResponse as? HTTPURLResponse)?.statusCode == 200 else {
                    throw NSError(domain: "sh.keynope.app", code: 21, userInfo: [NSLocalizedDescriptionKey: String(data: confirmData, encoding: .utf8) ?? "The saved snapshot changed while it was being written."])
                }
                await MainActor.run {
                    self.savingDocument = false
                    self.documentDirty = activityResultsPending
                    self.saveMenuItem?.isEnabled = activityResultsPending
                    self.didSaveDeckHandler?(destination.path)
                    self.updateEditorWindowTitle(deckPath: destination.path)
                    self.editorWebView?.evaluateJavaScript("window.keynopeDidSave && window.keynopeDidSave()")
                    // The deck was saved, but do not discard recoverable results
                    // by automatically closing or opening another document.
                    if activityResultsPending {
                        self.closeAfterSave = false
                        self.actionAfterSave = nil
                        if self.terminateAfterSave {
                            self.terminateAfterSave = false
                            NSApp.reply(toApplicationShouldTerminate: false)
                        }
                        return
                    }
                    if let action = self.actionAfterSave {
                        self.actionAfterSave = nil
                        action()
                        return
                    }
                    if self.terminateAfterSave {
                        self.terminateAfterSave = false
                        NSApp.reply(toApplicationShouldTerminate: true)
                        return
                    }
                    if self.closeAfterSave {
                        self.closeAfterSave = false
                        self.editorWindow?.performClose(nil)
                    }
                }
            } catch {
                await MainActor.run {
                    self.savingDocument = false
                    self.closeAfterSave = false
                    self.actionAfterSave = nil
                    if self.terminateAfterSave {
                        self.terminateAfterSave = false
                        NSApp.reply(toApplicationShouldTerminate: false)
                    }
                    self.saveMenuItem?.isEnabled = self.documentDirty
                    let alert = NSAlert(error: error)
                    alert.messageText = "Could Not Save Presentation"
                    alert.runModal()
                }
            }
        }
    }

    private func cancelDeferredSaveContinuation() {
        closeAfterSave = false
        actionAfterSave = nil
        if terminateAfterSave {
            terminateAfterSave = false
            NSApp.reply(toApplicationShouldTerminate: false)
        }
    }

    private func replaceDeck(path: String) {
        guard let newURL = openDeckHandler?(path) else {
            return
        }
        noPresentation()
        presenterURL = newURL
        documentDirty = false
        saveMenuItem?.isEnabled = false
        editorWebView?.load(URLRequest(url: editorURL()))
        participantTabsMenuItem?.isEnabled = false
        activityQRMenuItem?.isEnabled = false
        updateEditorWindowTitle(deckPath: path)
        showEditorWindow()
    }

    private var editorHostScreen: NSScreen? {
        editorWindow?.screen ?? NSScreen.main ?? NSScreen.screens.first
    }

    private func externalScreen() -> NSScreen? {
        guard let host = editorHostScreen else {
            return NSScreen.screens.dropFirst().first
        }
        let hostID = screenDisplayID(host)
        return NSScreen.screens.first { candidate in
            if let hostID, let candidateID = screenDisplayID(candidate) {
                return candidateID != hostID
            }
            return candidate !== host
        }
    }

    private var hasExternalDisplay: Bool {
        externalScreen() != nil
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
        if preferExternal, let external = externalScreen() {
            return external
        }
        return editorHostScreen ?? NSScreen.screens.first!
    }

    private func showPresentation(preferExternal: Bool, remember: Bool = true) {
        presentationPreparationGeneration += 1
        let generation = presentationPreparationGeneration
        Task {
            do {
                _ = try await editorWebView?.callAsyncJavaScript("await window.keynopePrepareDocumentSave?.();", arguments: [:], in: nil, contentWorld: .page)
                guard generation == presentationPreparationGeneration else { return }
                openPreparedPresentation(preferExternal: preferExternal, remember: remember)
            } catch {
                guard generation == presentationPreparationGeneration else { return }
                let alert = NSAlert(error: error)
                alert.messageText = "Could Not Start Presentation"
                alert.runModal()
            }
        }
    }

    private func openPreparedPresentation(preferExternal: Bool, remember: Bool) {
        if preferExternal && !hasExternalDisplay {
            noPresentation()
            return
        }
        presentationMode = preferExternal ? "external" : "main"
        presentationPaused = false
        updateParticipantPublicationTimer()
        if remember {
            UserDefaults.standard.set(presentationMode, forKey: presentationModeKey)
        }
        reportPresentationMode()

        let screen = targetScreen(preferExternal: preferExternal)
        presentationDisplayID = screenDisplayID(screen)
        let frame = screen.frame
        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "keynopePresenter")
        let composite = PresenterCompositeView(frame: NSRect(origin: .zero, size: frame.size), configuration: config)
        let view = composite.webView
        view.autoresizingMask = [.width, .height]
        view.setAccessibilityLabel("Keynope presentation output")
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
        presentationPreparationGeneration += 1
        Task {
            await screenShareController.stop()
        }
        window?.close()
        window = nil
        webView = nil
        compositeView = nil
        presentationDisplayID = nil
        screenShareController.previewView = nil
        presentationMode = "none"
        presentationPaused = false
        updateParticipantPublicationTimer()
        if remember {
            UserDefaults.standard.set(presentationMode, forKey: presentationModeKey)
        }
        updatePresentationMenuState()
        refreshShareMenu()
        reportPresentationMode()
        publishPresentationState()
    }

    @objc private func screenParametersChanged() {
        if presentationMode != "none" {
            guard let displayID = presentationDisplayID,
                  let target = NSScreen.screens.first(where: { screenDisplayID($0) == displayID }) else {
                noPresentation(remember: false)
                updateDisplayState()
                return
            }
            if let window, !NSEqualRects(window.frame, target.frame) {
                window.setFrame(target.frame, display: true, animate: false)
            }
        }
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
            } else if action == "save-presentation" {
                saveDocument(forceSaveAs: false)
            } else if action == "save-presentation-copy" {
                saveDocument(forceSaveAs: true, copyOnly: true)
            } else if action == "export-html" {
                exportHTML()
            } else if action == "editor-dirty-state", let dirty = body["dirty"] as? Bool {
                documentDirty = dirty
                saveMenuItem?.isEnabled = dirty && !savingDocument
                updateEditorWindowTitle()
            } else if action == "editor-tabs-availability", let available = body["available"] as? Bool {
                participantTabsMenuItem?.isEnabled = available
                activityQRMenuItem?.isEnabled = available
                activityQRMenuItem?.state = (body["activityQRVisible"] as? Bool ?? true) ? .on : .off
            } else if action == "editor-performance",
                      ProcessInfo.processInfo.environment["KEYNOPE_PERFORMANCE"] == "1" {
                let kind = body["kind"] as? String ?? "unknown"
                let response = body["responseMs"] as? Double ?? 0
                let command = body["commandMs"] as? Double ?? 0
                let frame = body["frameMs"] as? Double ?? 0
                let source = body["frameSource"] as? String ?? "unknown"
                let visibility = body["visibility"] as? String ?? "unknown"
                NSLog("Keynope performance %@ response=%.2fms command=%.2fms frame=%.2fms source=%@ visibility=%@", kind, response, command, frame, source, visibility)
            }
        }
    }

#if KEYNOPE_NATIVE_ACCEPTANCE
    private struct NativeAcceptanceError: LocalizedError {
        let message: String
        var errorDescription: String? { message }
    }

    private func waitForNativeAcceptance(
        _ label: String,
        timeout: TimeInterval = 45,
        condition: @escaping @MainActor () -> Bool
    ) async throws {
        let deadline = Date().addingTimeInterval(timeout)
        while !condition() {
            if Date() >= deadline {
                throw NativeAcceptanceError(message: "Timed out waiting for \(label)")
            }
            try await Task.sleep(for: .milliseconds(50))
        }
    }

    private func requireNativeAcceptance(_ condition: Bool, _ label: String, checks: inout [String]) throws {
        guard condition else { throw NativeAcceptanceError(message: label) }
        checks.append(label)
    }

    private func nativeAcceptancePresenterState() async throws -> NativeAcceptancePresenterState {
        guard let url = editorEndpoint("/state") else {
            throw NativeAcceptanceError(message: "Could not form the presenter-state URL")
        }
        let (data, response) = try await URLSession.shared.data(from: url)
        guard (response as? HTTPURLResponse)?.statusCode == 200 else {
            throw NativeAcceptanceError(message: "Could not read presenter state")
        }
        return try JSONDecoder().decode(NativeAcceptancePresenterState.self, from: data)
    }

    private func waitForNativePresenterState(
        _ label: String,
        timeout: TimeInterval = 45,
        condition: @escaping (NativeAcceptancePresenterState) -> Bool
    ) async throws -> NativeAcceptancePresenterState {
        let deadline = Date().addingTimeInterval(timeout)
        while true {
            let state = try await nativeAcceptancePresenterState()
            if condition(state) { return state }
            if Date() >= deadline {
                throw NativeAcceptanceError(message: "Timed out waiting for \(label)")
            }
            try await Task.sleep(for: .milliseconds(50))
        }
    }

    private func waitForNativeJavaScript(
        _ label: String,
        in view: WKWebView,
        expression: String,
        timeout: TimeInterval = 45
    ) async throws {
        let deadline = Date().addingTimeInterval(timeout)
        while true {
            if (try? await view.evaluateJavaScript(expression) as? Bool) == true { return }
            if Date() >= deadline {
                throw NativeAcceptanceError(message: "Timed out waiting for \(label)")
            }
            try await Task.sleep(for: .milliseconds(50))
        }
    }

    private func postNativeAcceptanceEditorAction(_ body: [String: Any]) async throws {
        guard let url = editorEndpoint("/api/editor/action") else {
            throw NativeAcceptanceError(message: "Could not form the editor-action URL")
        }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONSerialization.data(withJSONObject: body)
        let (data, response) = try await URLSession.shared.data(for: request)
        guard (response as? HTTPURLResponse)?.statusCode == 200 else {
            throw NativeAcceptanceError(message: String(data: data, encoding: .utf8) ?? "Native editor action failed")
        }
    }

    private func expectNativePanel(
        title: String,
        destination: URL? = nil,
        response: NSApplication.ModalResponse
    ) {
        nativePanelAcceptanceFailure = nil
        nativePanelAcceptanceStep = NativePanelAcceptanceStep(title: title, destination: destination, response: response)
    }

    private func requireNativePanelConsumed(_ label: String, checks: inout [String]) throws {
        if let failure = nativePanelAcceptanceFailure {
            throw NativeAcceptanceError(message: failure)
        }
        try requireNativeAcceptance(nativePanelAcceptanceStep == nil, label, checks: &checks)
    }

    private func nativeMenu(named title: String) -> NSMenu? {
        NSApp.mainMenu?.items.compactMap(\.submenu).first(where: { $0.title == title })
    }

    private func performNativeMenuItem(menu menuTitle: String, item itemTitle: String) throws {
        guard let menu = nativeMenu(named: menuTitle),
              let item = menu.items.first(where: { $0.title == itemTitle }),
              let action = item.action else {
            throw NativeAcceptanceError(message: "Missing \(menuTitle) → \(itemTitle)")
        }
        guard NSApp.sendAction(action, to: item.target, from: item) else {
            throw NativeAcceptanceError(message: "AppKit did not deliver \(menuTitle) → \(itemTitle)")
        }
    }

    @discardableResult
    private func performNativeShortcut(_ characters: String, keyCode: UInt16, modifiers: NSEvent.ModifierFlags) throws -> Bool {
        guard let event = NSEvent.keyEvent(
            with: .keyDown,
            location: .zero,
            modifierFlags: modifiers,
            timestamp: ProcessInfo.processInfo.systemUptime,
            windowNumber: editorWindow?.windowNumber ?? 0,
            context: nil,
            characters: characters,
            charactersIgnoringModifiers: characters.lowercased(),
            isARepeat: false,
            keyCode: keyCode
        ) else {
            throw NativeAcceptanceError(message: "Could not create the \(characters) shortcut event")
        }
        return NSApp.mainMenu?.performKeyEquivalent(with: event) == true
    }

    private func finishNativePanelAcceptance(root: URL, checks: [String], failure: String?) {
        let report: [String: Any] = [
            "passed": failure == nil,
            "checks": checks,
            "failure": failure ?? ""
        ]
        if let data = try? JSONSerialization.data(withJSONObject: report, options: [.prettyPrinted, .sortedKeys]) {
            try? data.write(to: root.appendingPathComponent("report.json"), options: .atomic)
        }
        discardingUnsavedChanges = true
        NSApp.terminate(nil)
    }

    private func runNativePanelAcceptance(root: URL) async {
        var checks: [String] = []
        let fileManager = FileManager.default
        let original = root.appendingPathComponent("original.md")
        let recovered = root.appendingPathComponent("recovered.md")
        let saved = root.appendingPathComponent("saved.md")
        let copy = root.appendingPathComponent("copy.md")
        let cancelledCopy = root.appendingPathComponent("cancelled-copy.md")
        let html = root.appendingPathComponent("deck.html")
        let cancelledHTML = root.appendingPathComponent("cancelled.html")
        let png = root.appendingPathComponent("slide.png")
        let cancelledPNG = root.appendingPathComponent("cancelled.png")
        let pdf = root.appendingPathComponent("deck.pdf")
        let cancelledPDF = root.appendingPathComponent("cancelled.pdf")

        do {
            try await waitForNativeAcceptance("initial editor load") {
                self.editorWebView?.isLoading == false && self.documentDirty == false
            }
            try requireNativeAcceptance(activeDeckURLHandler?()?.standardizedFileURL == original.standardizedFileURL, "opened the initial deck through the native app", checks: &checks)
            try requireNativeAcceptance(statusItem?.button?.accessibilityLabel() == "Keynope Presenter", "menu-bar presentation controls expose a stable native accessibility label", checks: &checks)
            try requireNativeAcceptance(editorWebView?.accessibilityLabel() == "Keynope presentation editor", "editor WebView exposes a stable native accessibility entry label", checks: &checks)

            let fileTitles = Set(nativeMenu(named: "File")?.items.map(\.title) ?? [])
            try requireNativeAcceptance(["New Presentation", "Open…", "Save", "Save As…", "Save a Copy…", "Export Presentation as PDF…", "Export Slide as PNG…"].allSatisfy(fileTitles.contains), "native File menu exposes the standard document commands", checks: &checks)
            let openMenuItem = nativeMenu(named: "File")?.items.first(where: { $0.title == "Open…" })
            let saveAsMenuItem = nativeMenu(named: "File")?.items.first(where: { $0.title == "Save As…" })
            try requireNativeAcceptance(openMenuItem?.keyEquivalent.lowercased() == "o" && openMenuItem?.keyEquivalentModifierMask == [.command], "Command-O is wired to native Open", checks: &checks)
            try requireNativeAcceptance(saveAsMenuItem?.keyEquivalent.uppercased() == "S" && saveAsMenuItem?.keyEquivalentModifierMask == [.command, .shift], "Command-Shift-S is wired to native Save As", checks: &checks)
            let applicationSubmenus = NSApp.mainMenu?.items.compactMap(\.submenu) ?? []
            try requireNativeAcceptance(applicationSubmenus.contains(where: { menu in menu.items.contains(where: { $0.title == "Settings" }) && menu.items.contains(where: { $0.title == "Services" }) }), "native application menu exposes Settings and Services", checks: &checks)
            let windowTitles = Set(nativeMenu(named: "Window")?.items.map(\.title) ?? [])
            try requireNativeAcceptance(["Minimize", "Zoom", "Show Keynope", "Bring All to Front"].allSatisfy(windowTitles.contains), "native Window menu exposes standard window commands", checks: &checks)

            editorWindow?.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            try requireNativeAcceptance(try performNativeShortcut("m", keyCode: 46, modifiers: [.command]), "Command-M routed to Window → Minimize", checks: &checks)
            editorWindow?.performMiniaturize(nil)
            try await waitForNativeAcceptance("window minimization") { self.editorWindow?.isMiniaturized == true }
            editorWindow?.deminiaturize(nil)
            try await waitForNativeAcceptance("window restoration") { self.editorWindow?.isMiniaturized == false }
            try requireNativeAcceptance(true, "Window → Minimize uses the native window action and restores cleanly", checks: &checks)

            showPresentation(preferExternal: false)
            try await waitForNativeAcceptance("main-display presentation window", timeout: 60) {
                self.presentationMode == "main" && self.window?.isVisible == true && self.webView?.isLoading == false
            }
            let presentationState = try await waitForNativePresenterState("presenting state") { $0.presenting }
            try requireNativeAcceptance(presentationState.slide == 0 && presentationState.page == 0, "native presentation opened on the active slide", checks: &checks)
            try requireNativeAcceptance(webView?.accessibilityLabel() == "Keynope presentation output", "presentation WebView is distinct from the editor in the native accessibility tree", checks: &checks)
            let expectedFrame = targetScreen(preferExternal: false).frame
            try requireNativeAcceptance(window?.styleMask.contains(.borderless) == true && NSEqualRects(window?.frame ?? .zero, expectedFrame), "main-display presentation uses a borderless screen-sized native window", checks: &checks)
            try requireNativeAcceptance(presentationDisplayID == window?.screen.flatMap(screenDisplayID), "native presentation retains its physical display identity across screen-geometry notifications", checks: &checks)
            try requireNativeAcceptance(editorWindow?.isVisible == true && mainDisplayItem?.state == .on && noPresentationItem?.state == .off, "editor and native presentation controls expose the active main-display state", checks: &checks)

            pausePresentation()
            _ = try await waitForNativePresenterState("paused presentation state") { !$0.presenting }
            try requireNativeAcceptance(presentationPaused && shareRootItem?.isEnabled == true, "Pause keeps the presentation target while suspending output", checks: &checks)
            resumePresentation()
            _ = try await waitForNativePresenterState("resumed presentation state") { $0.presenting }
            try requireNativeAcceptance(!presentationPaused, "Resume restores native presentation output", checks: &checks)

            try await postNativeAcceptanceEditorAction(["action": "navigate-presentation", "slide": 1, "page": 0])
            _ = try await waitForNativePresenterState("cheat-sheet navigation") { $0.presenting && $0.slide == 1 && $0.page == 0 }
            guard let presentationWebView = webView else {
                throw NativeAcceptanceError(message: "Native presentation WebView disappeared")
            }
            try await waitForNativeJavaScript(
                "cheat-sheet presentation paint",
                in: presentationWebView,
                expression: "window.keynopePresentationPosition?.().tabOnly === true"
            )
            try requireNativeAcceptance(true, "tab-only cheat sheet follows the active native presentation", checks: &checks)

            _ = try await presentationWebView.evaluateJavaScript("['0','0','0','3','0','Enter'].forEach(key => dispatchEvent(new KeyboardEvent('keydown',{key,bubbles:true})))")
            let timerState = try await waitForNativePresenterState("native presentation timer") {
                $0.timerMode == "running" && ($0.timerEndMs ?? 0) > Int64(Date().timeIntervalSince1970 * 1000)
            }
            try requireNativeAcceptance((timerState.timerEndMs ?? 0) > 0, "timer deadline reaches the native presentation state", checks: &checks)
            try await waitForNativeJavaScript("timer paint", in: presentationWebView, expression: "presenterTimerText().text.length > 0", timeout: 10)
            try requireNativeAcceptance(true, "native presenter renders the shared timer", checks: &checks)
            _ = try await presentationWebView.evaluateJavaScript("dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}))")
            _ = try await waitForNativePresenterState("stopped native timer") { ($0.timerMode ?? "").isEmpty && ($0.timerEndMs ?? 0) == 0 }
            try requireNativeAcceptance(true, "stopping the timer clears the shared deadline", checks: &checks)

            noPresentation()
            _ = try await waitForNativePresenterState("stopped presentation state") { !$0.presenting }
            try requireNativeAcceptance(window == nil && webView == nil && presentationMode == "none" && presentationDisplayID == nil, "Stop closes and releases the native presentation surface and display identity", checks: &checks)
            try requireNativeAcceptance(editorWindow?.isVisible == true && noPresentationItem?.state == .on && mainDisplayItem?.state == .off && shareRootItem?.isEnabled == false, "Stop restores the editor and disables presentation-only controls", checks: &checks)

            let initialDirty = documentDirty
            expectNativePanel(title: "Save Keynope Presentation", destination: root.appendingPathComponent("cancelled-save.md"), response: .cancel)
            try performNativeMenuItem(menu: "File", item: "Save As…")
            try requireNativePanelConsumed("cancelled Save As through NSSavePanel", checks: &checks)
            try requireNativeAcceptance(documentDirty == initialDirty && activeDeckURLHandler?()?.standardizedFileURL == original.standardizedFileURL, "cancelled Save As retained document URL and dirty state", checks: &checks)

            expectNativePanel(title: "Save Keynope Presentation", destination: saved, response: .OK)
            try performNativeMenuItem(menu: "File", item: "Save As…")
            try await waitForNativeAcceptance("Save As completion") {
                !self.savingDocument && fileManager.fileExists(atPath: saved.path) && self.activeDeckURLHandler?()?.standardizedFileURL == saved.standardizedFileURL
            }
            try requireNativePanelConsumed("completed Save As through NSSavePanel", checks: &checks)
            try requireNativeAcceptance(documentDirty == false, "Save As cleared dirty state", checks: &checks)

            expectNativePanel(title: "Save a Copy of Presentation", destination: copy, response: .OK)
            try performNativeMenuItem(menu: "File", item: "Save a Copy…")
            try await waitForNativeAcceptance("Save a Copy completion") {
                !self.savingDocument && fileManager.fileExists(atPath: copy.path)
            }
            try requireNativePanelConsumed("completed Save a Copy through NSSavePanel", checks: &checks)
            try requireNativeAcceptance(activeDeckURLHandler?()?.standardizedFileURL == saved.standardizedFileURL && documentDirty == false, "Save a Copy retained original document URL and dirty state", checks: &checks)

            expectNativePanel(title: "Save a Copy of Presentation", destination: cancelledCopy, response: .cancel)
            try performNativeMenuItem(menu: "File", item: "Save a Copy…")
            try requireNativePanelConsumed("cancelled Save a Copy through NSSavePanel", checks: &checks)
            try requireNativeAcceptance(!fileManager.fileExists(atPath: cancelledCopy.path) && activeDeckURLHandler?()?.standardizedFileURL == saved.standardizedFileURL && documentDirty == false, "cancelled Save a Copy left document and filesystem unchanged", checks: &checks)

            expectNativePanel(title: "Export Keynope Presentation to HTML", destination: cancelledHTML, response: .cancel)
            exportHTML()
            try requireNativePanelConsumed("cancelled HTML export through NSSavePanel", checks: &checks)
            try requireNativeAcceptance(!fileManager.fileExists(atPath: cancelledHTML.path), "cancelled HTML export wrote no file", checks: &checks)

            expectNativePanel(title: "Export Keynope Presentation to HTML", destination: html, response: .OK)
            exportHTML()
            try await waitForNativeAcceptance("HTML export completion") { fileManager.fileExists(atPath: html.path) }
            try requireNativePanelConsumed("completed HTML export through NSSavePanel", checks: &checks)
            let htmlText = try String(contentsOf: html, encoding: .utf8)
            try requireNativeAcceptance(htmlText.localizedCaseInsensitiveContains("<!doctype html"), "HTML export produced a standalone document", checks: &checks)

            expectNativePanel(title: "Export Slide as PNG", destination: cancelledPNG, response: .cancel)
            try performNativeMenuItem(menu: "File", item: "Export Slide as PNG…")
            try await waitForNativeAcceptance("cancelled PNG export") { !self.exportingSlides && self.nativePanelAcceptanceStep == nil }
            try requireNativePanelConsumed("cancelled PNG export through NSSavePanel", checks: &checks)
            try requireNativeAcceptance(!fileManager.fileExists(atPath: cancelledPNG.path), "cancelled PNG export wrote no file", checks: &checks)

            expectNativePanel(title: "Export Slide as PNG", destination: png, response: .OK)
            try performNativeMenuItem(menu: "File", item: "Export Slide as PNG…")
            try await waitForNativeAcceptance("PNG export completion", timeout: 90) {
                !self.exportingSlides && fileManager.fileExists(atPath: png.path)
            }
            try requireNativePanelConsumed("completed PNG export through NSSavePanel", checks: &checks)
            let pngData = try Data(contentsOf: png)
            try requireNativeAcceptance(pngData.starts(with: [0x89, 0x50, 0x4e, 0x47]), "PNG export produced PNG bytes", checks: &checks)

            expectNativePanel(title: "Export Presentation as PDF", destination: cancelledPDF, response: .cancel)
            try performNativeMenuItem(menu: "File", item: "Export Presentation as PDF…")
            try await waitForNativeAcceptance("cancelled PDF export", timeout: 90) { !self.exportingSlides && self.nativePanelAcceptanceStep == nil }
            try requireNativePanelConsumed("cancelled PDF export through NSSavePanel", checks: &checks)
            try requireNativeAcceptance(!fileManager.fileExists(atPath: cancelledPDF.path), "cancelled PDF export wrote no file", checks: &checks)

            expectNativePanel(title: "Export Presentation as PDF", destination: pdf, response: .OK)
            try performNativeMenuItem(menu: "File", item: "Export Presentation as PDF…")
            try await waitForNativeAcceptance("PDF export completion", timeout: 120) {
                !self.exportingSlides && fileManager.fileExists(atPath: pdf.path)
            }
            try requireNativePanelConsumed("completed PDF export through NSSavePanel", checks: &checks)
            let pdfData = try Data(contentsOf: pdf)
            try requireNativeAcceptance(pdfData.starts(with: Array("%PDF".utf8)), "PDF export produced PDF bytes", checks: &checks)

            expectNativePanel(title: "Open a Keynope Deck", destination: recovered, response: .OK)
            try performNativeMenuItem(menu: "File", item: "Open…")
            try await waitForNativeAcceptance("recoverable deck open", timeout: 60) {
                self.activeDeckURLHandler?()?.standardizedFileURL == recovered.standardizedFileURL && self.editorWebView?.isLoading == false && self.documentDirty == false
            }
            try requireNativePanelConsumed("opened a recoverable deck through NSOpenPanel", checks: &checks)
            guard let stateURL = editorEndpoint("/api/editor/state") else {
                throw NativeAcceptanceError(message: "Could not form the editor-state URL after recovery")
            }
            let (stateData, stateResponse) = try await URLSession.shared.data(from: stateURL)
            guard (stateResponse as? HTTPURLResponse)?.statusCode == 200 else {
                throw NativeAcceptanceError(message: "Could not read editor state after recovery")
            }
            let recoveredState = try JSONDecoder().decode(NativeAcceptanceEditorState.self, from: stateData)
            try requireNativeAcceptance(recoveredState.diagnostics?.contains(where: { $0.code == "appearance-recovered" }) == true, "recoverable optional metadata surfaced its editor diagnostic", checks: &checks)

            expectNativePanel(title: "Open a Keynope Deck", destination: original, response: .cancel)
            try performNativeMenuItem(menu: "File", item: "Open…")
            try requireNativePanelConsumed("cancelled Open through NSOpenPanel", checks: &checks)
            try requireNativeAcceptance(activeDeckURLHandler?()?.standardizedFileURL == recovered.standardizedFileURL && documentDirty == false, "cancelled Open retained document URL and dirty state", checks: &checks)

            try requireNativeAcceptance(activeDeckURLHandler?()?.standardizedFileURL == recovered.standardizedFileURL, "exports did not replace the active deck", checks: &checks)
            try performNativeMenuItem(menu: "File", item: "New Presentation")
            try await waitForNativeAcceptance("new presentation", timeout: 60) {
                self.activeDeckURLHandler?() == nil && self.editorWebView?.isLoading == false && self.documentDirty == true
            }
            try requireNativeAcceptance(editorWindow?.title.contains("Untitled *") == true, "File → New created a dirty untitled deck and updated the native title", checks: &checks)
            finishNativePanelAcceptance(root: root, checks: checks, failure: nil)
        } catch {
            finishNativePanelAcceptance(root: root, checks: checks, failure: error.localizedDescription)
        }
    }
#endif

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return appMode
    }

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard appMode, documentDirty, !discardingUnsavedChanges else { return .terminateNow }
        let response = unsavedChangesAlert().runModal()
        switch response {
        case .alertFirstButtonReturn:
            terminateAfterSave = true
            saveDocument(forceSaveAs: false)
            return .terminateLater
        case .alertSecondButtonReturn:
            discardingUnsavedChanges = true
            return .terminateNow
        default:
            return .terminateCancel
        }
    }

    private func unsavedChangesAlert() -> NSAlert {
        let alert = NSAlert()
        alert.alertStyle = .warning
        alert.messageText = "Do you want to save the changes to this presentation?"
        alert.informativeText = "Your changes will be lost if you don’t save them."
        alert.addButton(withTitle: "Save")
        alert.addButton(withTitle: "Don’t Save")
        alert.addButton(withTitle: "Cancel")
        return alert
    }

    func windowShouldClose(_ sender: NSWindow) -> Bool {
        guard appMode, sender === editorWindow, documentDirty else { return true }
        let alert = unsavedChangesAlert()
        switch alert.runModal() {
        case .alertFirstButtonReturn:
            closeAfterSave = true
            saveDocument(forceSaveAs: false)
            return false
        case .alertSecondButtonReturn:
            discardingUnsavedChanges = true
            return true
        default:
            return false
        }
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
        let url = URL(fileURLWithPath: path).standardizedFileURL
        let accepted = requestDocumentReplacement { [weak self] in
            self?.openSelectedDeck(url)
        }
        sender.reply(toOpenOrPrint: accepted ? .success : .failure)
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
            selectedDeckURLHandler: appMode ? { url in
                try rememberUserSelectedDeckAccess(url)
            } : nil,
            newDeckHandler: appMode ? {
                guard let path = bundledWelcomeDeckPath() else { return nil }
                return startEngine(deckPath: path, untitled: true)
            } : nil,
            activeDeckURLHandler: appMode ? {
                activeDeckPath.map { URL(fileURLWithPath: $0) }
            } : nil,
            recentDeckPathsHandler: appMode ? {
                recentDecks().map(\.path)
            } : nil,
            didSaveDeckHandler: appMode ? { path in
                activeDeckPath = path
                recordRecentDeck(path)
                rememberSavedDeckAccess(path)
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
        if let recent = recentDecks().first,
           let url = startEngine(deckPath: recent.path, restoring: true) {
            return url
        }
        guard let path = bundledWelcomeDeckPath() else {
            showFileError(title: "Could Not Open Starter Presentation", error: NSError(
                domain: "sh.keynope.app", code: 5,
                userInfo: [NSLocalizedDescriptionKey: "The bundled Welcome.md starter is missing."]
            ))
            return nil
        }
        return startEngine(deckPath: path, untitled: true)
    }

    private static func recentDecks() -> [RecentDeck] {
        guard let data = UserDefaults.standard.data(forKey: recentDecksKey),
              let records = try? JSONDecoder().decode([RecentDeck].self, from: data) else {
            return []
        }
        var seen = Set<String>()
        return records
            .sorted { $0.lastOpened > $1.lastOpened }
            .filter { !$0.path.hasSuffix("/Keynope Presentations/Welcome/Welcome.md") }
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

    private static func bundledWelcomeDeckPath() -> String? {
        Bundle.main.url(forResource: "Welcome", withExtension: "md")?.path
    }

    private static func startEngine(deckPath: String, untitled: Bool = false, restoring: Bool = false) -> URL? {
        let authorization: DeckAuthorization
        do {
            authorization = try authorizeDeck(for: deckPath, allowPrompt: !restoring)
            if restoring && !FileManager.default.isReadableFile(atPath: authorization.path) { return nil }
        } catch {
            if !restoring && (error as? CocoaError)?.code != .userCancelled {
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
            + (untitled ? ["--untitled"] : [])
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
            if !restoring { showFileError(title: "Could Not Start Presentation", error: error) }
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
                    activeDeckPath = untitled ? nil : authorization.path
                    if !untitled { recordRecentDeck(authorization.path) }
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
        if restoring { return nil }
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

    private static func authorizeDeck(for deckPath: String, allowPrompt: Bool = true) throws -> DeckAuthorization {
        guard isSandboxed else {
            return DeckAuthorization(path: deckPath, helperBookmark: nil)
        }
        let deckURL = URL(fileURLWithPath: deckPath).standardizedFileURL
        if contains(deckURL, in: Bundle.main.bundleURL.standardizedFileURL) {
            return DeckAuthorization(path: deckURL.path, helperBookmark: nil)
        }
        if try contains(deckURL, in: presentationsLibraryURL()) {
            stopAccessingDeckDirectory()
            return DeckAuthorization(path: deckURL.path, helperBookmark: nil)
        }
        _ = try authorizeDeckFile(deckURL, allowPrompt: allowPrompt)
        return DeckAuthorization(path: deckURL.path, helperBookmark: try bookmarkForEngineHelper())
    }

    private static func rememberSavedDeckAccess(_ path: String) {
        guard isSandboxed else { return }
        let url = URL(fileURLWithPath: path).standardizedFileURL
        guard let bookmark = try? url.bookmarkData(options: .withSecurityScope, includingResourceValuesForKeys: nil, relativeTo: nil) else { return }
        UserDefaults.standard.set(bookmark, forKey: deckFileBookmarkPrefix + bookmarkKeySuffix(for: url))
    }

    private static func rememberUserSelectedDeckAccess(_ url: URL) throws {
        guard isSandboxed else { return }
        let selected = url.standardizedFileURL
        let bookmark = try selected.bookmarkData(
            options: .withSecurityScope,
            includingResourceValuesForKeys: nil,
            relativeTo: nil
        )
        UserDefaults.standard.set(bookmark, forKey: deckFileBookmarkPrefix + bookmarkKeySuffix(for: selected))
        stopAccessingDeckDirectory()
        _ = selected.startAccessingSecurityScopedResource()
        activeSandboxURL = selected
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

    private static func authorizeDeckFile(_ deckURL: URL, allowPrompt: Bool = true) throws -> Data {
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
            guard allowPrompt else { throw CocoaError(.fileReadNoPermission) }
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

    private static func contains(_ child: URL, in parent: URL) -> Bool {
        let childPath = child.resolvingSymlinksInPath().standardizedFileURL.path
        let parentPath = parent.resolvingSymlinksInPath().standardizedFileURL.path
        return childPath == parentPath || childPath.hasPrefix(parentPath + "/")
    }

    private static func bookmarkKeySuffix(for url: URL) -> String {
        Data(url.standardizedFileURL.path.utf8).base64EncodedString()
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
