@preconcurrency import Foundation
import Cocoa
import Darwin
@preconcurrency import ScreenCaptureKit
import UniformTypeIdentifiers
import WebKit
import CoreText
import PDFKit

// WebKit added actor/sendability annotations to this callback in the Xcode 16
// SDK. Match the SDK signature on both the release runners and newer Xcodes.
#if compiler(>=6.0)
typealias KeynopeScriptReplyHandler = @MainActor @Sendable (Any?, String?) -> Void
#else
typealias KeynopeScriptReplyHandler = (Any?, String?) -> Void
#endif

final class PresenterWindow: NSWindow {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { true }
}

@MainActor
final class PresenterDelegate: NSObject, NSApplicationDelegate, NSWindowDelegate, WKScriptMessageHandler, WKScriptMessageHandlerWithReply, NSMenuDelegate, WKUIDelegate {
    private var presenterURL: URL
    private let appMode: Bool
    private let openDeckHandler: ((String) -> URL?)?
    private let importDeckHandler: ((String) -> URL?)?
    private let selectedDeckURLHandler: ((URL) throws -> Void)?
    private let newDeckHandler: ((String?) -> URL?)?
    private var choosingTemplate = false
    private let activeDeckURLHandler: (() -> URL?)?
    private let recentDeckPathsHandler: (() -> [String])?
    private let didSaveDeckHandler: ((String) -> Void)?
    private let inputHandler: ((Data) -> Void)?
    private var statusItem: NSStatusItem?
    private var editorWindow: NSWindow?
    private var editorWebView: WKWebView?
    // Formatting survives document replacement, but never leaves this process.
    private var styleClipboardData = ""
    private var aboutWindow: NSWindow?
    private var licensesWindow: NSWindow?
    private let automationService = AutomationService()
    private var automationSettings: AutomationSettingsController?
    private var window: NSWindow?
    private var webView: WKWebView?
    private var compositeView: PresenterCompositeView?
    private var noPresentationItem: NSMenuItem?
    private var externalDisplayItem: NSMenuItem?
    private var mainDisplayItem: NSMenuItem?
    private var shareRootItem: NSMenuItem?
    private var shareMenu: NSMenu?
    private var recentDecksMenu: NSMenu?
    private var styleMenu: NSMenu?
    private var workspaceMenu: NSMenu?
    private var appearanceMenuItems: [String: NSMenuItem] = [:]
    private let interfaceAppearanceKey = "keynope.interface.appearance"
    private var styleMenuItems: [String: NSMenuItem] = [:]
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
        let accessoryIndex: Int?
        let manualConfirmation: Bool
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
    private var nativeSlideExportFailure: String?
    private var nativeSlideExportCancelPhase: String?
    private var nativePanelAcceptanceFailure: String?
    // Keep the actual selected URL for a later test-only cancelled staging pass.
    // The unsandboxed test parent verifies saved files after the production
    // writer releases its grant; the app does not extend that grant for readback.
    private var nativePanelAcceptanceSelectedURL: URL?
    private var nativePanelAcceptancePDFBytes: Data?
    private var nativeDeniedStyleMessages = 0
    private var nativeOrdinaryClipboardDeliveries = 0
#endif

    init(
        url: URL,
        appMode: Bool = false,
        openDeckHandler: ((String) -> URL?)? = nil,
        importDeckHandler: ((String) -> URL?)? = nil,
        selectedDeckURLHandler: ((URL) throws -> Void)? = nil,
        newDeckHandler: ((String?) -> URL?)? = nil,
        activeDeckURLHandler: (() -> URL?)? = nil,
        recentDeckPathsHandler: (() -> [String])? = nil,
        didSaveDeckHandler: ((String) -> Void)? = nil,
        inputHandler: ((Data) -> Void)? = nil
    ) {
        self.presenterURL = url
        self.appMode = appMode
        self.openDeckHandler = openDeckHandler
        self.importDeckHandler = importDeckHandler
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
        if UserDefaults.standard.bool(forKey: AutomationService.enabledKey) {
            Task { @MainActor in
                do { try await automationService.setEnabled(true) }
                catch { NSAlert(error: error).runModal() }
            }
        }
        // Keynope owns one document session, not independently tabbed windows.
        NSWindow.allowsAutomaticWindowTabbing = false
        applyInterfaceAppearance(savedInterfaceAppearance)
        #if KEYNOPE_NATIVE_ACCEPTANCE
        let backgroundTest = ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_BACKGROUND"] == "1" || ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_STYLE_MENU_ACCEPTANCE"] == "1" && ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_STYLE_MENU_FOREGROUND"] != "1"
        NSApp.setActivationPolicy(appMode && !backgroundTest ? .regular : .accessory)
        #else
        NSApp.setActivationPolicy(appMode ? .regular : .accessory)
        #endif
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
        settingsMenu.addItem(.separator())
        let automationItem = settingsMenu.addItem(withTitle: "Automation (MCP)…", action: #selector(configureAutomation), keyEquivalent: "")
        automationItem.target = self
        let appearanceItem = NSMenuItem(title: "Appearance", action: nil, keyEquivalent: "")
        let appearanceMenu = NSMenu(title: "Appearance")
        appearanceMenu.autoenablesItems = false
        for (mode, label) in [("system", "System"), ("light", "Light"), ("dark", "Dark")] {
            let item = appearanceMenu.addItem(withTitle: label, action: #selector(changeInterfaceAppearance(_:)), keyEquivalent: "")
            item.target = self
            item.identifier = NSUserInterfaceItemIdentifier(mode)
            item.state = savedInterfaceAppearance == mode ? .on : .off
            appearanceMenuItems[mode] = item
        }
        appearanceItem.submenu = appearanceMenu
        settingsMenu.addItem(appearanceItem)
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
        let pptxItem = fileMenu.addItem(withTitle: "Export Presentation as PowerPoint…", action: #selector(exportPPTX), keyEquivalent: "")
        pptxItem.target = self
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
        let cutItem = editMenu.addItem(withTitle: "Cut", action: #selector(cutEditorObjects(_:)), keyEquivalent: "x")
        let copyItem = editMenu.addItem(withTitle: "Copy", action: #selector(copyEditorObjects(_:)), keyEquivalent: "c")
        let pasteItem = editMenu.addItem(withTitle: "Paste", action: #selector(pasteEditorObjects(_:)), keyEquivalent: "v")
        for item in [cutItem, copyItem, pasteItem] { item.target = self }
        editMenu.addItem(withTitle: "Select All", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editMenu.addItem(NSMenuItem.separator())
        let styleItem = NSMenuItem(title: "Style", action: nil, keyEquivalent: "")
        let styles = NSMenu(title: "Style")
        // Shared capabilities own validation; do not override responder-chain
        // validation for the rest of Edit (Undo, text clipboard, Select All).
        styles.autoenablesItems = false
        styles.delegate = self
        for (key, title, shortcut) in [
            ("style.copy", "Copy Style", "c"),
            ("style.paste", "Paste Style", "v"),
            ("style.reset", "Reset Style", ""),
            ("style.painter", "Format Painter", ""),
            ("style.label.copy", "Copy Label Style", ""),
            ("style.label.paste", "Paste Label Style", ""),
            ("style.label.reset", "Reset Label Style", ""),
            ("style.label.painter", "Label Format Painter", "")
        ] {
            if key == "style.label.copy" { styles.addItem(NSMenuItem.separator()) }
            let item = styles.addItem(withTitle: title, action: #selector(performEditorStyleCommand(_:)), keyEquivalent: shortcut)
            item.identifier = NSUserInterfaceItemIdentifier(key)
            item.target = self
            item.isEnabled = false
            if !shortcut.isEmpty { item.keyEquivalentModifierMask = [.command, .option] }
            styleMenuItems[key] = item
        }
        styleMenu = styles
        styleItem.submenu = styles
        editMenu.addItem(styleItem)
        editItem.submenu = editMenu
        mainMenu.addItem(editItem)

        let viewItem = NSMenuItem()
        let viewMenu = NSMenu(title: "View")
        viewMenu.autoenablesItems = false
        viewMenu.delegate = self
        workspaceMenu = viewMenu
        for (command, title, shortcut, modifiers) in [
            ("navigator", "Slide Navigator", "1", NSEvent.ModifierFlags([.command, .option])),
            ("format", "Format Inspector", "i", [.command, .option]),
            ("fit", "Zoom to Fit", "0", [.command]),
            ("zoom-in", "Zoom In", "+", [.command]),
            ("zoom-out", "Zoom Out", "-", [.command]),
            ("rulers", "Rulers", "r", [.command]),
            ("snap", "Snap to Alignment Guides", "", []),
            ("focus-next", "Move Focus to Next Pane", "", [])
        ] {
            if command == "fit" || command == "rulers" || command == "focus-next" { viewMenu.addItem(.separator()) }
            let item = viewMenu.addItem(withTitle: title, action: #selector(performWorkspaceCommand(_:)), keyEquivalent: shortcut)
            item.target = self
            item.identifier = NSUserInterfaceItemIdentifier(command)
            item.keyEquivalentModifierMask = modifiers
            item.isEnabled = false
        }
        viewItem.submenu = viewMenu
        mainMenu.addItem(viewItem)

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

    static func installedFontCatalogue() -> [[String: String]] {
        NSFontManager.shared.availableFontFamilies.sorted { $0.localizedStandardCompare($1) == .orderedAscending }.flatMap { family in
            (NSFontManager.shared.availableMembers(ofFontFamily: family) ?? []).compactMap { member in
                guard member.count >= 2, let postscript = member[0] as? String, let style = member[1] as? String else { return nil }
                return ["family": family, "name": family + " — " + style, "postScriptName": postscript]
            }
        }
    }

    static func installedTypeface(_ name: String) throws -> [String: Any] {
        guard let entry = installedFontCatalogue().first(where: { $0["postScriptName"] == name }) else {
            throw NSError(domain: "KeynopeFonts", code: 1, userInfo: [NSLocalizedDescriptionKey: "This font is no longer installed."])
        }
        let font = CTFontCreateWithName(name as CFString, 32, nil)
        var result: [String: Any] = ["name": entry["name"] ?? name, "postScriptName": name]
        // Restricted outlines must stay on this device. Use an installed-font
        // reference even for the picker preview; never send their bytes to JS.
        var permissions: UInt16 = 2
        if let os2 = CTFontCopyTable(font, CTFontTableTag(0x4f532f32), []) as Data?, os2.count >= 10 {
            permissions = UInt16(os2[8]) << 8 | UInt16(os2[9])
            if (UInt16(os2[0]) << 8 | UInt16(os2[1])) < 2 { permissions &= 15 }
        }
        let restricted = permissions & 8 == 0 && permissions & 4 == 0 && permissions & 2 != 0
        if restricted || permissions & 0x200 != 0 {
            result["localOnly"] = true
            result["embeddingFlags"] = permissions
            return result
        }
        guard let available = CTFontCopyAvailableTables(font, []) else {
            throw NSError(domain: "KeynopeFonts", code: 2, userInfo: [NSLocalizedDescriptionKey: "The selected font has no readable OpenType tables."])
        }
        // Reconstitute this face, not the entire TTC collection. CoreText table
        // access also works for fonts activated outside the usual font folders.
        var tables: [(UInt32, Data)] = []
        var total = 0
        // CoreText returns raw table tags in a CFArray with no retaining
        // callbacks, NOT CFNumber objects. Bridging to NSArray/NSNumber crashes.
        let tags = (0..<CFArrayGetCount(available)).map { UInt32(truncatingIfNeeded: UInt(bitPattern: CFArrayGetValueAtIndex(available, $0))) }.sorted()
        for tag in tags {
            if tag == 0x44534947 { continue } // Repacking invalidates DSIG.
            guard var data = CTFontCopyTable(font, CTFontTableTag(tag), []) as Data? else { continue }
            if tag == 0x68656164, data.count >= 12 { data.replaceSubrange(8..<12, with: [0, 0, 0, 0]) }
            total += data.count
            guard total <= 16 * 1024 * 1024 else { throw NSError(domain: "KeynopeFonts", code: 3, userInfo: [NSLocalizedDescriptionKey: "This font exceeds the 16 MB font limit."]) }
            tables.append((tag, data))
        }
        guard !tables.isEmpty, tables.count <= 256 else { throw NSError(domain: "KeynopeFonts", code: 4, userInfo: [NSLocalizedDescriptionKey: "Invalid font table directory."]) }
        func append16(_ value: UInt16, to data: inout Data) { data.append(UInt8(value >> 8)); data.append(UInt8(value & 255)) }
        func append32(_ value: UInt32, to data: inout Data) { data.append(UInt8(value >> 24)); data.append(UInt8((value >> 16) & 255)); data.append(UInt8((value >> 8) & 255)); data.append(UInt8(value & 255)) }
        func checksum(_ data: Data) -> UInt32 {
            var value: UInt32 = 0
            for start in stride(from: 0, to: data.count, by: 4) { var word: UInt32 = 0; for i in 0..<4 { word = word << 8 | (start+i < data.count ? UInt32(data[start+i]) : 0) }; value = value &+ word }
            return value
        }
        var output = Data(), payload = Data(), headOffset: Int?
        let count = tables.count, power = Int(floor(log2(Double(count)))), search = (1 << power) * 16
        append32(tables.contains(where: { $0.0 == 0x43464620 || $0.0 == 0x43464632 }) ? 0x4f54544f : 0x00010000, to: &output)
        append16(UInt16(count), to: &output); append16(UInt16(search), to: &output); append16(UInt16(power), to: &output); append16(UInt16(count*16-search), to: &output)
        for (tag, data) in tables {
            let offset = 12 + count*16 + payload.count
            append32(tag, to: &output); append32(checksum(data), to: &output); append32(UInt32(offset), to: &output); append32(UInt32(data.count), to: &output)
            if tag == 0x68656164 { headOffset = offset }
            payload.append(data); while payload.count % 4 != 0 { payload.append(0) }
        }
        output.append(payload)
        if let headOffset, output.count >= headOffset+12 { var adjustment = Data(); append32(0xb1b0afba &- checksum(output), to: &adjustment); output.replaceSubrange((headOffset+8)..<(headOffset+12), with: adjustment) }
        result["data"] = output.base64EncodedString()
        return result
    }

    private var savedInterfaceAppearance: String {
        let mode = UserDefaults.standard.string(forKey: interfaceAppearanceKey) ?? "system"
        return ["system", "light", "dark"].contains(mode) ? mode : "system"
    }

    private func installAppearanceBootstrap(_ controller: WKUserContentController) {
        let prefix = "window.KEYNOPE_INTERFACE_APPEARANCE = "
        let otherScripts = controller.userScripts.filter { !$0.source.hasPrefix(prefix) }
        controller.removeAllUserScripts()
        otherScripts.forEach { controller.addUserScript($0) }
        controller.addUserScript(WKUserScript(source: prefix + "'" + savedInterfaceAppearance + "';", injectionTime: .atDocumentStart, forMainFrameOnly: true))
    }

    private func applyInterfaceAppearance(_ mode: String) {
        guard ["system", "light", "dark"].contains(mode) else { return }
        UserDefaults.standard.set(mode, forKey: interfaceAppearanceKey)
        NSApp.appearance = mode == "system" ? nil : NSAppearance(named: mode == "dark" ? .darkAqua : .aqua)
        for (key, item) in appearanceMenuItems { item.state = key == mode ? .on : .off }
        if let view = editorWebView {
            installAppearanceBootstrap(view.configuration.userContentController)
            Task { @MainActor [weak view] in
                _ = try? await view?.callAsyncJavaScript("return window.keynopeSetInterfaceAppearance?.(mode, false)", arguments: ["mode": mode], in: nil, contentWorld: .page)
            }
        }
    }

    @objc private func changeInterfaceAppearance(_ sender: NSMenuItem) {
        guard let mode = sender.identifier?.rawValue, appearanceMenuItems[mode] === sender else { return }
        applyInterfaceAppearance(mode)
    }

    @objc private func cutEditorObjects(_ sender: Any?) { editorClipboardCommand("cut") }
    @objc private func copyEditorObjects(_ sender: Any?) { editorClipboardCommand("copy") }
    @objc private func pasteEditorObjects(_ sender: Any?) { editorClipboardCommand("paste") }

    @objc private func performWorkspaceCommand(_ sender: NSMenuItem) {
        guard appMode, editorWindow?.isKeyWindow == true, let view = editorWebView,
              let command = sender.identifier?.rawValue, workspaceMenu?.items.contains(sender) == true else { return }
        Task { @MainActor [weak self, weak view] in
            guard let self, let view, view === self.editorWebView, self.editorWindow?.isKeyWindow == true else { return }
            if command == "focus-next" {
                let focused = self.editorWindow?.firstResponder as? NSView
                if focused !== view && focused?.isDescendant(of: view) != true { self.editorWindow?.makeFirstResponder(view) }
            }
            _ = try? await view.callAsyncJavaScript("return window.keynopeWorkspaceCommand?.(command) ?? false", arguments: ["command": command], in: nil, contentWorld: .page)
        }
    }

    private func updateEditorStyleCommands(_ flags: [String: Bool] = [:]) {
        let ready = appMode && editorWindow?.isKeyWindow == true
        for (key, item) in styleMenuItems { item.isEnabled = ready && flags[key] == true }
    }

    private func refreshEditorStyleCommands() {
        guard appMode, editorWindow?.isKeyWindow == true, let view = editorWebView else {
            updateEditorStyleCommands()
            return
        }
        view.evaluateJavaScript("window.keynopeStyleCommandState?.() || {}") { [weak self, weak view] result, _ in
            MainActor.assumeIsolated {
                guard let self, let view, view === self.editorWebView else { return }
                self.updateEditorStyleCommands(result as? [String: Bool] ?? [:])
            }
        }
    }

    @objc private func performEditorStyleCommand(_ sender: NSMenuItem) {
        guard appMode, editorWindow?.isKeyWindow == true, let view = editorWebView,
              let key = sender.identifier?.rawValue, styleMenuItems[key] === sender else { return }
        let parts = key.split(separator: ".")
        guard let command = parts.last else { return }
        let context = parts.count == 3 ? "shape-label" : ""
        // Recheck the live shared state inside JS. A cached enabled menu item
        // cannot bypass an active caret, modal, gesture or changed selection.
        Task { @MainActor [weak self, weak view] in
            guard let self, let view, view === self.editorWebView,
                  self.editorWindow?.isKeyWindow == true else { return }
            do {
                _ = try await view.callAsyncJavaScript("return await window.keynopeStyleCommand?.(command, context) ?? false",
                    arguments: ["command": String(command), "context": context], in: nil, contentWorld: .page)
            } catch {
                guard view === self.editorWebView else { return }
                _ = try? await view.evaluateJavaScript("window.showEngagementToast?.('Could not change style. Try again.')")
            }
            self.refreshEditorStyleCommands()
        }
    }

    private func editorClipboardCommand(_ command: String) {
        #if KEYNOPE_NATIVE_ACCEPTANCE
        nativeOrdinaryClipboardDeliveries += 1
        #endif
        guard let editorWebView, editorWindow?.isKeyWindow == true else {
            NSApp.sendAction(Selector((command + ":")), to: nil, from: nil)
            return
        }
        let script = """
        (() => {
          const target=document.activeElement;
          if(target&&(target.matches('input,textarea')||target.isContentEditable))return false;
          if(document.querySelector('dialog[open]')||typeof window.keynopeClipboardCommand!=='function')return false;
          window.keynopeClipboardCommand('\(command)').catch(error=>window.showEngagementToast?.(error.message||'Could not use the clipboard'));
          return true;
        })()
        """
        editorWebView.evaluateJavaScript(script) { result, error in
            MainActor.assumeIsolated {
                if error != nil || (result as? Bool) != true {
                    NSApp.sendAction(Selector((command + ":")), to: nil, from: nil)
                }
            }
        }
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

    @objc private func configureAutomation() {
        if automationSettings == nil { automationSettings = AutomationSettingsController(service: automationService) }
        automationSettings?.showWindow(nil)
        automationSettings?.window?.makeKeyAndOrderFront(nil)
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
            orderEditorWindow(editorWindow)
            return
        }
        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "keynopeInput")
        config.userContentController.add(self, name: "keynopePresenter")
        config.userContentController.addScriptMessageHandler(self, contentWorld: .page, name: "keynopeClipboard")
        config.userContentController.addScriptMessageHandler(self, contentWorld: .page, name: "keynopeFonts")
        installAppearanceBootstrap(config.userContentController)
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
        newWindow.tabbingMode = .disallowed
        newWindow.contentView = view
        newWindow.isReleasedWhenClosed = false
        newWindow.delegate = self
        newWindow.minSize = NSSize(width: 960, height: 640)
        newWindow.setFrameAutosaveName("KeynopeEditorWindow")
        newWindow.center()
        orderEditorWindow(newWindow)
        editorWindow = newWindow
        editorWebView = view
        updateEditorWindowTitle()
    }

    private func orderEditorWindow(_ window: NSWindow) {
#if KEYNOPE_NATIVE_ACCEPTANCE
        if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_BACKGROUND"] == "1" {
            window.orderBack(nil)
            return
        }
#endif
        window.makeKeyAndOrderFront(nil)
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
            .mpeg4Movie,
            .quickTimeMovie,
            UTType(filenameExtension: "svg"),
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
        panel.title = "Open a Keynope or PowerPoint Presentation"
        panel.prompt = "Open"
        panel.allowedContentTypes = ["md", "markdown", "pptx"].compactMap { UTType(filenameExtension: $0) }
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
            if url.pathExtension.caseInsensitiveCompare("pptx") == .orderedSame {
                guard let importedURL = importDeckHandler?(url.path) else { return }
                noPresentation()
                presenterURL = importedURL
                editorWebView?.load(URLRequest(url: editorURL()))
                participantTabsMenuItem?.isEnabled = false
                activityQRMenuItem?.isEnabled = false
                documentDirty = true
                saveMenuItem?.isEnabled = true
                updateEditorWindowTitle()
            } else {
                replaceDeck(path: url.path)
            }
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
        guard !choosingTemplate, let webView = editorWebView else { return }
        choosingTemplate = true
        Task { [weak self] in
            guard let self else { return }
            defer { self.choosingTemplate = false }
            do {
                let chosen = try await webView.callAsyncJavaScript(
                    "return await window.keynopeChooseTemplate?.();",
                    arguments: [:], in: nil, contentWorld: .page
                )
                guard let templateID = chosen as? String, !templateID.isEmpty else { return }
                _ = self.requestDocumentReplacement { [weak self] in
                    self?.openNewDeck(templateID: templateID)
                }
            } catch {
                self.showCaptureError(error)
            }
        }
    }

    private func openNewDeck(templateID: String? = nil) {
        guard let newURL = newDeckHandler?(templateID) else {
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

    private struct ExportDocumentResponse: Decodable, Sendable {
        let content: String
        let fallbackContent: String?
        let report: ExportConversionReport?
    }

    private struct ExportConversionReport: Decodable, Sendable {
        let warnings: [ExportConversionWarning]?
    }

    private struct ExportConversionWarning: Decodable, Sendable {
        let slide: Int?
        let object: String?
        let code: String
        let message: String
        let feature: String?
        let severity: String?
        let fallback: String?
        let remediation: String?
    }

    private func showExportConversionReport(_ report: ExportConversionReport?) {
        guard let warnings = report?.warnings, !warnings.isEmpty else { return }
        let alert = NSAlert()
        alert.messageText = "PowerPoint Exported with Conversion Notes"
        alert.informativeText = "The file was saved. Review these notes before sharing it; some content may be pictures, static, approximated or omitted."
        let scroll = NSScrollView(frame: NSRect(x: 0, y: 0, width: 540, height: 260))
        scroll.hasVerticalScroller = true
        scroll.hasHorizontalScroller = false
        let text = NSTextView(frame: scroll.bounds)
        text.isEditable = false
        text.isSelectable = true
        text.isRichText = false
        text.isVerticallyResizable = true
        text.isHorizontallyResizable = false
        text.minSize = NSSize(width: 0, height: scroll.contentSize.height)
        text.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        text.autoresizingMask = [.width]
        text.textContainer?.containerSize = NSSize(width: scroll.contentSize.width, height: CGFloat.greatestFiniteMagnitude)
        text.textContainer?.widthTracksTextView = true
        text.font = NSFont.systemFont(ofSize: NSFont.smallSystemFontSize)
        text.string = warnings.map { warning in
            let location = [warning.slide.flatMap { $0 >= 0 ? "Slide \($0 + 1)" : nil }, warning.object].compactMap { $0 }.joined(separator: " · ")
            let policy = [warning.feature, warning.severity, warning.fallback.map { "Fallback: " + $0.replacingOccurrences(of: "-", with: " ") }].compactMap { $0 }.joined(separator: " · ")
            return (location.isEmpty ? "Document\n" : location + "\n") + warning.message + (policy.isEmpty ? "" : "\n" + policy) + (warning.remediation.map { "\n" + $0 } ?? "") + "\n[" + warning.code + "]"
        }.joined(separator: "\n\n")
        scroll.documentView = text
        alert.accessoryView = scroll
        alert.addButton(withTitle: "OK")
        alert.runModal()
    }

    private func editorEndpoint(_ path: String) -> URL? {
        var components = URLComponents(url: presenterURL, resolvingAgainstBaseURL: false)
        let relative = URLComponents(string: path)
        components?.path = relative?.path ?? path
        components?.percentEncodedQuery = relative?.percentEncodedQuery
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
    @objc private func exportPPTX() {
        guard !exportingSlides else { return }
        let panel = NSSavePanel()
        panel.title = "Export Keynope Presentation as PowerPoint"
        panel.prompt = "Export"
        panel.allowedContentTypes = [UTType(filenameExtension: "pptx")].compactMap { $0 }
        panel.canCreateDirectories = true
        configureDocumentDestinationPanel(panel, extension: "pptx")
        guard runDocumentPanel(panel) == .OK,
              let destination = panel.url?.standardizedFileURL,
              let endpoint = editorEndpoint("/api/editor/export-pptx") else { return }
        exportingSlides = true
        slideExportTask = Task {
            let progress = SlideExportProgress()
            progress.onCancel = { [weak self] in
                self?.slideExportTask?.cancel()
                self?.editorWebView?.evaluateJavaScript("window.keynopeCancelPPTXCapture?.()", completionHandler: nil)
            }
            progress.update("Preparing PowerPoint…")
            progress.show()
            defer {
                progress.close()
                exportingSlides = false
                slideExportTask = nil
            }
            do {
                progress.hide()
                guard try await editorWebView?.callAsyncJavaScript("return await window.keynopeConfirmFontExport('pptx');", arguments: [:], in: nil, contentWorld: .page) as? Bool == true else { return }
                progress.show()
                let writer = try SlideExportWriter(destination: destination)
                defer { writer.discard() }
                guard let artwork = try await editorWebView?.callAsyncJavaScript("return await window.keynopePreparePPTXExport();", arguments: [:], in: nil, contentWorld: .page) as? String else {
                    throw NSError(domain: "sh.keynope.app", code: 33, userInfo: [NSLocalizedDescriptionKey: "The presentation artwork could not be prepared."])
                }
                try progress.checkCancellation()
                progress.update("Converting presentation…")
                var request = URLRequest(url: endpoint)
                request.httpMethod = "POST"
                request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                request.httpBody = Data(artwork.utf8)
                let (data, response) = try await URLSession.shared.data(for: request)
                try progress.checkCancellation()
                guard (response as? HTTPURLResponse)?.statusCode == 200 else {
                    throw NSError(domain: "sh.keynope.app", code: 31, userInfo: [NSLocalizedDescriptionKey: String(data: data, encoding: .utf8) ?? "The PowerPoint presentation could not be exported."])
                }
                progress.update("Preparing PowerPoint file…")
                // Large base64 responses must not monopolise AppKit's executor.
                let decoding = Task.detached(priority: .userInitiated) {
                    try Task.checkCancellation()
                    let document = try JSONDecoder().decode(ExportDocumentResponse.self, from: data)
                    try Task.checkCancellation()
                    guard let bytes = Data(base64Encoded: document.content), bytes.starts(with: [0x50, 0x4b]) else {
                        throw NSError(domain: "sh.keynope.app", code: 32, userInfo: [NSLocalizedDescriptionKey: "Keynope produced an invalid PowerPoint file."])
                    }
                    try Task.checkCancellation()
                    return (bytes, document.report)
                }
                let (bytes, report) = try await withTaskCancellationHandler {
                    try await decoding.value
                } onCancel: { decoding.cancel() }
                try progress.checkCancellation()
                try await writer.stage(bytes) { completed, total in
                    try await MainActor.run {
                        try progress.checkCancellation()
                        progress.update("Writing PowerPoint…", completed: completed, total: total)
                    }
                }
                await Task.yield()
                try progress.beginCommit()
                try writer.commit()
                progress.close()
                _ = try? await editorWebView?.evaluateJavaScript("window.keynopeDidExport && window.keynopeDidExport()")
                showExportConversionReport(report)
            } catch {
                if progress.cancelled || Task.isCancelled || error is CancellationError { return }
                progress.hide()
                let alert = NSAlert(error: error)
                alert.messageText = "Could Not Export PowerPoint Presentation"
                alert.runModal()
            }
        }
    }

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
            if let index=step.accessoryIndex,let popup=panel.accessoryView as? NSPopUpButton { popup.selectItem(at:index) }
            // The system disallows programmatic OK on macOS 10.15+. Synthetic
            // modal completion tests app flow only, not a sandbox access grant.
            let timer = Timer(timeInterval: 0.15, repeats: false) { _ in
                MainActor.assumeIsolated {
                    if step.manualConfirmation { return }
                    NSApp.stopModal(withCode: step.response)
                    panel.orderOut(nil)
                }
            }
            RunLoop.main.add(timer, forMode: .common)
            let watchdog = Timer(timeInterval: step.manualConfirmation ? 60 : 15, repeats: false) { _ in
                MainActor.assumeIsolated {
                    self.nativePanelAcceptanceFailure = "Timed out confirming the native Save panel"
                    panel.cancel(nil)
                }
            }
            RunLoop.main.add(watchdog, forMode: .common)
            defer { watchdog.invalidate(); timer.invalidate() }
            let response=panel.runModal()
            if response != step.response {
                nativePanelAcceptanceFailure = nativePanelAcceptanceFailure ?? "Save panel returned \(response.rawValue) instead of \(step.response.rawValue)"
            }
            if !(panel is NSOpenPanel), response == .OK, let expected=step.destination,
               panel.url.map({$0.deletingLastPathComponent().resolvingSymlinksInPath().standardizedFileURL.appendingPathComponent($0.lastPathComponent).path}) != expected.deletingLastPathComponent().resolvingSymlinksInPath().standardizedFileURL.appendingPathComponent(expected.lastPathComponent).path {
                nativePanelAcceptanceFailure = "Save panel selected \(panel.url?.path ?? "nil") instead of \(expected.path)"
                return .cancel
            }
            if step.manualConfirmation, response == .OK { nativePanelAcceptanceSelectedURL = panel.url }
            return response
        }
#endif
        return panel.runModal()
    }

    private func exportSlides(pdf: Bool) {
        guard !exportingSlides,
            let endpoint = editorEndpoint(pdf ? "/api/editor/export-pdf" : "/api/editor/export-document?format=png")
        else { return }
        exportingSlides = true
        slideExportTask = Task {
            let renderer = SlideExportRenderer()
            var fallbackRenderer: SlideExportRenderer?
            let progress = SlideExportProgress()
            progress.onCancel = { [weak self, weak renderer] in
                self?.slideExportTask?.cancel()
                renderer?.close()
                fallbackRenderer?.close()
            }
            progress.show()
            defer {
                progress.close(); renderer.close(); fallbackRenderer?.close(); exportingSlides = false;
                slideExportTask = nil
            }
            do {
                if pdf {
                    progress.hide()
                    guard try await editorWebView?.callAsyncJavaScript("return await window.keynopeConfirmFontExport('pdf');", arguments: [:], in: nil, contentWorld: .page) as? Bool == true else { return }
                    progress.show()
                }
                _ = try await editorWebView?.callAsyncJavaScript(
                    "await window.keynopePrepareDocumentSave?.();", arguments: [:], in: nil, contentWorld: .page)
                var request = URLRequest(url: endpoint)
                request.httpMethod = "POST"
                request.setValue("application/json", forHTTPHeaderField: "Content-Type")
                request.httpBody = try JSONSerialization.data(withJSONObject: ["path": "Snapshot.html"])
                let (data, response) = try await URLSession.shared.data(for: request)
                try progress.checkCancellation()
                guard (response as? HTTPURLResponse)?.statusCode == 200 else {
                    throw NSError(
                        domain: "sh.keynope.export", code: 5,
                        userInfo: [NSLocalizedDescriptionKey: "The presentation could not be prepared for export."])
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
                if pdf {
                    selector.addItems(withTitles: [
                        "Quality - selectable text and vectors", "Exact appearance - image pages",
                    ])
                    selector.setAccessibilityLabel("PDF rendering")
                    panel.accessoryView = selector
                } else {
                    for page in pages { selector.addItem(withTitle: "Slide " + (page["label"] as? String ?? "")) }
                    selector.setAccessibilityLabel("Slide to export")
                    panel.accessoryView = selector
                }
                panel.message =
                    "Static slide artwork only. Activity results and generated join codes are excluded. Animations use their first frame."
                guard runDocumentPanel(panel) == .OK, let destination = panel.url else { return }
                let writer = try SlideExportWriter(destination: destination)
                defer { writer.discard() }
                progress.show()
                try progress.checkCancellation()
                if pdf {
                    let output = SlidePDFEncoder(title: name)
                    var fallbackLabels: [String] = []
                    for (position, page) in pages.enumerated() {
                        try progress.checkCancellation()
                        progress.update(
                            "Rendering slide \(position + 1) of \(pages.count)…", completed: position,
                            total: pages.count)
#if KEYNOPE_NATIVE_ACCEPTANCE
                        if nativeSlideExportCancelPhase == "render" { progress.cancel() }
#endif
                        try progress.checkCancellation()
                        guard let index = page["index"] as? Int else { continue }
                        let width = (page["width"] as? Double) ?? 1920
                        let height = (page["height"] as? Double) ?? 1080
                        let aspect = width / height
                        try renderer.setPageSize(width: width, height: height)
                        var captured = false
                        if selector.indexOfSelectedItem == 0 && (page["quality"] as? Bool ?? false) {
                            do {
                                let bytes = try await renderer.pdfData(page: index)
                                try progress.checkCancellation()
                                try await output.append(pdfData: bytes, expectedAspectRatio: aspect)
                                captured = true
                            } catch {
                                try progress.checkCancellation()
                                if error is CancellationError { throw error }
                            }
                        }
                        if !captured {
                            if fallbackRenderer == nil {
                                guard let fallback = document.fallbackContent else {
                                    throw CocoaError(.fileReadCorruptFile)
                                }
                                let fallbackView = SlideExportRenderer()
                                fallbackRenderer = fallbackView
                                try await fallbackView.load(fallback)
                            }
                            guard let fallbackRenderer else { throw CocoaError(.fileReadCorruptFile) }
                            try fallbackRenderer.setPageSize(width: width, height: height)
                            let bytes = try await fallbackRenderer.imageData(page: index)
                            try progress.checkCancellation()
                            try await output.append(imageData: bytes)
                            if selector.indexOfSelectedItem == 0 {
                                fallbackLabels.append(page["label"] as? String ?? String(position + 1))
                            }
                        }
                    }
                    progress.update("Writing PDF…", completed: pages.count, total: pages.count)
                    await Task.yield()
                    try progress.checkCancellation()
                    let bytes = try await output.finish()
                    try progress.checkCancellation()
                    try await writer.stage(bytes) { completed, total in
                        try await MainActor.run {
                            try progress.checkCancellation()
                            progress.update("Writing PDF…", completed: completed, total: total)
#if KEYNOPE_NATIVE_ACCEPTANCE
                            if self.nativeSlideExportCancelPhase == "write" { progress.cancel() }
#endif
                            try progress.checkCancellation()
                        }
                    }
                    await Task.yield()
#if KEYNOPE_NATIVE_ACCEPTANCE
                    if nativeSlideExportCancelPhase == "commit" { progress.cancel() }
#endif
                    try progress.beginCommit()
                    try writer.commit()
#if KEYNOPE_NATIVE_ACCEPTANCE
                    nativePanelAcceptancePDFBytes = bytes
#endif
                    if !fallbackLabels.isEmpty {
                        _ = try? await editorWebView?.callAsyncJavaScript(
                            "window.showEngagementToast?.(message,4000)",
                            arguments: [
                                "message": "PDF saved. Exact artwork used for slide"
                                    + (fallbackLabels.count == 1 ? " " : "s ") + fallbackLabels.joined(separator: ", ")
                                    + "."
                            ], in: nil, contentWorld: .page)
                    }
                } else {
                    progress.update("Rendering selected slide…", completed: 0)
                    let selectedPage = pages[selector.indexOfSelectedItem]
                    guard let index = selectedPage["index"] as? Int else {
                        throw CocoaError(.fileWriteUnknown)
                    }
                    try renderer.setPageSize(
                        width: (selectedPage["width"] as? Double) ?? 1920,
                        height: (selectedPage["height"] as? Double) ?? 1080)
                    let tiff = try await renderer.imageData(page: index)
                    try progress.checkCancellation()
                    guard let bitmap = NSBitmapImageRep(data: tiff),
                        let bytes = bitmap.representation(using: .png, properties: [:])
                    else { throw CocoaError(.fileWriteUnknown) }
                    try await writer.stage(bytes) { completed, total in
                        try await MainActor.run {
                            try progress.checkCancellation()
                            progress.update("Writing PNG…", completed: completed, total: total)
                        }
                    }
                    await Task.yield()
                    try progress.beginCommit()
                    try writer.commit()
                }
                _ = try? await editorWebView?.evaluateJavaScript("window.keynopeDidExport?.()")
            } catch {
                if progress.cancelled || Task.isCancelled || error is CancellationError { return }
#if KEYNOPE_NATIVE_ACCEPTANCE
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_PDF_ACCEPTANCE"] == "1" {
                    nativeSlideExportFailure = error.localizedDescription
                    return
                }
#endif
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
                guard try await editorWebView?.callAsyncJavaScript("return await window.keynopeConfirmFontExport('html');", arguments: [:], in: nil, contentWorld: .page) as? Bool == true else { return }
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
                    let approved = try await webView.callAsyncJavaScript("return await window.keynopeConfirmFontExport?.('save') ?? true;", arguments: [:], in: nil, contentWorld: .page)
                    if (approved as? Bool) == false {
                        self.savingDocument = false
                        self.saveMenuItem?.isEnabled = self.documentDirty
                        self.cancelDeferredSaveContinuation()
                        return
                    }
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
        config.mediaTypesRequiringUserActionForPlayback = []
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
        if menu === workspaceMenu {
            let ready = appMode && editorWindow?.isKeyWindow == true
            for item in menu.items where !item.isSeparatorItem { item.isEnabled = ready }
            guard ready, let view = editorWebView else { return }
            view.evaluateJavaScript("window.keynopeWorkspaceState?.() || {enabled:false}") { [weak self, weak view, weak menu] result, _ in
                MainActor.assumeIsolated {
                    guard let self, let view, let menu, view === self.editorWebView else { return }
                    let state = result as? [String: Bool] ?? [:]
                    for item in menu.items where !item.isSeparatorItem {
                        item.isEnabled = self.editorWindow?.isKeyWindow == true && state["enabled"] == true
                        item.state = state[item.identifier?.rawValue ?? ""] == true ? .on : .off
                    }
                }
            }
            return
        }
        if menu === styleMenu {
            refreshEditorStyleCommands()
            return
        }
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

    // Only the trusted editor main frame can access object/style clipboards.
    // Audience windows and URL-tab iframes never receive this capability.
    nonisolated func userContentController(
        _ userContentController: WKUserContentController,
        didReceive message: WKScriptMessage,
        replyHandler: @escaping KeynopeScriptReplyHandler
    ) {
        MainActor.assumeIsolated {
            let expected = editorURL()
            guard appMode, ["keynopeClipboard", "keynopeFonts"].contains(message.name), message.webView === editorWebView,
                  message.frameInfo.isMainFrame, let source = message.frameInfo.request.url,
                  source.scheme == expected.scheme, source.host == expected.host,
                  source.port == expected.port, source.path == expected.path,
                  let body = message.body as? [String: Any], let action = body["action"] as? String else {
                replyHandler(nil, "Clipboard access is restricted to the Keynope editor.")
                return
            }
            if message.name == "keynopeFonts" {
                do {
                    if action == "catalogue" { replyHandler(Self.installedFontCatalogue(), nil) }
                    else if action == "read", let name = body["postScriptName"] as? String { replyHandler(try Self.installedTypeface(name), nil) }
                    else { replyHandler(nil, "Unsupported font request.") }
                } catch { replyHandler(nil, error.localizedDescription) }
                return
            }
            if action == "style-read" {
                replyHandler(styleClipboardData, nil)
                return
            }
            if action == "style-write" {
                guard let value = body["data"] as? String, value.utf8.count <= 8192,
                      let bytes = value.data(using: .utf8),
                      let snapshot = (try? JSONSerialization.jsonObject(with: bytes)) as? [String: Any],
                      snapshot["version"] as? Int == 1,
                      let fields = snapshot["fields"] as? [String: Any],
                      !fields.isEmpty, fields.count <= 32 else {
                    replyHandler(nil, "Invalid or oversized style clipboard.")
                    return
                }
                // Go validates every field again before applying the snapshot.
                styleClipboardData = value
                replyHandler(true, nil)
                return
            }
            let board = NSPasteboard.general
            let objectsType = NSPasteboard.PasteboardType("sh.keynope.objects.v1")
            let maximumBytes = 65 * 1024 * 1024
            if action == "text-write", let text = body["text"] as? String {
                guard text.utf8.count <= maximumBytes else {
                    replyHandler(nil, "Clipboard text exceeds the supported size.")
                    return
                }
                board.clearContents()
                guard board.setString(text, forType: .string) else {
                    replyHandler(nil, "Could not write clipboard text.")
                    return
                }
                replyHandler(true, nil)
            } else if action == "read" {
                let value = board.string(forType: objectsType) ?? ""
                let text = board.string(forType: .string) ?? ""
                let html = board.string(forType: .html) ?? ""
                guard value.utf8.count <= maximumBytes, text.utf8.count <= maximumBytes,
                      html.utf8.count <= 96 * 1024 * 1024 else {
                    replyHandler(nil, "Clipboard contents exceed the supported size.")
                    return
                }
                replyHandler(["data": value, "text": text, "html": html], nil)
            } else if action == "write", let value = body["data"] as? String, let text = body["text"] as? String,
                      let html = body["html"] as? String {
                guard value.hasPrefix("keynope-elements:"), value.utf8.count <= maximumBytes,
                      text.utf8.count <= maximumBytes, html.utf8.count <= 96 * 1024 * 1024 else {
                    replyHandler(nil, "Invalid or oversized object clipboard.")
                    return
                }
                let item = NSPasteboardItem()
                item.setString(value, forType: objectsType)
                item.setString(text, forType: .string)
                item.setString(html, forType: .html)
                board.clearContents()
                guard board.writeObjects([item]) else {
                    replyHandler(nil, "Could not write the system clipboard.")
                    return
                }
                replyHandler(true, nil)
            } else {
                replyHandler(nil, "Unsupported clipboard operation.")
            }
        }
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
            if action == "interface-appearance" {
                let expected = editorURL()
                guard appMode, message.webView === editorWebView, message.frameInfo.isMainFrame,
                      let source = message.frameInfo.request.url,
                      source.scheme == expected.scheme, source.host == expected.host,
                      source.port == expected.port, source.path == expected.path,
                      let mode = body["mode"] as? String else { return }
                applyInterfaceAppearance(mode)
            } else if action == "editor-style-commands" {
                let expected = editorURL()
                guard appMode, message.webView === editorWebView,
                      message.frameInfo.isMainFrame, let source = message.frameInfo.request.url,
                      source.scheme == expected.scheme, source.host == expected.host,
                      source.port == expected.port, source.path == expected.path else {
                    #if KEYNOPE_NATIVE_ACCEPTANCE
                    nativeDeniedStyleMessages += 1
                    #endif
                    return
                }
                updateEditorStyleCommands(body["flags"] as? [String: Bool] ?? [:])
            } else if action == "stop" {
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
                throw NativeAcceptanceError(message: "Timed out waiting for \(label); deck=\(activeDeckURLHandler?()?.path ?? "Untitled"), loading=\(editorWebView?.isLoading ?? false), dirty=\(documentDirty)")
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
            if !view.isLoading, (try? await view.callAsyncJavaScript("return await (\(expression));",arguments:[:],in:nil,contentWorld:.page) as? Bool) == true { return }
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
        response: NSApplication.ModalResponse,
        accessoryIndex: Int? = nil,
        manualConfirmation: Bool = false
    ) {
        nativePanelAcceptanceFailure = nil
        nativePanelAcceptanceStep = NativePanelAcceptanceStep(title: title, destination: destination, response: response,accessoryIndex:accessoryIndex,manualConfirmation:manualConfirmation)
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

    private func verifyNativeObjectClipboard(checks: inout [String], element: Int = 0, useMenuShortcuts: Bool = true) async throws {
        guard let view = editorWebView else { throw NativeAcceptanceError(message: "Missing clipboard test view") }
        let board = NSPasteboard.general
        let savedItems = (board.pasteboardItems ?? []).map { item in
            item.types.compactMap { type in item.data(forType: type).map { (type, $0) } }
        }
        defer {
            let restored = savedItems.map { values in
                let item = NSPasteboardItem()
                for (type, data) in values { item.setData(data, forType: type) }
                return item
            }
            board.clearContents()
            if !restored.isEmpty { board.writeObjects(restored) }
        }
        try await postNativeAcceptanceEditorAction(["action": "select-element", "element": element])
        view.reload()
        try await waitForNativeJavaScript("clipboard editor reload", in: view, expression: "typeof window.keynopeClipboardCommand === 'function' && !!document.querySelector('[data-object-id]')")
        let before = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.length;", contentWorld: .page) as? Int
        board.clearContents()
        if useMenuShortcuts {
            editorWindow?.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            try await waitForNativeAcceptance("clipboard Copy editor key window") { self.editorWindow?.isKeyWindow == true }
            try requireNativeAcceptance(try performNativeShortcut("c", keyCode: 8, modifiers: [.command]), "Command-C routed through the native Edit menu", checks: &checks)
        } else {
            _ = try await view.callAsyncJavaScript("await window.keynopeClipboardCommand('copy');return true;", contentWorld: .page)
        }
        try await waitForNativeAcceptance("native object Copy") {
            board.string(forType: NSPasteboard.PasteboardType("sh.keynope.objects.v1"))?.hasPrefix("keynope-elements:") == true
        }
        let custom = board.string(forType: NSPasteboard.PasteboardType("sh.keynope.objects.v1")) ?? ""
        try requireNativeAcceptance(custom.hasPrefix("keynope-elements:{"), "native object Copy wrote the typed system clipboard", checks: &checks)
        let plain = board.string(forType: .string) ?? ""
        try requireNativeAcceptance(!plain.isEmpty && !plain.hasPrefix("keynope-elements:"), "native object Copy retained a readable plain-text fallback", checks: &checks)
        let html = board.string(forType: .html) ?? ""
        try requireNativeAcceptance(html.contains("data-keynope-objects=\""), "native object Copy provided portable HTML clipboard data", checks: &checks)
        view.reload() // Discard the JavaScript in-memory clipboard deliberately.
        try await waitForNativeJavaScript("clipboard paste reload", in: view, expression: "typeof window.keynopeClipboardCommand === 'function' && !!document.querySelector('[data-object-id]')")
        let renderedBefore = try await view.evaluateJavaScript("document.querySelectorAll('#stage .keynope-modern-scene > [data-object-id]').length") as? Int ?? 0
        if useMenuShortcuts {
            editorWindow?.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            try await waitForNativeAcceptance("clipboard editor key window") { self.editorWindow?.isKeyWindow == true }
            try requireNativeAcceptance(try performNativeShortcut("v", keyCode: 9, modifiers: [.command]), "Command-V routed through the native Edit menu", checks: &checks)
        } else {
            _ = try await view.callAsyncJavaScript("await window.keynopeClipboardCommand('paste');return true;", contentWorld: .page)
        }
        do {
            try await waitForNativeJavaScript("native menu Paste", in: view, expression: "document.querySelectorAll('#stage .keynope-modern-scene > [data-object-id]').length === \(renderedBefore + 1)")
        } catch {
            let detail = try? await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify({elements:s.slides[s.current].elements.length,stageObjects:document.querySelectorAll('#stage .keynope-modern-scene > [data-object-id]').length,activeTag:document.activeElement?.tagName,dialogs:document.querySelectorAll('dialog[open]').length,status:document.querySelector('.keynope-editor-status')?.textContent,toasts:[...document.querySelectorAll('[role=status]')].map(n=>n.textContent)});", contentWorld: .page) as? String
            throw NativeAcceptanceError(message: "Native menu Paste did not render (before=\(renderedBefore), \(detail ?? "no diagnostics"))")
        }
        let pasted = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.length;", contentWorld: .page) as? Int
        try requireNativeAcceptance(before != nil && pasted == before.map { $0 + 1 }, "native object Paste survived a WebView reload without the in-memory fallback", checks: &checks)
        try await postNativeAcceptanceEditorAction(["action": "undo"])
        let undone = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.length;", contentWorld: .page) as? Int
        try requireNativeAcceptance(undone == before, "native object Paste was one undoable transaction", checks: &checks)
        board.clearContents()
        board.setString(html, forType: .html)
        board.setString(plain, forType: .string)
        view.reload()
        try await waitForNativeJavaScript("HTML clipboard reload", in: view, expression: "typeof window.keynopeClipboardCommand === 'function' && !!document.querySelector('[data-object-id]')")
        _ = try await view.callAsyncJavaScript("await window.keynopeClipboardCommand('paste');return true;", contentWorld: .page)
        let htmlPasted = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.length;", contentWorld: .page) as? Int
        try requireNativeAcceptance(htmlPasted == before.map { $0 + 1 }, "native object Paste accepted the browser-compatible HTML transport", checks: &checks)
    }

    private func verifyNativeSelectionProperties(checks: inout [String]) async throws {
        guard let view = editorWebView else { throw NativeAcceptanceError(message: "Missing selection test view") }
        let index = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.findIndex(e=>['text','heading','bullet','code'].includes(e.kind));", contentWorld: .page) as? Int ?? -1
        guard index >= 0 else { throw NativeAcceptanceError(message: "Missing acceptance text object") }
        try await postNativeAcceptanceEditorAction(["action": "select-element", "element": index])
        // This setup action bypasses the WebView's normal action-response path.
        // Reload to consume its selection before driving the actual controls.
        view.reload()
        try await waitForNativeJavaScript("selection property inspector", in: view, expression: "!!document.querySelector('input[data-selection-property=\"text.size\"]')")
        let valid = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['text.size'].editable===true && s.selectionCapabilities.revision===s.version;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(valid, "native selection receives the shared revisioned capability contract", checks: &checks)
        _ = try await view.evaluateJavaScript("const input=document.querySelector('input[data-selection-property=\"text.size\"]');input.value='180';input.dispatchEvent(new Event('change',{bubbles:true}));")
        try await waitForNativeJavaScript("accepted native font size", in: view, expression: "document.querySelector('input[data-selection-property=\"text.size\"]')?.value==='180'")
        let accepted = try await view.callAsyncJavaScript("for(let i=0;i<100;i++){const s=await fetch('/api/editor/state').then(r=>r.json());if(s.selectionCapabilities?.properties['text.size'].value===180)return true;await new Promise(resolve=>setTimeout(resolve,50));}return false;", contentWorld: .page) as? Bool ?? false
        if !accepted {
            let detail = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify({version:s.version,applied:window.keynopeWorkspaceAppliedRevision,capabilities:s.selectionCapabilities,field:document.querySelector('input[data-selection-property=\"text.size\"]')?.value,status:document.querySelector('.keynope-editor-status')?.textContent});", contentWorld: .page) as? String ?? "no state"
            throw NativeAcceptanceError(message: "Native font field did not commit: \(detail)")
        }
        try requireNativeAcceptance(accepted, "native font field commits and displays the accepted property value", checks: &checks)
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Increase font size\"]').click()")
        try await waitForNativeJavaScript("native font size increment", in: view, expression: "document.querySelector('input[data-selection-property=\"text.size\"]')?.value==='181'")
        try requireNativeAcceptance(true, "native plus control immediately updates the adjacent font field", checks: &checks)
        try await postNativeAcceptanceEditorAction(["action": "undo"])
        try await waitForNativeJavaScript("native property undo", in: view, expression: "document.querySelector('input[data-selection-property=\"text.size\"]')?.value==='180'")
        try requireNativeAcceptance(true, "native property undo restores the accepted inspector value", checks: &checks)
        _ = try await view.evaluateJavaScript("document.getElementById('keynope-ribbon-tab-arrange').click()")
        try await waitForNativeJavaScript("native geometry fields", in: view, expression: "!!document.querySelector('input[aria-label=\"Object x\"]')")
        _ = try await view.evaluateJavaScript("for(const [label,value]of [['Object x','35.125'],['Object y','4.25'],['Object width','100.5'],['Object height','10.25']])document.querySelector('input[aria-label=\"'+label+'\"]').value=value;document.querySelector('form[aria-label=\"Object geometry\"]').requestSubmit();")
        let geometry = try await view.callAsyncJavaScript("for(let i=0;i<100;i++){const s=await fetch('/api/editor/state').then(r=>r.json()),b=s.selectionCapabilities?.geometry;if(b?.x===35.125&&b.width===100.5&&b.height===10.25)return s.selectionCapabilities.properties['text.size'].value===180;await new Promise(resolve=>setTimeout(resolve,50));}return false;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(geometry, "native precision geometry commits fractional bounds without changing font size", checks: &checks)
        try await waitForNativeJavaScript("native accepted geometry fields", in: view, expression: "document.querySelector('input[aria-label=\"Object x\"]')?.value==='35.125'&&document.querySelector('input[aria-label=\"Object width\"]')?.value==='100.5'")
        try requireNativeAcceptance(true, "native precision fields display accepted bounds immediately", checks: &checks)
        try await waitForNativeJavaScript("native rotation control", in: view, expression: "!!document.querySelector('input[aria-label=\"Object rotation degrees\"]')")
        _ = try await view.evaluateJavaScript("(()=>{const input=document.querySelector('input[aria-label=\"Object rotation degrees\"]');input.value='22.5';input.dispatchEvent(new Event('change',{bubbles:true}));})()")
        let rotation = try await view.callAsyncJavaScript("for(let i=0;i<100;i++){const s=await fetch('/api/editor/state').then(r=>r.json());if(s.selectionCapabilities?.properties['geometry.rotation'].value===22.5)return true;await new Promise(resolve=>setTimeout(resolve,50));}return false;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(rotation, "native rotation uses the shared validated property command", checks: &checks)
        _ = try await view.evaluateJavaScript("document.getElementById('keynope-ribbon-tab-style').click()")
        try await waitForNativeJavaScript("native shared paint control", in: view, expression: "!!document.querySelector('[data-selection-property=\"paint.color\"]')")
        _ = try await view.evaluateJavaScript("document.querySelector('[data-selection-property=\"paint.color\"]').click()")
        try await waitForNativeJavaScript("native shared colour picker", in: view, expression: "!!document.querySelector('.keynope-colour-picker')")
        _ = try await view.evaluateJavaScript("(()=>{const p=document.querySelector('.keynope-colour-picker');p.querySelector('[aria-label=\"HTML color\"]').value='#AA55FF';[...p.querySelectorAll('button')].find(b=>b.textContent==='Apply').click();})()")
        try await waitForNativeJavaScript("native accepted paint colour", in: view, expression: "document.querySelector('[data-selection-property=\"paint.color\"]')?.getAttribute('aria-description')==='#aa55ff'")
        let paintAccepted = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['paint.color'].value==='#aa55ff'&&s.selectionCapabilities.properties['paint.color'].provenance==='explicit';", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(paintAccepted, "native shared paint picker returns canonical accepted colour and provenance", checks: &checks)
        _ = try await view.evaluateJavaScript("(()=>{const input=document.querySelector('[data-selection-property=\"paint.gradientDirection\"]');input.closest('details').open=true;input.value='diagonal';input.dispatchEvent(new Event('change',{bubbles:true}));})()")
        try await waitForNativeJavaScript("native gradient enabled", in: view, expression: "document.querySelector('[data-selection-property=\"paint.gradientDirection\"]')?.value==='diagonal'")
        let gradientAccepted = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities?.properties['paint.gradientStart'].value==='#55aaff'&&s.selectionCapabilities.properties['paint.gradientEnd'].value==='#ff55aa';", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(gradientAccepted, "native gradient enabling commits valid colour stops together", checks: &checks)
        _ = try await view.evaluateJavaScript("(()=>{const input=document.querySelector('[data-selection-property=\"paint.gradientDirection\"]');input.value='none';input.dispatchEvent(new Event('change',{bubbles:true}));})()")
        try await waitForNativeJavaScript("native gradient off", in: view, expression: "document.querySelector('[data-selection-property=\"paint.gradientDirection\"]')?.value==='none'")
        try await postNativeAcceptanceEditorAction(["action": "undo"])
        try await waitForNativeJavaScript("native gradient undo", in: view, expression: "document.querySelector('[data-selection-property=\"paint.gradientDirection\"]')?.value==='diagonal'")
        try requireNativeAcceptance(true, "native shared paint off is undoable without losing colour stops", checks: &checks)
        try await waitForNativeJavaScript("native copy style control", in: view, expression: "!!document.querySelector('[aria-label=\"Copy style\"]')")
        let copyVersion = try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version;", contentWorld: .page) as? Int ?? -1
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Copy style\"]').click()")
        try await waitForNativeJavaScript("native copied style", in: view, expression: "document.querySelector('[aria-label=\"Paste style\"]')?.disabled===false")
        let copiedStyle = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.version===\(copyVersion)&&s.styleClipboard?.fields['text.size'].value==='180';", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(copiedStyle, "native style copy captures formatting without a document revision", checks: &checks)
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Reset style\"]').click()")
        let resetStyle = try await view.callAsyncJavaScript("for(let i=0;i<100;i++){const s=await fetch('/api/editor/state').then(r=>r.json()),e=s.slides[s.current].elements[s.selected],q=new URLSearchParams(e.query);if(!q.has('modern-size'))return q.get('object-rotation')==='22.5'&&q.get('width')==='100.5';await new Promise(resolve=>setTimeout(resolve,50));}return false;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(resetStyle, "native reset style restores inheritance and preserves geometry", checks: &checks)
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Paste style\"]').click()")
        let pastedStyle = try await view.callAsyncJavaScript("for(let i=0;i<100;i++){const s=await fetch('/api/editor/state').then(r=>r.json());if(s.selectionCapabilities?.properties['text.size'].value===180)return true;await new Promise(resolve=>setTimeout(resolve,50));}return false;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(pastedStyle, "native paste style applies the shared captured formatting", checks: &checks)
        let retainedStyle = try await view.callAsyncJavaScript("const value=await window.webkit.messageHandlers.keynopeClipboard.postMessage({action:'style-read'});return JSON.parse(value).fields['text.size'].value==='180';", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(retainedStyle, "native style clipboard is retained in app memory", checks: &checks)
        // Replace both the Go engine session and WebView document, not just the DOM.
        openNewDeck(templateID: "clean-light")
        try await waitForNativeJavaScript("style recipient after document replacement", in: view, expression: "!!document.querySelector('[data-object-id]')")
        let newIndex = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.slides[s.current].elements.findIndex(e=>['text','heading','bullet','code'].includes(e.kind));", contentWorld: .page) as? Int ?? -1
        guard newIndex >= 0 else { throw NativeAcceptanceError(message: "Missing cross-deck style recipient") }
        try await postNativeAcceptanceEditorAction(["action": "select-element", "element": newIndex])
        view.reload()
        try await waitForNativeJavaScript("cross-deck style controls", in: view, expression: "!!document.getElementById('keynope-ribbon-tab-style')")
        _ = try await view.evaluateJavaScript("document.getElementById('keynope-ribbon-tab-style').click()")
        try await waitForNativeJavaScript("cross-deck paste style available", in: view, expression: "document.querySelector('[aria-label=\"Paste style\"]')?.disabled===false")
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Paste style\"]').click()")
        let crossDeckStyle = try await view.callAsyncJavaScript("for(let i=0;i<100;i++){const s=await fetch('/api/editor/state').then(r=>r.json());if(s.selectionCapabilities?.properties['text.size'].value===180)return !s.styleClipboard;await new Promise(resolve=>setTimeout(resolve,50));}return false;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(crossDeckStyle, "native cross-deck paste restores the ephemeral style without engine clipboard persistence", checks: &checks)
    }

    private func verifyBackgroundNativeBuildDrafts(checks: inout [String]) async throws {
        guard let view = editorWebView else { throw NativeAcceptanceError(message: "Missing build-draft test view") }
        let result = try await view.callAsyncJavaScript("""
            const checks=[],require=(ok,message)=>{if(!ok)throw Error(message);checks.push(message);};
            const state=()=>fetch('/api/editor/state').then(r=>r.json());
            const wait=async predicate=>{for(let i=0;i<150;i++){if(await predicate())return;await new Promise(r=>setTimeout(r,20));}throw Error('Native build draft readiness timed out');};
            try {
              await editorAction({action:'select-slide',slide:0});
              const s=await state();await editorAction({action:'select-element',element:0,elementData:s.slides[0].elements[0]});
              document.getElementById('keynope-ribbon-tab-slide').click();
              document.querySelector('[aria-label="Edit slide builds"]').click();
              await wait(()=>!!document.querySelector('dialog[aria-label="Slide builds"]'));
              const d=document.querySelector('dialog[aria-label="Slide builds"]'),before=await state();
              const effect=d.querySelector('[aria-label="Transition effect"]');effect.value='fade';effect.dispatchEvent(new Event('change'));
              const duration=d.querySelector('[aria-label="Transition duration (ms)"]');duration.value='750';duration.dispatchEvent(new Event('input'));duration.focus();
              require(KeynopeBuilds.isDirty(),'native build draft publishes pending numeric/transition changes');
              require((await state()).version===before.version,'native build draft does not mutate document before Save');
              await Promise.all([keynopePrepareDocumentSave(),keynopePrepareDocumentSave()]);
              const after=await state();
              require(after.version===before.version+1&&after.slideBuild.transition.effect==='fade'&&after.slideBuild.transition.durationMs===750,'native Save flushes a focused build draft exactly once');
              require(!d.isConnected&&!KeynopeBuilds.isDirty(),'native saved build draft closes and clears local dirty state');
              require(JSON.stringify(after.slides)===JSON.stringify(before.slides),'native build Save leaves content and geometry unchanged');
              await editorAction({action:'undo'});require((await state()).slideBuild.transition.effect==='cut','native document Undo restores the original transition');
              await editorAction({action:'redo'});require((await state()).slideBuild.transition.durationMs===750,'native document Redo restores the accepted transition');
              document.getElementById('keynope-ribbon-tab-slide').click();document.querySelector('[aria-label="Edit slide builds"]').click();
              await wait(()=>!!document.querySelector('dialog[aria-label="Slide builds"]'));
              const cancelled=document.querySelector('dialog[aria-label="Slide builds"]'),control=cancelled.querySelector('[aria-label="Transition effect"]'),version=(await state()).version;control.value='wipe';control.dispatchEvent(new Event('change'));
              [...cancelled.querySelectorAll('button')].find(b=>b.textContent==='Cancel').click();
              require(!KeynopeBuilds.isDirty()&&(await state()).version===version,'native build Cancel discards only the local draft');
              return {checks};
            } catch(error) { return {failure:String(error.message),checks}; }
            """, contentWorld: .page) as? [String: Any] ?? [:]
        let passed = result["checks"] as? [String] ?? []
        try requireNativeAcceptance(passed.count == 8, "native build draft acceptance: \(result["failure"] as? String ?? "passed")", checks: &checks)
        checks.append(contentsOf: passed)
    }

    // Opt-in real AppKit -> WKWebView input. Events target this isolated test
    // window only; no global cursor warp, Accessibility grant or clipboard use.
    // These are synthetic native events, not a physical input-to-photon claim.
    private func verifyNativeAppKitGestures(root: URL, checks: inout [String]) async throws {
        guard let view=editorWebView,let editor=editorWindow else {throw NativeAcceptanceError(message:"Missing native gesture editor")}
        let source=try Data(contentsOf:root.appendingPathComponent("original.md"))
        let clipboardRevision=NSPasteboard.general.changeCount
        editor.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps:true)
        try await waitForNativeAcceptance("native gesture key window") {editor.isKeyWindow && NSApp.isActive}
        try await waitForNativeJavaScript("native dense scene",in:view,expression:"typeof modernPageView!=='undefined'&&deck.pages[pageIndex]?.scene?.objects.length===49")
        _ = try await view.callAsyncJavaScript("await document.fonts.ready;await modernPageView.ready;return true;",contentWorld:.page)
        var records:[[String:Any]]=[]
        for ids in [["dense-text-0"],["dense-text-0","dense-text-1"],["dense-table"],["alpha"]] {
            let point=try await view.callAsyncJavaScript("""
              const s=await fetch('/api/editor/state').then(r=>r.json());
              const indices=ids.map(id=>s.slides[0].elements.findIndex(e=>e.id===id));
              await editorAction({action:'select-elements',elementIndices:indices,elementsData:ids.map(id=>({id}))});
              await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
              const host=document.querySelector('#stage .keynope-modern-scene'),hit=document.querySelector('.keynope-canvas-element[data-element="'+indices[0]+'"]'),node=host.querySelector('[data-object-id="'+ids[0]+'"]'),box=node.getBoundingClientRect();
              window.nativeGestureProof={ids,host,owners:new Map([...host.querySelectorAll('[data-object-id]')].map(n=>[n.dataset.objectId,n])),images:[...host.querySelectorAll('img')],before:JSON.stringify(s.slides[0].elements),x:node.getBoundingClientRect().x,samples:[],fullUpdates:0,on:true,view:modernPageView,originalPositions:modernPageView.previewPositions,originalUpdate:modernPageView.update};
              const p=nativeGestureProof;
              p.pointer=e=>{p.at=performance.now();p.trusted=e.isTrusted;};window.addEventListener('pointermove',p.pointer,true);
              modernPageView.update=(...args)=>{if(p.on)p.fullUpdates++;return p.originalUpdate(...args);};
              modernPageView.previewPositions=(...args)=>{const start=performance.now(),pointer=p.at,result=p.originalPositions(...args);if(p.on&&pointer){const sample={trusted:p.trusted,paintMS:performance.now()-start,eventToPaintMS:start-pointer};p.samples.push(sample);requestAnimationFrame(()=>sample.nextFrameMS=performance.now()-pointer);}return result;};
              for(const fy of [.5,.2,.8,.1,.9])for(const fx of [.5,.2,.8,.1,.9]){const x=box.x+box.width*fx,y=box.y+box.height*fy,target=document.elementFromPoint(x,y);if(target?.closest('.keynope-canvas-element')===hit&&!target.closest('.keynope-resize-handle'))return {x,y};}
              throw Error('No exposed native hit target '+ids[0]);
              """,arguments:["ids":ids],in:nil,contentWorld:.page) as? [String:Double]
            guard let point,let x=point["x"],let y=point["y"] else {throw NativeAcceptanceError(message:"Missing native drag point")}
            func send(_ type:NSEvent.EventType,_ dx:Double,_ dy:Double) throws {
                let local=NSPoint(x:x+dx,y:view.isFlipped ? y+dy : view.bounds.height-y-dy)
                let location=view.convert(local,to:nil)
                guard let event=NSEvent.mouseEvent(with:type,location:location,modifierFlags:[.option],timestamp:ProcessInfo.processInfo.systemUptime,windowNumber:editor.windowNumber,context:nil,eventNumber:0,clickCount:1,pressure:type == .leftMouseUp ? 0 : 1) else {throw NativeAcceptanceError(message:"Could not construct native pointer event")}
                NSApp.sendEvent(event)
            }
            try send(.mouseMoved,0,0)
            try send(.leftMouseDown,0,0)
            for step in 1...24 {
                try send(.leftMouseDragged,Double(step)*2,Double(step)*0.3)
                try await Task.sleep(for:.milliseconds(16))
            }
            let during=try await view.callAsyncJavaScript("""
              await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));
              const p=nativeGestureProof,s=await fetch('/api/editor/state').then(r=>r.json());
              p.on=false;
              return {ids:p.ids,moved:p.host.querySelector('[data-object-id="'+p.ids[0]+'"]').getBoundingClientRect().x-p.x,owners:[...p.owners].every(([id,n])=>p.host.querySelector('[data-object-id="'+id+'"]')===n),images:p.images.every(n=>p.host.contains(n)),uncommitted:JSON.stringify(s.slides[0].elements)===p.before,samples:p.samples,fullUpdates:p.fullUpdates};
              """,contentWorld:.page) as? [String:Any] ?? [:]
            try send(.leftMouseUp,48,7.2)
            try requireNativeAcceptance((during["moved"] as? Double ?? 0)>15,"native pointer moves rendered \(ids.joined(separator:"+"))",checks:&checks)
            try requireNativeAcceptance(during["owners"] as? Bool == true && during["images"] as? Bool == true && during["uncommitted"] as? Bool == true && during["fullUpdates"] as? Int == 0,"native drag retains 49 owners/media without early commit or full scene reconciliation",checks:&checks)
            let samples=during["samples"] as? [[String:Any]] ?? []
            try requireNativeAcceptance(!samples.isEmpty && samples.allSatisfy{$0["trusted"] as? Bool == true},"AppKit delivers trusted WKWebView pointer events",checks:&checks)
            try await waitForNativeJavaScript("native pointer commit",in:view,expression:"(async()=>JSON.stringify((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements)!==nativeGestureProof.before)()")
            let restored=try await view.callAsyncJavaScript("""
              await editorAction({action:'undo'});const p=nativeGestureProof,s=await fetch('/api/editor/state').then(r=>r.json());
              p.view.previewPositions=p.originalPositions;p.view.update=p.originalUpdate;window.removeEventListener('pointermove',p.pointer,true);
              return JSON.stringify(s.slides[0].elements)===p.before;
              """,contentWorld:.page) as? Bool ?? false
            try requireNativeAcceptance(restored,"native mouse release is one undoable document edit",checks:&checks)
            records.append(during)
        }
        try JSONSerialization.data(withJSONObject:["platform":ProcessInfo.processInfo.operatingSystemVersionString,"events":"targeted synthetic NSEvent through NSApp","records":records],options:[.prettyPrinted,.sortedKeys]).write(to:root.appendingPathComponent("native-gestures.json"))
        let snapshot=try await view.takeSnapshot(configuration:nil)
        if let tiff=snapshot.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]) {try png.write(to:root.appendingPathComponent("native-gestures.png"))}
        try requireNativeAcceptance(NSPasteboard.general.changeCount==clipboardRevision && (try Data(contentsOf:root.appendingPathComponent("original.md")))==source,"native gesture test leaves clipboard and owned source unchanged",checks:&checks)
    }

    private func verifyNativeStyleMenus(root: URL, checks: inout [String]) async throws {
        guard let view = editorWebView, let editor = editorWindow, let styles = styleMenu else {
            throw NativeAcceptanceError(message: "Missing native style menu/editor")
        }
        let foreground = NSWorkspace.shared.frontmostApplication?.processIdentifier
        let boardRevision = NSPasteboard.general.changeCount
        let originalBytes = try Data(contentsOf: root.appendingPathComponent("original.md"))
        let foregroundAllowed = ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_STYLE_MENU_FOREGROUND"] == "1"
        if !foregroundAllowed { try requireNativeAcceptance(!NSApp.isActive, "isolated style test starts without activating Keynope", checks: &checks) }
        try await waitForNativeJavaScript("style command editor ready", in: view, expression: "typeof window.keynopeStyleCommand==='function' && !!document.querySelector('[data-object-id=\"style-source\"]')")
        let names: Set<String> = ["style.copy", "style.paste", "style.reset", "style.painter", "style.label.copy", "style.label.paste", "style.label.reset", "style.label.painter"]
        try requireNativeAcceptance(Set(styleMenuItems.keys) == names && !styles.autoenablesItems, "native Style submenu has eight explicitly scoped capability-owned actions", checks: &checks)
        try requireNativeAcceptance(styleMenuItems["style.copy"]?.keyEquivalent == "c" && styleMenuItems["style.paste"]?.keyEquivalent == "v" && styleMenuItems["style.copy"]?.keyEquivalentModifierMask == [.command, .option] && styleMenuItems["style.paste"]?.keyEquivalentModifierMask == [.command, .option], "native style key equivalents are Command-Option-C/V", checks: &checks)
        try requireNativeAcceptance(nativeMenu(named: "Edit")?.autoenablesItems == true && nativeMenu(named: "Edit")?.items.first(where: { $0.title == "Undo" })?.target == nil, "Style does not replace ordinary Edit responder-chain validation", checks: &checks)
        if !foregroundAllowed {
            // Inactive AppKit apps cannot own a real key window. Verify the
            // negative security contract without faking key ownership or
            // activating a test window. Positive shortcuts are an explicit
            // foreground-only acceptance run, never silently substituted.
            let initialVersion = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'select-element',element:0,elementData:s.slides[0].elements[0]});return (await fetch('/api/editor/state').then(r=>r.json())).version;", contentWorld: .page) as? Int ?? -1
            try await waitForNativeJavaScript("background source capability", in: view, expression: "window.keynopeStyleCommandState()['style.copy']===true")
            refreshEditorStyleCommands()
            try requireNativeAcceptance(styleMenuItems.values.allSatisfy { !$0.isEnabled }, "background editor cannot enable native style actions despite eligible JS selection", checks: &checks)
            let ordinaryDeliveries = nativeOrdinaryClipboardDeliveries
            _ = try performNativeShortcut("c", keyCode: 8, modifiers: [.command, .option])
            try requireNativeAcceptance(nativeOrdinaryClipboardDeliveries == ordinaryDeliveries, "AppKit style key equivalent does not spill into ordinary Copy", checks: &checks)
            for item in styleMenuItems.values { if let action = item.action { _ = NSApp.sendAction(action, to: item.target, from: item) } }
            updateEditorStyleCommands(Dictionary(uniqueKeysWithValues: names.map { ($0, true) }))
            try requireNativeAcceptance(styleMenuItems.values.allSatisfy { !$0.isEnabled }, "even cached all-true capabilities cannot authorize a background editor", checks: &checks)
            let denied = nativeDeniedStyleMessages
            _ = try await view.callAsyncJavaScript("const f=document.createElement('iframe');f.hidden=true;f.srcdoc='<script>window.webkit.messageHandlers.keynopePresenter.postMessage({action:\"editor-style-commands\",flags:{\"style.copy\":true}})<\\/script>';document.body.append(f);return true;", contentWorld: .page)
            try await waitForNativeAcceptance("background iframe capability denial") { self.nativeDeniedStyleMessages > denied }
            try requireNativeAcceptance(styleMenuItems.values.allSatisfy { !$0.isEnabled }, "received iframe capability message is rejected by the real WebKit bridge", checks: &checks)
            let config = WKWebViewConfiguration()
            config.userContentController.add(self, name: "keynopePresenter")
            let audience = WKWebView(frame: NSRect(x: 0, y: 0, width: 20, height: 20), configuration: config)
            defer { config.userContentController.removeScriptMessageHandler(forName: "keynopePresenter") }
            audience.loadHTMLString("<script>window.webkit.messageHandlers.keynopePresenter.postMessage({action:'editor-style-commands',flags:{'style.copy':true}})</script>", baseURL: editorURL())
            try await waitForNativeAcceptance("background audience capability denial") { self.nativeDeniedStyleMessages > denied + 1 }
            try requireNativeAcceptance(styleMenuItems.values.allSatisfy { !$0.isEnabled }, "received audience WebView capability message cannot enable editor menu actions", checks: &checks)
            let protected = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.version===version&&!s.styleClipboard;", arguments: ["version": initialVersion], in: nil, contentWorld: .page) as? Bool ?? false
            try requireNativeAcceptance(protected && styleClipboardData.isEmpty, "direct native actions without keyboard ownership leave document and ephemeral clipboard intact", checks: &checks)
            // Exercise the production WebView callable route without pretending
            // an inactive window owns AppKit keyboard focus. These are positive
            // shared-editor checks, not positive native-menu acceptance.
            let sharedChecks = try await view.callAsyncJavaScript("""
                const checks=[];
                const require=(ok,message)=>{if(!ok)throw new Error(message);checks.push(message);};
                const state=()=>fetch('/api/editor/state').then(r=>r.json());
                const select=async(slide,element)=>{
                  await editorAction({action:'select-slide',slide});
                  const s=await state();
                  await editorAction({action:'select-element',element,elementData:s.slides[slide].elements[element]});
                  document.activeElement?.blur();
                };
                await select(0,0);
                let before=await state();
                require(await window.keynopeStyleCommand('copy'),'WKWebView body Copy succeeds');
                let after=await state();
                require(after.version===before.version&&JSON.stringify(after.slides)===JSON.stringify(before.slides),'WKWebView Copy is read-only');
                await select(0,1);before=await state();
                require(await window.keynopeStyleCommand('paste'),'WKWebView body Paste succeeds');
                after=await state();
                let q=new URLSearchParams(after.slides[0].elements[1].query),old=new URLSearchParams(before.slides[0].elements[1].query);
                require(after.version===before.version+1&&after.slides[0].elements[1].text==='Keep this text'&&q.get('modern-size')==='120'&&q.get('fg')==='#ff55aa'&&['left','top','width','height'].every(k=>q.get(k)===old.get(k)),'WKWebView Paste preserves content and geometry in one transaction');
                await editorAction({action:'undo'});after=await state();
                require(JSON.stringify(after.slides)===JSON.stringify(before.slides),'WKWebView style Undo restores all slide data');
                await editorAction({action:'redo'});
                require(new URLSearchParams((await state()).slides[0].elements[1].query).get('modern-size')==='120','WKWebView style Redo restores formatting');
                for(const tag of ['input','textarea','div']){
                  const input=document.createElement(tag);if(tag==='div')input.contentEditable='true';document.body.append(input);input.focus();
                  before=await state();
                  require(!(await window.keynopeStyleCommand('reset'))&&!(await window.keynopeStyleCommand('paste')),'WKWebView '+tag+' caret rejects object style commands');
                  require((await state()).version===before.version,'WKWebView '+tag+' caret preserves history');input.remove();
                }
                await select(1,0);
                require(await window.keynopeStyleCommand('copy','shape-label'),'WKWebView label Copy succeeds');
                await select(1,1);before=await state();
                require(!(await window.keynopeStyleCommand('paste')),'WKWebView label clipboard cannot paste as object style');
                require(await window.keynopeStyleCommand('paste','shape-label'),'WKWebView label Paste succeeds');
                after=await state();q=new URLSearchParams(after.slides[1].elements[1].query);old=new URLSearchParams(before.slides[1].elements[1].query);
                require(after.version===before.version+1&&q.get('label-modern-size')==='80'&&q.get('fg')===old.get('fg')&&q.get('shape-label')===old.get('shape-label'),'WKWebView label Paste preserves shape paint and label content');
                await select(2,0);before=await state();editSelectedTable();
                return checks;
                """, contentWorld: .page) as? [String] ?? []
            try requireNativeAcceptance(sharedChecks.count == 16, "positive shared-editor checks completed in the real background WKWebView", checks: &checks)
            checks.append(contentsOf: sharedChecks)
            try await waitForNativeJavaScript("background table draft ready", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table\"] textarea[aria-label=\"Cell 1, 1\"]')")
            let tableResult = try await view.callAsyncJavaScript("""
                try {
                const d=document.querySelector('dialog[aria-label="Edit table"]');
                const state=()=>fetch('/api/editor/state').then(r=>r.json());
                const ready=async(command)=>{
                  const deadline=performance.now()+5000;
                  while(!window.keynopeStyleCommandState()['style.'+command]){
                    if(performance.now()>deadline)throw new Error('Cell draft '+command+' capability timed out');
                    await new Promise(resolve=>setTimeout(resolve,20));
                  }
                };
                const before=await state();
                d.querySelector('textarea[aria-label="Cell 1, 1"]').focus();
                if(await window.keynopeStyleCommand('copy'))throw new Error('Table caret accepted object Copy');
                d.focus();await ready('copy');if(!(await window.keynopeStyleCommand('copy')))throw new Error('Cell draft Copy rejected');
                d.querySelector('textarea[aria-label="Cell 2, 1"]').focus();d.focus();
                await ready('paste');
                if(!(await window.keynopeStyleCommand('paste')))throw new Error('Cell draft Paste rejected');
                if(d.querySelector('input[aria-label="Cell font size"]').value!=='80')throw new Error('Cell draft style did not update');
                if((await state()).version!==before.version)throw new Error('Cell draft Paste edited document before Apply');
                [...d.querySelectorAll('button')].find(b=>b.textContent==='Cancel').click();
                const after=await state();
                if(after.version!==before.version||JSON.stringify(after.slides)!==JSON.stringify(before.slides)||JSON.stringify(after.selectedTable?.data)!==JSON.stringify(before.selectedTable?.data))throw new Error('Cell draft Cancel changed document');
                return 'passed';
                } catch(error) { return String(error.message); }
                """, contentWorld: .page) as? String ?? "Missing cell draft result"
            try requireNativeAcceptance(tableResult == "passed", "real WKWebView cell Copy/Paste stays in draft history and Cancel preserves the document: \(tableResult)", checks: &checks)
            _ = try await view.callAsyncJavaScript("editSelectedTable();return true;", contentWorld: .page)
            try await waitForNativeJavaScript("background table Apply draft ready", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table\"] textarea[aria-label=\"Cell 1, 1\"]')")
            let applyResult = try await view.callAsyncJavaScript("""
                try {
                  const checks=[],require=(ok,message)=>{if(!ok)throw new Error(message);checks.push(message);};
                  const state=()=>fetch('/api/editor/state').then(r=>r.json());
                  const wait=async(test,name)=>{
                    const deadline=performance.now()+10000;
                    while(!(await test())){if(performance.now()>deadline)throw new Error(name+' timed out');await new Promise(resolve=>setTimeout(resolve,20));}
                  };
                  const d=document.querySelector('dialog[aria-label="Edit table"]'),before=await state();
                  const button=name=>[...d.querySelectorAll('button')].find(b=>b.textContent===name);
                  const size=()=>d.querySelector('input[aria-label="Cell font size"]').value;
                  d.querySelector('textarea[aria-label="Cell 1, 1"]').focus();d.focus();
                  await wait(()=>window.keynopeStyleCommandState()['style.copy'],'table Copy readiness');
                  require(await window.keynopeStyleCommand('copy'),'native WebView table source style captured');
                  d.querySelector('textarea[aria-label="Cell 2, 1"]').focus();d.focus();
                  await wait(()=>window.keynopeStyleCommandState()['style.paste'],'table Paste readiness');
                  const oldSize=size();
                  require(await window.keynopeStyleCommand('paste'),'native WebView target cell style pasted');
                  require(size()==='80'&&oldSize!=='80','table draft visibly accepts source metrics');
                  button('Undo').click();await wait(()=>window.keynopeStyleCommandState()['style.paste'],'table Undo readiness');require(size()===oldSize,'table local Undo restores prior metrics');
                  button('Redo').click();await wait(()=>window.keynopeStyleCommandState()['style.paste'],'table Redo readiness');require(size()==='80','table local Redo restores pasted metrics');
                  let current=await state();
                  require(current.version===before.version&&JSON.stringify(current.selectedTable.data)===JSON.stringify(before.selectedTable.data),'draft Undo and Redo never commit document history');
                  const expected=JSON.parse(JSON.stringify(before.selectedTable.data));
                  button('Apply').click();
                  await wait(async()=>!d.isConnected&&(await state()).version===before.version+1,'table Apply commit');
                  const applied=await state(),table=applied.selectedTable.data;
                  require(table.cells[1][0].style.fontSize===80,'table Apply commits selected cell typography');
                  require(JSON.stringify(table.cells[1][0].runs)===JSON.stringify(expected.cells[1][0].runs)&&table.cells[1][0].colSpan===expected.cells[1][0].colSpan&&table.cells[1][1].hidden===expected.cells[1][1].hidden,'table Apply preserves word styling and merged structure');
                  require(JSON.stringify(table.cells[0])===JSON.stringify(expected.cells[0])&&JSON.stringify(table.cells[2])===JSON.stringify(expected.cells[2]),'table Apply leaves unselected rows intact');
                  require(JSON.stringify(applied.slides)===JSON.stringify(before.slides),'table Apply preserves element geometry and neighbouring slides');
                  await editorAction({action:'undo'});current=await state();
                  require(JSON.stringify(current.selectedTable.data)===JSON.stringify(expected),'document Undo restores complete original table');
                  await editorAction({action:'redo'});current=await state();
                  require(JSON.stringify(current.selectedTable.data)===JSON.stringify(table),'document Redo restores complete applied table');
                  require(current.version===before.version+3,'Apply Undo and Redo each advance document revision once');
                  return {checks};
                } catch(error) { return {failure:String(error.message)}; }
                """, contentWorld: .page) as? [String: Any] ?? [:]
            let applyChecks = applyResult["checks"] as? [String] ?? []
            try requireNativeAcceptance(applyChecks.count == 13, "native WebView table Apply/history acceptance: \(applyResult["failure"] as? String ?? "passed")", checks: &checks)
            checks.append(contentsOf: applyChecks)
            try await verifyBackgroundNativeGeometry(root: root, checks: &checks)
            try await verifyBackgroundNativeBuildDrafts(checks: &checks)
            try requireNativeAcceptance(NSPasteboard.general.changeCount == boardRevision, "background style checks never wrote the system clipboard", checks: &checks)
            try requireNativeAcceptance(try Data(contentsOf: root.appendingPathComponent("original.md")) == originalBytes, "background checks leave the owned source file unchanged", checks: &checks)
            try requireNativeAcceptance(!NSApp.isActive && NSWorkspace.shared.frontmostApplication?.processIdentifier == foreground, "background native checks never activate or foreground Keynope", checks: &checks)
            return
        }
        editor.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        try await waitForNativeAcceptance("foreground editor key designation") { editor.isKeyWindow }
        func select(_ slide: Int, _ element: Int) async throws {
            _ = try await view.callAsyncJavaScript("await editorAction({action:'select-slide',slide});const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'select-element',element,elementData:s.slides[slide].elements[element]});document.activeElement?.blur();return true;", arguments: ["slide": slide, "element": element], in: nil, contentWorld: .page)
            refreshEditorStyleCommands()
        }
        func send(_ key: String) throws {
            guard let item = styleMenuItems[key], let action = item.action,
                  NSApp.sendAction(action, to: item.target, from: item) else {
                throw NativeAcceptanceError(message: "Native Style action not delivered: \(key)")
            }
        }
        func expect(_ key: String, _ enabled: Bool) async throws {
            try await waitForNativeAcceptance("native \(key) availability \(enabled)") { self.styleMenuItems[key]?.isEnabled == enabled }
        }
        try await select(0, 0)
        try await expect("style.copy", true)
        _ = try await view.callAsyncJavaScript("window.__menuBefore=await fetch('/api/editor/state').then(r=>r.json());return true;", contentWorld: .page)
        try requireNativeAcceptance(try performNativeShortcut("c", keyCode: 8, modifiers: [.command, .option]), "Command-Option-C is delivered by the actual AppKit Edit menu", checks: &checks)
        try await expect("style.paste", true)
        let copied = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.version===window.__menuBefore.version&&JSON.stringify(s.slides)===JSON.stringify(window.__menuBefore.slides)&&s.styleClipboard?.fields['text.size'].value==='120';", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(copied && !styleClipboardData.isEmpty, "native menu Copy captures owned ephemeral formatting without a document edit", checks: &checks)
        try await select(0, 1)
        _ = try await view.callAsyncJavaScript("window.__menuBefore=await fetch('/api/editor/state').then(r=>r.json());return true;", contentWorld: .page)
        try await expect("style.paste", true)
        try requireNativeAcceptance(try performNativeShortcut("v", keyCode: 9, modifiers: [.command, .option]), "Command-Option-V is delivered by the actual AppKit Edit menu", checks: &checks)
        try await waitForNativeJavaScript("native menu Paste committed", in: view, expression: "(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),e=s.slides[0].elements[1],q=new URLSearchParams(e.query),old=new URLSearchParams(window.__menuBefore.slides[0].elements[1].query);return s.version===window.__menuBefore.version+1&&e.text==='Keep this text'&&q.get('modern-size')==='120'&&q.get('fg')==='#ff55aa'&&['left','top','width','height'].every(k=>q.get(k)===old.get(k));})()")
        try requireNativeAcceptance(true, "native menu Paste is one history transaction and preserves text/geometry", checks: &checks)
        _ = try await view.callAsyncJavaScript("await editorAction({action:'undo'});window.__menuBefore=await fetch('/api/editor/state').then(r=>r.json());return true;", contentWorld: .page)
        // The queued action must not survive loss of editor keyboard ownership.
        try await expect("style.paste", true)
        try send("style.paste")
        let otherKeyWindow = PresenterWindow(contentRect: NSRect(x: 0, y: 0, width: 20, height: 20), styleMask: .borderless, backing: .buffered, defer: false)
        otherKeyWindow.isReleasedWhenClosed = false
        otherKeyWindow.makeKeyAndOrderFront(nil)
        defer { otherKeyWindow.orderOut(nil) }
        try await waitForNativeAcceptance("style actions disabled after editor resigns key") { self.styleMenuItems.values.allSatisfy { !$0.isEnabled } }
        let cancelled = try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version===window.__menuBefore.version;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(cancelled, "queued native style command cancels when editor loses key ownership", checks: &checks)
        editor.makeKeyAndOrderFront(nil)
        refreshEditorStyleCommands()
        try await expect("style.reset", true)
        try send("style.reset")
        try await waitForNativeJavaScript("native Reset committed", in: view, expression: "(async()=>!new URLSearchParams((await fetch('/api/editor/state').then(r=>r.json())).slides[0].elements[1].query).has('modern-size'))()")
        try requireNativeAcceptance(true, "native Reset menu restores inherited formatting", checks: &checks)
        // Validate live caret/modal gating, not just a cached enabled item.
        _ = try await view.callAsyncJavaScript("const input=document.createElement('input');input.id='native-style-caret';document.body.append(input);input.focus();window.__menuBefore=await fetch('/api/editor/state').then(r=>r.json());return true;", contentWorld: .page)
        try await expect("style.copy", false)
        try await expect("style.paste", false)
        _ = try performNativeShortcut("c", keyCode: 8, modifiers: [.command, .option])
        try send("style.reset")
        let caretProtected = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());document.querySelector('#native-style-caret').remove();return s.version===window.__menuBefore.version;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(caretProtected, "even direct native action delivery cannot restyle during caret editing", checks: &checks)
        try await select(0, 0)
        try await expect("style.painter", true)
        try send("style.painter")
        try await waitForNativeJavaScript("native Painter armed", in: view, expression: "document.querySelector('[aria-label=\"Format painter\"]')?.getAttribute('aria-pressed')==='true'")
        _ = try await view.callAsyncJavaScript("window.__menuBefore=await fetch('/api/editor/state').then(r=>r.json());window.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));return true;", contentWorld: .page)
        let painterCancelled = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.version===window.__menuBefore.version&&s.selected===window.__menuBefore.selected&&document.querySelector('[aria-label=\"Format painter\"]')?.getAttribute('aria-pressed')==='false';", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(painterCancelled, "native menu Painter Escape preserves selection and document history", checks: &checks)
        // Explicit label context: body styling remains independent.
        try await select(1, 0)
        try await expect("style.label.copy", true)
        try send("style.label.copy")
        try await expect("style.label.paste", true)
        try await expect("style.paste", false)
        try await select(1, 1)
        _ = try await view.callAsyncJavaScript("window.__menuBefore=await fetch('/api/editor/state').then(r=>r.json());return true;", contentWorld: .page)
        try await expect("style.label.paste", true)
        try send("style.label.paste")
        try await waitForNativeJavaScript("native Label Paste committed", in: view, expression: "(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),q=new URLSearchParams(s.slides[1].elements[1].query),old=new URLSearchParams(window.__menuBefore.slides[1].elements[1].query);return s.version===window.__menuBefore.version+1&&q.get('label-modern-size')==='80'&&q.get('fg')===old.get('fg')&&q.get('shape-label')===old.get('shape-label');})()")
        try requireNativeAcceptance(true, "native Label Paste preserves independent shape paint and label content", checks: &checks)
        // Table modal commands must stay local until Apply.
        try await select(2, 0)
        _ = try await view.callAsyncJavaScript("window.__menuBefore=await fetch('/api/editor/state').then(r=>r.json());editSelectedTable();return true;", contentWorld: .page)
        try await waitForNativeJavaScript("native table modal ready", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table\"] textarea[aria-label=\"Cell 1, 1\"]')")
        try await expect("style.copy", false)
        _ = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table\"]');d.querySelector('textarea[aria-label=\"Cell 1, 1\"]').focus();d.focus();return true;", contentWorld: .page)
        refreshEditorStyleCommands()
        try await expect("style.copy", true)
        try requireNativeAcceptance(try performNativeShortcut("c", keyCode: 8, modifiers: [.command, .option]), "native Copy Style shortcut routes to the active cell draft", checks: &checks)
        try await expect("style.paste", true)
        try await expect("style.label.paste", false)
        _ = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table\"]');d.querySelector('textarea[aria-label=\"Cell 2, 1\"]').focus();d.focus();return true;", contentWorld: .page)
        refreshEditorStyleCommands()
        try await expect("style.paste", true)
        try requireNativeAcceptance(try performNativeShortcut("v", keyCode: 9, modifiers: [.command, .option]), "native Paste Style shortcut routes to the selected cell draft", checks: &checks)
        try await waitForNativeJavaScript("native cell draft Paste", in: view, expression: "document.querySelector('dialog[aria-label=\"Edit table\"] input[aria-label=\"Cell font size\"]')?.value==='80'")
        let draftOnly = try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version===window.__menuBefore.version;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(draftOnly, "native table menu Paste changes only draft history", checks: &checks)
        _ = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table\"]');[...d.querySelectorAll('button')].find(b=>b.textContent==='Apply').click();return true;", contentWorld: .page)
        try await waitForNativeJavaScript("native cell draft committed", in: view, expression: "(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return !document.querySelector('dialog[aria-label=\"Edit table\"]')&&s.version===window.__menuBefore.version+1&&s.selectedTable?.data.cells[1][0].style.fontSize===80;})()")
        try requireNativeAcceptance(true, "native table draft Apply commits the menu style operation once", checks: &checks)
        // Trusted main-frame capability messages only; explicitly observe
        // rejected deliveries rather than assuming a quiet iframe proves it.
        updateEditorStyleCommands()
        let denied = nativeDeniedStyleMessages
        _ = try await view.callAsyncJavaScript("const f=document.createElement('iframe');f.hidden=true;f.srcdoc='<script>window.webkit.messageHandlers.keynopePresenter.postMessage({action:\"editor-style-commands\",flags:{\"style.copy\":true}})<\\/script>';document.body.append(f);return true;", contentWorld: .page)
        try await waitForNativeAcceptance("iframe capability denial") { self.nativeDeniedStyleMessages > denied }
        try requireNativeAcceptance(styleMenuItems.values.allSatisfy { !$0.isEnabled }, "iframe cannot enable the native Style menu", checks: &checks)
        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "keynopePresenter")
        let audience = WKWebView(frame: NSRect(x: 0, y: 0, width: 20, height: 20), configuration: config)
        let audienceWindow = PresenterWindow(contentRect: audience.frame, styleMask: .borderless, backing: .buffered, defer: false)
        audienceWindow.contentView = audience
        audienceWindow.isReleasedWhenClosed = false
        audienceWindow.orderBack(nil)
        audienceWindow.makeKeyAndOrderFront(nil)
        defer { audienceWindow.orderOut(nil); config.userContentController.removeScriptMessageHandler(forName: "keynopePresenter") }
        audience.loadHTMLString("<script>window.webkit.messageHandlers.keynopePresenter.postMessage({action:'editor-style-commands',flags:{'style.copy':true}})</script>", baseURL: editorURL())
        try await waitForNativeAcceptance("audience capability denial") { self.nativeDeniedStyleMessages > denied + 1 }
        try requireNativeAcceptance(styleMenuItems.values.allSatisfy { !$0.isEnabled } && editor.isKeyWindow == false, "audience window cannot enable or own editor style commands", checks: &checks)
        try send("style.reset")
        let audienceProtected = try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version===window.__menuBefore.version+1;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(audienceProtected, "native action delivery from an audience key window leaves editor history intact", checks: &checks)
        try requireNativeAcceptance(NSApp.isActive, "foreground opt-in exercised actual AppKit key-window ownership", checks: &checks)
        try requireNativeAcceptance(NSPasteboard.general.changeCount == boardRevision, "native style commands never wrote the system clipboard", checks: &checks)
        try requireNativeAcceptance(try Data(contentsOf: root.appendingPathComponent("original.md")) == originalBytes && activeDeckURLHandler?()?.standardizedFileURL == root.appendingPathComponent("original.md").standardizedFileURL, "native style verification keeps its owned source file and document path intact", checks: &checks)
    }

    private func verifyBackgroundNativeGeometry(root: URL, checks: inout [String]) async throws {
        guard let view = editorWebView else { throw NativeAcceptanceError(message: "Missing geometry test view") }
        _ = try await view.callAsyncJavaScript("await editorAction({action:'select-slide',slide:0});const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'select-element',element:0,elementData:s.slides[0].elements[0]});document.getElementById('keynope-ribbon-tab-arrange').click();return true;", contentWorld: .page)
        let ready = try await view.callAsyncJavaScript("""
            const deadline=performance.now()+10000;
            while(!document.querySelector('input[aria-label="Object width"]:not(:disabled)')){
              // Occluded accessory windows may suspend animation frames. Flush
              // the real callbacks, without inventing bounds or key ownership.
              renderEditorCanvasOverlay();renderEditorTopbar();
              if(document.querySelector('input[aria-label="Object width"]:not(:disabled)'))break;
              if(performance.now()>deadline){const s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify({selected:s.selected,current:s.current,geometry:s.selectionCapabilities?.geometry,page:deck.pages[pageIndex]?.slide,bounds:[...canvasCurrentBounds()],geometryProbe:canvasGeometryControls(0)?.outerHTML.slice(0,150),arrange:document.querySelector('#keynope-ribbon-arrange')?.textContent.slice(0,500),forms:[...document.querySelectorAll('form')].map(n=>n.getAttribute('aria-label')),tabs:[...document.querySelectorAll('[role=tab][aria-selected=true]')].map(n=>n.textContent)});}
              await new Promise(resolve=>setTimeout(resolve,20));
            }
            return 'ready';
            """, contentWorld: .page) as? String ?? "Missing geometry readiness"
        try requireNativeAcceptance(ready == "ready", "background native geometry form ready: \(ready)", checks: &checks)
        let result = try await view.callAsyncJavaScript("""
            try {
              const checks=[],require=(ok,message)=>{if(!ok)throw new Error(message);checks.push(message);};
              const state=()=>fetch('/api/editor/state').then(r=>r.json());
              const wait=async(test,name)=>{const deadline=performance.now()+10000;while(!(await test())){if(performance.now()>deadline)throw new Error(name+' timed out');await new Promise(resolve=>setTimeout(resolve,20));}};
              const input=name=>document.querySelector('[aria-label="Object '+name+'"]');
              const set=(name,value)=>{const field=input(name);field.value=String(value);field.dispatchEvent(new Event('input',{bubbles:true}));};
              const apply=()=>document.querySelector('form[aria-label="Object geometry"]').requestSubmit();
              const before=await state(),initial=before.slides[0].elements[0],originalFont=before.selectionCapabilities.properties['text.size'].value;
              for(const [key,value]of [['x',35.125],['y',4.25],['width',110.375],['height',10.125]])set(key,value);
              apply();
              await wait(async()=>{const s=await state();return s.version===before.version+1&&s.selectionCapabilities.geometry.width===110.375;},'fractional geometry commit');
              await wait(()=>input('width').value==='110.375'&&input('height').value==='10.125','accepted field projection');
              let accepted=await state();const positioned=accepted;
              require(accepted.selectionCapabilities.geometry.x===35.125&&accepted.selectionCapabilities.geometry.y===4.25,'native geometry preserves fractional position and bounds');
              require(accepted.selectionCapabilities.properties['text.size'].value===originalFont,'native box resize preserves font size');
              const lock=()=>document.querySelector('[aria-label="Lock object aspect ratio"]');lock().click();
              require(lock().checked,'native aspect lock activates');
              set('width',55.1875);
              require(Number(input('height').value)===5.0625,'native width draft couples height proportionally');
              [...document.querySelectorAll('button')].find(b=>b.textContent==='Reset fields').click();
              require(input('width').value==='110.375'&&input('height').value==='10.125'&&lock().checked,'native Reset fields restores accepted metrics and keeps aspect lock');
              require((await state()).version===accepted.version,'native Reset fields creates no history entry');
              set('width',55.1875);apply();
              await wait(async()=>{const s=await state();return s.version===accepted.version+1&&s.selectionCapabilities.geometry.width===55.1875&&s.selectionCapabilities.geometry.height===5.0625;},'locked aspect commit');
              await wait(()=>input('width').value==='55.188'&&input('height').value==='5.063','locked accepted fields');
              const resized=await state();
              require(lock().checked&&resized.selectionCapabilities.properties['text.size'].value===originalFont,'native accepted aspect resize retains lock and typography');
              set('height',10.125);
              require(Number(input('width').value)===110.375,'native height draft couples width proportionally');
              [...document.querySelectorAll('button')].find(b=>b.textContent==='Reset fields').click();
              await editorAction({action:'undo'});accepted=await state();
              require(JSON.stringify(accepted.slides)===JSON.stringify(positioned.slides),'native resize Undo restores exact preceding slide data');
              require(accepted.selectionCapabilities.geometry.width===110.375&&accepted.selectionCapabilities.geometry.height===10.125,'native aspect Undo restores previous accepted bounds');
              await editorAction({action:'redo'});
              require(JSON.stringify((await state()).slides)===JSON.stringify(resized.slides),'native aspect Redo restores exact slide data');
              await editorAction({action:'undo'});await editorAction({action:'undo'});
              require(JSON.stringify((await state()).slides)===JSON.stringify(before.slides),'native geometry Undo restores original placement and content');
              require(initial.text===(await state()).slides[0].elements[0].text,'native geometry retains text content');
              await editorAction({action:'redo'});await editorAction({action:'redo'});
              await wait(()=>input('width').value==='55.188'&&input('height').value==='5.063','redo accepted inspector fields');
              require(JSON.stringify((await state()).slides)===JSON.stringify(resized.slides),'native geometry complete Redo restores the inspected result');
              return {checks};
            }catch(error){return {failure:String(error.message)};}
            """, contentWorld: .page) as? [String: Any] ?? [:]
        let accepted = result["checks"] as? [String] ?? []
        try requireNativeAcceptance(accepted.count == 14, "background native geometry acceptance: \(result["failure"] as? String ?? "passed")", checks: &checks)
        checks.append(contentsOf: accepted)
        let image = try await view.takeSnapshot(configuration: nil)
        guard let tiff = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) else {
            throw NativeAcceptanceError(message: "Missing native geometry snapshot")
        }
        try png.write(to: root.appendingPathComponent("native-geometry-inspector.png"))
    }

    private func verifyNativeCharts(root: URL, checks: inout [String]) async throws {
        guard let view = editorWebView else { throw NativeAcceptanceError(message: "Missing chart test view") }
        if let editorWindow { orderEditorWindow(editorWindow) }
        try await waitForNativeJavaScript("native chart editor ready", in: view, expression: "typeof KeynopeCharts === 'object' && [...document.querySelectorAll('button')].some(b=>b.textContent==='Insert')")
        _ = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),elements=s.slides[0].elements.slice(0,2).map(e=>({...e}));elements[0].text='Chart workshop';elements[0].query='left=24&top=1&render=truetype&modern-font=sans&modern-size=60';elements[1].text='Shared, editable data';elements[1].query='left=24&top=47&render=truetype&modern-font=sans&modern-size=36';await editorAction({action:'update-elements',elementIndices:[0,1],elementsData:elements});return true;", contentWorld: .page)
        _ = try await view.evaluateJavaScript("[...document.querySelectorAll('button')].find(b=>b.textContent==='Insert').click();document.querySelector('[aria-label=\"Add chart\"]').click()")
        try await waitForNativeJavaScript("native chart data dialog", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit chart\"]')")
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit chart\"]'),p=d.querySelector('[aria-label=\"Paste chart data\"]');p.value='Category\\tActual\\tPlan\\nResearch\\t4\\t6\\nBuild\\t9\\t7\\nTest\\t-3\\t5\\nLaunch\\t7\\t10';[...d.querySelectorAll('button')].find(b=>b.textContent==='Paste data').click();const t=d.querySelector('[aria-label=\"Chart title\"]');t.value='From ideas to launch';t.dispatchEvent(new Event('input'));[...d.querySelectorAll('button')].find(b=>b.textContent==='Apply').click();})()")
        try await waitForNativeJavaScript("native semantic chart paint", in: view, expression: "!!document.querySelector('#stage .keynope-scene-chart path') && !document.querySelector('dialog[aria-label=\"Edit chart\"]')")
        try requireNativeAcceptance(true, "native chart insertion and data dialog paint a vector chart through the shared scene renderer", checks: &checks)
        for kind in ["column", "bar", "line", "area", "pie", "doughnut"] {
            let accepted = try await view.callAsyncJavaScript("""
            const s=await fetch('/api/editor/state').then(r=>r.json()),d=s.selectedChart.data;d.type='\(kind)';d.options.dataLabels=true;
            if(['pie','doughnut'].includes(d.type)){d.series=d.series.slice(0,1);d.series[0].values=d.series[0].values.map(Math.abs);}
            await editorAction({action:'set-chart-data',slide:s.current,sceneRevision:s.version,objectId:s.selectedChart.objectId,chartData:d});
            for(let i=0;i<100;i++){if(document.querySelector('#stage .keynope-scene-chart')?.dataset.chartType===d.type)return true;await new Promise(resolve=>setTimeout(resolve,50));}return false;
            """, contentWorld: .page) as? Bool ?? false
            try requireNativeAcceptance(accepted, "native \(kind) chart data edit updates its vector marks without reopening the window", checks: &checks)
        }
        _ = try await view.evaluateJavaScript("[...document.querySelectorAll('button')].find(b=>b.textContent==='Edit chart').click()")
        try await waitForNativeJavaScript("native chart reopening", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit chart\"]')")
        let invalid = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit chart\"]'),v=d.querySelector('[aria-label=\"Value 1, 1\"]');v.value='not a number';v.dispatchEvent(new Event('input'));[...d.querySelectorAll('button')].find(b=>b.textContent==='Apply').click();await new Promise(resolve=>setTimeout(resolve,100));return d.querySelector('[role=alert]').textContent.includes('finite number');", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(invalid, "native chart rejects malformed values inline without silently replacing them with zero", checks: &checks)
        _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit chart\"]').querySelectorAll('button')].find(b=>b.textContent==='Cancel').click()")
        let saved = root.appendingPathComponent("charts.md")
        expectNativePanel(title: "Save Keynope Presentation", destination: saved, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Save As…")
        try await waitForNativeAcceptance("native chart Markdown save") { !self.savingDocument && FileManager.default.fileExists(atPath: saved.path) && !self.documentDirty }
        try requireNativeAcceptance(try String(contentsOf: saved, encoding: .utf8).contains("keynope-objects"), "native Save embeds editable chart data in the Markdown deck", checks: &checks)
        let image = try await view.takeSnapshot(configuration: nil)
        if let tiff = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("chart-native.png"))
        }
    }

    private func verifyNativeTables(root: URL, checks: inout [String]) async throws {
        guard let view = editorWebView else { throw NativeAcceptanceError(message: "Missing table test view") }
        // Canvas hit regions are produced on animation frames. The owned
        // acceptance window must be visible before pointer/caret checks.
        if let editorWindow { orderEditorWindow(editorWindow) }
        let board = NSPasteboard.general
        let savedItems = (board.pasteboardItems ?? []).map { item in item.types.compactMap { type in item.data(forType: type).map { (type, $0) } } }
        defer {
            let restored = savedItems.map { values in
                let item = NSPasteboardItem()
                for (type, data) in values { item.setData(data, forType: type) }
                return item
            }
            board.clearContents()
            if !restored.isEmpty { board.writeObjects(restored) }
        }
        // Keep this focused visual fixture readable; the general native harness
        // deliberately uses an oversized default title to exercise pagination.
        _ = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());const elements=s.slides[0].elements.slice(0,2).map(e=>({...e}));elements[0].text='Workshop plan';elements[0].query='left=10&top=1&render=truetype&modern-font=sans&modern-size=64';elements[1].text='Editable table acceptance';elements[1].query='left=24&top=44&render=truetype&modern-font=sans&modern-size=48';const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'update-elements',elementIndices:[0,1],elementsData:elements})});if(!r.ok)throw Error(await r.text());return true;", contentWorld: .page)
        view.reload()
        try await waitForNativeJavaScript("native table editor ready", in: view, expression: "typeof KeynopeTables === 'object' && [...document.querySelectorAll('button')].some(b=>b.textContent==='Insert')")
        _ = try await view.evaluateJavaScript("[...document.querySelectorAll('button')].find(b=>b.textContent==='Insert').click()")
        try await waitForNativeJavaScript("native table insertion control", in: view, expression: "!!document.querySelector('[aria-label=\"Add table\"]')")
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Add table\"]').click()")
        try await waitForNativeJavaScript("native table grid picker", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Insert table\"] [aria-label=\"3 rows by 4 columns\"]')")
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Insert table\"]');d.querySelector('[aria-label=\"3 rows by 4 columns\"]').click();[...d.querySelectorAll('button')].find(b=>b.textContent==='Insert').click();})()")
        do {
            try await waitForNativeJavaScript("native inserted table", in: view, expression: "!!document.querySelector('#stage .keynope-scene-table') && !!document.querySelector('[aria-label=\"Edit table\"]')")
        } catch {
            let detail = try? await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify({version:s.version,selectedTable:s.selectedTable,table:!!document.querySelector('#stage .keynope-scene-table'),buttons:[...document.querySelectorAll('button')].filter(b=>b.textContent==='Edit table').length,alerts:[...document.querySelectorAll('[role=alert]')].map(n=>n.textContent)});", contentWorld: .page) as? String
            throw NativeAcceptanceError(message: "Native table insertion did not paint: \(detail ?? "no diagnostics")")
        }
        let inserted = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectedTable?.data.rows.length===3&&s.selectedTable.data.columns.length===4;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(inserted, "native grid picker inserts a semantic 3 by 4 table", checks: &checks)
        let version = try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version;", contentWorld: .page) as? Int ?? -1
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Edit table\"]').click()")
        try await waitForNativeJavaScript("native table draft", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table\"] textarea[aria-label=\"Paste table data\"]')")
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table\"]');d.querySelector('[aria-label=\"Paste table data\"]').value='Session\\tOwner\\tMinutes\\tOutcome\\nDiscovery\\tTeam A\\t15\\tIdeas\\nBuild\\tTeam B\\t30\\tPrototype';[...d.querySelectorAll('button')].find(b=>b.textContent==='Paste data').click();d.querySelector('[aria-label=\"Cell 2, 2\"]').focus();d.querySelector('[aria-label=\"Cell text colour\"]').click();})()")
        try await waitForNativeJavaScript("native table shared palette", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table\"] .keynope-colour-picker')")
        _ = try await view.evaluateJavaScript("(()=>{const p=document.querySelector('.keynope-colour-picker');p.querySelector('[aria-label=\"HTML color\"]').value='#55AAFF';[...p.querySelectorAll('button')].find(b=>b.textContent==='Apply').click();})()")
        try await waitForNativeJavaScript("native table draft colour", in: view, expression: "document.querySelector('dialog [aria-label=\"Cell text colour\"]')?.getAttribute('aria-description')==='#55AAFF' || document.querySelector('dialog [aria-label=\"Cell text colour\"]')?.getAttribute('aria-description')==='#55aaff'")
        try requireNativeAcceptance(true, "native table cells use the shared modal-aware colour palette", checks: &checks)
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table\"]');d.dispatchEvent(new KeyboardEvent('keydown',{key:'z',metaKey:true,bubbles:true}));})()")
        try await waitForNativeJavaScript("native table local undo", in: view, expression: "document.querySelector('dialog [aria-label=\"Cell text colour\"]')?.getAttribute('aria-description')==='Inherited'")
        _ = try await view.evaluateJavaScript("document.querySelector('dialog[aria-label=\"Edit table\"]').dispatchEvent(new KeyboardEvent('keydown',{key:'y',metaKey:true,bubbles:true}))")
        try await waitForNativeJavaScript("native table local redo", in: view, expression: "document.querySelector('dialog [aria-label=\"Cell text colour\"]')?.getAttribute('aria-description')?.toLowerCase()==='#55aaff'")
        let draftOnly = try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version===\(version);", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(draftOnly, "table draft undo and redo do not modify document history", checks: &checks)
        let tracks = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table\"]'),i=d.querySelector('[aria-label=\"Column width weight\"]'),before=Number(i.value),h=d.querySelector('[aria-label=\"Resize column B\"]');h.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}));const changed=Number(d.querySelector('[aria-label=\"Column width weight\"]').value)>before;[...d.querySelectorAll('button')].find(b=>b.textContent==='Undo').click();return changed&&Number(d.querySelector('[aria-label=\"Column width weight\"]').value)===before&&d.querySelectorAll('[role=\"separator\"]').length===5;})()") as? Bool ?? false
        try requireNativeAcceptance(tracks, "native table track separators resize adjacent weights and undo in the draft", checks: &checks)
        let trackImage = try await view.takeSnapshot(configuration: nil)
        if let tiff = trackImage.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-track-editor.png"))
        }
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table\"]'),size=d.querySelector('[aria-label=\"Cell font size\"]');size.value='46';size.dispatchEvent(new Event('change',{bubbles:true}));[...d.querySelectorAll('button')].find(b=>b.textContent==='Apply').click();})()")
        try await waitForNativeJavaScript("native table content paint", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table\"]') && document.querySelector('#stage .keynope-scene-table')?.textContent.includes('Prototype')")
        let committed = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.version===\(version + 1)&&s.selectedTable.data.cells[1][1].style.color.toLowerCase()==='#55aaff'&&document.querySelectorAll('#stage .keynope-scene-table th').length===4;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(committed, "native Apply renders formatted semantic cells in one document revision", checks: &checks)
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Edit table\"]').click()")
        try await waitForNativeJavaScript("native table cancellation draft", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table\"]')")
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table\"]');[...d.querySelectorAll('button')].find(b=>b.textContent==='Copy range').click();})()")
        try await waitForNativeAcceptance("native table Copy range") { board.string(forType: .string) == "Session" }
        try requireNativeAcceptance(true, "native table Copy range writes readable data to the system clipboard", checks: &checks)
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table\"]'),i=d.querySelector('[aria-label=\"Cell 1, 1\"]');i.value='Discard me';i.dispatchEvent(new Event('input',{bubbles:true}));[...d.querySelectorAll('button')].find(b=>b.textContent==='Cancel').click();})()")
        let cancelled = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.version===\(version + 1)&&s.selectedTable.data.cells[0][0].text==='Session';", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(cancelled, "native Cancel discards the table draft without a history entry", checks: &checks)
        let canvasPaintDiagnostic = try await view.evaluateJavaScript("(()=>{try{renderEditorCanvasOverlay();return 'ok';}catch(e){return String(e)+'\\n'+e.stack;}})()") as? String ?? "Missing canvas paint diagnostics"
        guard canvasPaintDiagnostic == "ok" else { throw NativeAcceptanceError(message: canvasPaintDiagnostic) }
        do {
            try await waitForNativeJavaScript("native selected table canvas", in: view, expression: "!!document.querySelector('.keynope-canvas-element.active')", timeout: 10)
        } catch {
            let detail = try? await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify({appSurface:window.KEYNOPE_APP_SURFACE,selected:s.selected,current:s.current,pageIndex,pageSlide:deck.pages?.[pageIndex]?.slide,overlayFunction:renderEditorCanvasOverlay.toString().slice(0,80),elementIDs:s.slides?.[s.current]?.elements.map(e=>e.id),sceneIDs:[...document.querySelectorAll('#stage [data-object-id]')].map(n=>n.dataset.objectId),hits:[...document.querySelectorAll('.keynope-canvas-element')].map(n=>({index:n.dataset.element,classes:n.className})),tables:document.querySelectorAll('#stage .keynope-scene-table').length,dialogs:[...document.querySelectorAll('dialog')].map(d=>d.getAttribute('aria-label'))});", contentWorld: .page) as? String
            throw NativeAcceptanceError(message: "Native table canvas selection did not paint: \(detail ?? "no diagnostics")")
        }
        _ = try await view.evaluateJavaScript("(()=>{const h=document.querySelector('.keynope-canvas-element.active'),b=h.getBoundingClientRect();h.dispatchEvent(new MouseEvent('dblclick',{bubbles:true,clientX:b.x+b.width/2,clientY:b.y+b.height/2}));})()")
        try await waitForNativeJavaScript("native table canvas editor", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table cells\"] [contenteditable=\"true\"]')")
        _ = try await view.evaluateJavaScript("document.querySelector('dialog[aria-label=\"Edit table cells\"] [contenteditable=\"true\"]').dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}))")
        try await waitForNativeJavaScript("native table cell selection state", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"] [contenteditable=\"true\"]')")
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]');d.querySelector('[aria-label=\"Cell B2\"]').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,cancelable:true}));d.querySelector('[aria-label=\"Cell C2\"]').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,shiftKey:true,cancelable:true}));const f=d.querySelector('[aria-label=\"Canvas cell font\"]');f.value='sans';f.dispatchEvent(new Event('change',{bubbles:true}));})()")
        try await waitForNativeJavaScript("native canvas range font preview", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        _ = try await view.evaluateJavaScript("(()=>{const i=document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Canvas cell font size\"]');i.value='72';i.dispatchEvent(new Event('change',{bubbles:true}));})()")
        try await waitForNativeJavaScript("native canvas range size preview", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        let canvasStylePreview = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]'),s=await fetch('/api/editor/state').then(r=>r.json());return d.querySelectorAll('[role=\"gridcell\"][aria-selected=\"true\"]').length===2&&['1:1','1:2'].every(id=>{const n=d.querySelector('[data-table-cell=\"'+id+'\"] [data-cell-draft] .keynope-shaped-text');return n?.style.fontSize==='72px'&&n.style.fontFamily.includes('KeynopeModern,');})&&s.version===\(version + 1)&&s.selectedTable.data.cells[1][1].style.fontSize===46;", contentWorld: .page) as? Bool ?? false
        if !canvasStylePreview {
            let detail = try? await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]'),s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify({version:s.version,original:s.selectedTable?.data.cells[1][1],selected:d.querySelectorAll('[role=gridcell][aria-selected=true]').length,styles:['1:1','1:2'].map(id=>{const n=d.querySelector('[data-table-cell=\"'+id+'\"] [data-cell-draft] .keynope-shaped-text');return {id,size:n?.style.fontSize,font:n?.style.fontFamily};}),alerts:[...d.querySelectorAll('[role=alert]')].map(n=>n.textContent)});", contentWorld: .page) as? String
            throw NativeAcceptanceError(message: "Native canvas range style preview failed: \(detail ?? "no diagnostics")")
        }
        try requireNativeAcceptance(canvasStylePreview, "native canvas range font and size preview use production layout without changing the deck", checks: &checks)
        let canvasStyleImage = try await view.takeSnapshot(configuration: nil)
        if let tiff = canvasStyleImage.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-canvas-formatting.png"))
        }
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Increase canvas cell font size\"]').click()")
        try await waitForNativeJavaScript("native canvas size increment", in: view, expression: "document.querySelector('[aria-label=\"Canvas cell font size\"]')?.value==='73'&&!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Decrease canvas cell font size\"]').click()")
        try await waitForNativeJavaScript("native canvas size decrement", in: view, expression: "document.querySelector('[aria-label=\"Canvas cell font size\"]')?.value==='72'&&!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        try requireNativeAcceptance(true, "native canvas font steppers display the accepted preview immediately", checks: &checks)
        // Remove the two stepper edits before undoing the original size/font.
        for _ in 0..<2 {
            _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click()")
            try await waitForNativeJavaScript("native canvas undo stepper", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        }
        _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click()")
        try await waitForNativeJavaScript("native canvas undo size preview", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click()")
        try await waitForNativeJavaScript("native canvas undo font preview", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        let canvasStyleUndo = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]'),i=d.querySelector('[aria-label=\"Canvas cell font size\"]');return i.dataset.mixed==='true'&&i.value===''&&!KeynopeTables.isDirty()&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1);", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(canvasStyleUndo, "native canvas range style undo restores mixed values and a clean draft", checks: &checks)
        let canvasTracks = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]'),h=d.querySelector('[aria-label=\"Resize canvas column A\"]'),table=d.querySelector('.keynope-scene-table'),cell=table.querySelector('[data-table-cell=\"0:0\"]'),old=Number(h.getAttribute('aria-valuenow'));h.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true,cancelable:true}));const resized=Number(h.getAttribute('aria-valuenow'))>old&&d.querySelector('.keynope-scene-table')===table&&table.querySelector('[data-table-cell=\"0:0\"]')===cell;[...d.querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click();return resized&&Number(h.getAttribute('aria-valuenow'))===old&&!KeynopeTables.isDirty()&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1);", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(canvasTracks, "native canvas track resizing retains the table and undoes locally without changing the deck", checks: &checks)
        let nativeBorderTracks = try await view.callAsyncJavaScript("""
        const d=document.querySelector('dialog[aria-label="Edit table cells"]'),table=d.querySelector('.keynope-scene-table'),layer=table.querySelector('.keynope-table-borders'),line=layer.querySelector('[data-table-border^="vertical:1:"]'),x=line.getAttribute('x1'),h=d.querySelector('[aria-label="Resize canvas column A"]');
        h.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true,cancelable:true}));
        const valid=line.getAttribute('x1')!==x&&line.isConnected&&table.querySelector('.keynope-table-borders')===layer&&[...table.querySelectorAll('td,th')].every(n=>parseFloat(getComputedStyle(n).borderLeftWidth)===0);
        [...d.querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click();
        return valid&&line.getAttribute('x1')===x&&!KeynopeTables.isDirty()&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1);
        """, contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(nativeBorderTracks, "native track resize retains shared border nodes and updates seam geometry without per-cell doubled borders", checks: &checks)
        let nativeProportionalTracks = try await view.callAsyncJavaScript("""
        const d=document.querySelector('dialog[aria-label="Edit table cells"]'),table=d.querySelector('.keynope-scene-table');
        const pixels=()=>[...d.querySelectorAll('.keynope-table-canvas-track')].filter(n=>n.querySelector('[aria-label^="Select column"]')).map(n=>parseFloat(n.style.width));
        const before=pixels(),h=d.querySelector('[aria-label="Resize canvas column B"]');
        h.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',altKey:true,bubbles:true,cancelable:true}));const after=pixels();
        const valid=after[0]>before[0]&&after[3]<before[3]&&Math.abs(after[0]/after[1]-before[0]/before[1])<1e-6&&Math.abs(after[2]/after[3]-before[2]/before[3])<1e-6&&table===d.querySelector('.keynope-scene-table');
        [...d.querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click();
        return valid&&JSON.stringify(pixels())===JSON.stringify(before)&&!KeynopeTables.isDirty()&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1);
        """, contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(nativeProportionalTracks, "native Option track resizing preserves side ratios fixed bounds and the retained table with one local undo", checks: &checks)
        let nativeReadableTracks = try await view.callAsyncJavaScript("""
        const d=document.querySelector('dialog[aria-label="Edit table cells"]'),h=d.querySelector('[aria-label="Resize canvas column A"]'),scene=await fetch('/api/editor/scene?slide=0').then(r=>r.json()),table=scene.objects.find(o=>o.kind==='table').table;
        const minimum=Math.max(40,...table.cells.filter(c=>c.column===0).map(c=>(3*c.text.size*c.text.widthScale+2*c.contentBounds.x)/c.colSpan));
        for(let i=0;i<100;i++)h.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',bubbles:true,cancelable:true}));
        const width=parseFloat(d.querySelector('[aria-label="Select column A"]').parentElement.style.width),clamped=Math.abs(width-minimum)<.001;
        for(let i=0;i<100;i++)[...d.querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click();
        return clamped&&!KeynopeTables.isDirty()&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1);
        """, contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(nativeReadableTracks, "native canvas track keyboard resizing clamps readable widths and undoes to a clean draft", checks: &checks)
        // Structural edits are local drafts in the same production canvas, not
        // a textarea-only fallback. Every replacement waits for native fonts.
        for label in ["Add row", "Add column"] {
            _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='\(label)').click()")
            try await waitForNativeJavaScript("native canvas \(label)", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        }
        let canvasStructure = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]');return d.querySelectorAll('[role=gridcell]').length===20&&d.querySelectorAll('.keynope-scene-table tr').length===4&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1);", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(canvasStructure, "native canvas inserts rows and columns without changing the deck", checks: &checks)
        for _ in 0..<2 {
            _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click()")
            try await waitForNativeJavaScript("native canvas undo structure", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        }
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]');d.querySelector('[aria-label=\"Cell A1\"]').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,cancelable:true}));d.querySelector('[aria-label=\"Cell B2\"]').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,shiftKey:true,cancelable:true}));[...d.querySelectorAll('button')].find(b=>b.textContent==='Merge cells').click();})()")
        try await waitForNativeJavaScript("native canvas merge", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        let canvasMerge = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]');return d.querySelectorAll('[role=gridcell]').length===9&&d.querySelector('[data-table-cell=\"0:0\"]').textContent.includes('Team A')&&!d.querySelector('[aria-label=\"Cell B2\"]');})()") as? Bool ?? false
        try requireNativeAcceptance(canvasMerge, "native canvas merge collects text and removes covered hit targets", checks: &checks)
        _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='Split cells').click()")
        try await waitForNativeJavaScript("native canvas split", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        let canvasSplit = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]');return d.querySelectorAll('[role=gridcell]').length===12&&d.querySelector('[data-table-cell=\"0:0\"]').textContent.includes('Team A')&&!d.querySelector('[data-table-cell=\"1:1\"]').textContent;})()") as? Bool ?? false
        try requireNativeAcceptance(canvasSplit, "native canvas split retains anchor content and exposes empty cells", checks: &checks)
        for _ in 0..<2 {
            _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click()")
            try await waitForNativeJavaScript("native canvas undo span", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        }
        _ = try await view.evaluateJavaScript("(()=>{const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]'),cell=d.querySelector('[aria-label=\"Cell D3\"]');cell.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,cancelable:true}));const clipboardData=new DataTransfer();clipboardData.setData('text/plain','Expanded\\tColumn\\nNew row\\tLast');cell.dispatchEvent(new ClipboardEvent('paste',{clipboardData,bubbles:true,cancelable:true}));})()")
        try await waitForNativeJavaScript("native canvas expanding paste", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        let canvasPaste = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]');return d.querySelectorAll('[role=gridcell]').length===20&&d.querySelector('[data-table-cell=\"3:4\"]').textContent==='Last'&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1);", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(canvasPaste, "native canvas paste expands tracks and previews the complete matrix locally", checks: &checks)
        let structureImage = try await view.takeSnapshot(configuration: nil)
        if let tiff = structureImage.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-canvas-structure.png"))
        }
        _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='Undo cell change').click()")
        try await waitForNativeJavaScript("native canvas undo paste", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')")
        let cleanStructure = try await view.callAsyncJavaScript("return !KeynopeTables.isDirty()&&document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('[role=gridcell]').length===12&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1);", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(cleanStructure, "native canvas structural undo restores the clean table and original document revision", checks: &checks)
        _ = try await view.evaluateJavaScript("(()=>{const cell=document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Cell A1\"]');cell.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,cancelable:true}));cell.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));})()")
        try await waitForNativeJavaScript("native A1 caret", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Edit cell A1\"]')")
        // Selected-word typography must stay inside the owned caret draft,
        // including when a nested palette temporarily takes native focus.
        _ = try await view.evaluateJavaScript("""
        (()=>{
          const d=document.querySelector('dialog[aria-label="Edit table cells"]'),e=d.querySelector('[aria-label="Edit cell A1"]');
          window.nativeSelectTableRun=()=>{const n=document.createTreeWalker(e,NodeFilter.SHOW_TEXT);n.nextNode();document.getSelection().setBaseAndExtent(n.currentNode,0,n.currentNode,4);document.dispatchEvent(new Event('selectionchange'));};
          window.nativeSelectTableRun();const f=d.querySelector('[aria-label="Canvas cell font"]');f.value='mono';f.dispatchEvent(new Event('change',{bubbles:true}));
          const s=d.querySelector('[aria-label="Canvas cell font size"]');s.value='70';s.dispatchEvent(new Event('change',{bubbles:true}));
          d.querySelector('[aria-label="Canvas cell text colour"]').click();
        })()
        """)
        try await waitForNativeJavaScript("native selected-word palette", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table cells\"] .keynope-colour-picker')")
        _ = try await view.evaluateJavaScript("(()=>{const p=document.querySelector('.keynope-colour-picker');p.querySelector('[aria-label=\"HTML color\"]').value='#ff0055';[...p.querySelectorAll('button')].find(b=>b.textContent==='Apply').click();})()")
        let nativeRunStyles = try await view.callAsyncJavaScript("const e=document.querySelector('[aria-label=\"Edit cell A1\"]'),spans=[...e.querySelectorAll('span')].filter(n=>n.firstChild?.nodeType===Node.TEXT_NODE&&n.textContent),s=await fetch('/api/editor/state').then(r=>r.json());return spans[0]?.textContent==='Sess'&&spans[0].style.fontFamily.includes('KeynopeModernMono')&&spans[0].style.fontSize==='70px'&&spans[0].style.color==='rgb(255, 0, 85)'&&!spans.at(-1).style.fontFamily&&!spans.at(-1).style.fontSize&&s.version===\(version + 1);", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(nativeRunStyles, "native selected-word font size and nested colour picker retain the range without styling the rest of the cell", checks: &checks)
        _ = try await view.evaluateJavaScript("(()=>{const e=document.querySelector('[aria-label=\"Edit cell A1\"]');window.nativeSelectTableRun();e.dispatchEvent(new KeyboardEvent('keydown',{key:'z',metaKey:true,bubbles:true,cancelable:true}));})()")
        try await waitForNativeJavaScript("native selected-word undo", in: view, expression: "document.querySelector('[aria-label=\"Edit cell A1\"] span')?.style.color===''")
        _ = try await view.evaluateJavaScript("document.querySelector('[aria-label=\"Edit cell A1\"]').dispatchEvent(new KeyboardEvent('keydown',{key:'y',metaKey:true,bubbles:true,cancelable:true}))")
        try await waitForNativeJavaScript("native selected-word redo", in: view, expression: "document.querySelector('[aria-label=\"Edit cell A1\"] span')?.style.color==='rgb(255, 0, 85)'")
        _ = try await view.evaluateJavaScript("window.nativeSelectTableRun();document.querySelector('[aria-label=\"Increase canvas cell font size\"]').click()")
        try await waitForNativeJavaScript("native selected-word stepper", in: view, expression: "document.querySelector('[aria-label=\"Canvas cell font size\"]')?.value==='71'&&document.querySelector('[aria-label=\"Edit cell A1\"] span')?.style.fontSize==='71px'")
        try requireNativeAcceptance(true, "native selected-word undo redo and font stepper update only the selected range", checks: &checks)
        let runImage = try await view.takeSnapshot(configuration: nil)
        if let tiff = runImage.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-canvas-rich-runs.png"))
        }
        _ = try await view.evaluateJavaScript("(()=>{const e=document.querySelector('[aria-label=\"Edit cell A1\"]');for(let i=0;i<4;i++)e.dispatchEvent(new KeyboardEvent('keydown',{key:'z',metaKey:true,bubbles:true,cancelable:true}));e.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));delete window.nativeSelectTableRun;})()")
        let nativeRunUndo = try await view.callAsyncJavaScript("return !KeynopeTables.isDirty()&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1);", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(nativeRunUndo, "native selected-word undo restores the clean cell draft and leaves document history untouched", checks: &checks)
        _ = try await view.evaluateJavaScript("(()=>{const cell=document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Cell A1\"]');cell.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,cancelable:true}));cell.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));})()")
        try await waitForNativeJavaScript("native A1 typing caret", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Edit cell A1\"]')")
        let canvasInputDiagnostic = try await view.evaluateJavaScript("(()=>{try{const editor=document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Edit cell A1\"]'),r=document.createRange();r.selectNodeContents(editor);const s=document.getSelection();s.removeAllRanges();s.addRange(r);editor.dispatchEvent(new InputEvent('beforeinput',{inputType:'insertText',data:'Native canvas draft',bubbles:true,cancelable:true}));return 'ok';}catch(e){return String(e)+'\\n'+e.stack;}})()") as? String ?? "Missing canvas input diagnostics"
        guard canvasInputDiagnostic == "ok" else { throw NativeAcceptanceError(message: canvasInputDiagnostic) }
        let canvasDraftState = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]'),e=d?.querySelector('[aria-label=\"Edit cell A1\"]');return JSON.stringify({text:e?.textContent,editors:[...d.querySelectorAll('[contenteditable]')].map(e=>e.getAttribute('aria-label')),status:d.querySelector('[role=\"status\"]')?.textContent,dirty:KeynopeTables.isDirty(),version:(await fetch('/api/editor/state').then(r=>r.json())).version});", contentWorld: .page) as? String ?? "Missing canvas state"
        let canvasDraft = try await view.callAsyncJavaScript("return document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Edit cell A1\"]')?.textContent==='Native canvas draft'&&(await fetch('/api/editor/state').then(r=>r.json())).version===\(version + 1)&&KeynopeTables.isDirty();", contentWorld: .page) as? Bool ?? false
        guard canvasDraft else { throw NativeAcceptanceError(message: "Native canvas draft was not retained: \(canvasDraftState)") }
        try requireNativeAcceptance(canvasDraft, "native direct canvas cell typing owns a dirty draft without changing document history", checks: &checks)
        let canvasImage = try await view.takeSnapshot(configuration: nil)
        if let tiff = canvasImage.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-canvas-cell-editor.png"))
        }
        _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='Cancel').click()")
        let canvasCancelled = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.version===\(version + 1)&&s.selectedTable.data.cells[0][0].text==='Session'&&!document.querySelector('dialog[aria-label=\"Edit table cells\"]');", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(canvasCancelled, "native canvas Cancel restores the rendered table and discards its cell draft", checks: &checks)
        _ = try await view.callAsyncJavaScript("await Promise.race([Promise.all([KeynopeScene.ensureFonts(window.KEYNOPE_MODERN_FONT_URL),modernPageView.ready,document.fonts.ready]),new Promise((_,reject)=>setTimeout(()=>reject(Error('Table snapshot layout readiness timed out')),10000))]);await Promise.race([new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))),new Promise(resolve=>setTimeout(resolve,500))]);return true;", contentWorld: .page)
        let image = try await view.takeSnapshot(configuration: nil)
        if let tiff = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-editor.png"))
        }
        let saved = root.appendingPathComponent("table-saved.md")
        _ = try await view.evaluateJavaScript("renderEditorCanvasOverlay();(()=>{const h=document.querySelector('.keynope-canvas-element.active'),b=h.getBoundingClientRect();h.dispatchEvent(new MouseEvent('dblclick',{bubbles:true,clientX:b.x+b.width/2,clientY:b.y+b.height/2}));})()")
        try await waitForNativeJavaScript("native pending table Save draft", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table cells\"] [contenteditable=\"true\"]')")
        _ = try await view.evaluateJavaScript("document.querySelector('dialog[aria-label=\"Edit table cells\"] [contenteditable=\"true\"]').dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}))")
        try await waitForNativeJavaScript("native pending Save cell range", in: view, expression: "!document.querySelector('dialog[aria-label=\"Edit table cells\"] [contenteditable=\"true\"]')")
        _ = try await view.evaluateJavaScript("(()=>{const c=document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Cell C1\"]');c.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,cancelable:true}));c.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));})()")
        try await waitForNativeJavaScript("native pending Save caret", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Edit cell C1\"]')")
        _ = try await view.evaluateJavaScript("(()=>{const e=document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Edit cell C1\"]'),r=document.createRange();r.selectNodeContents(e);const s=document.getSelection();s.removeAllRanges();s.addRange(r);e.dispatchEvent(new InputEvent('beforeinput',{inputType:'insertText',data:'Saved from caret',bubbles:true,cancelable:true}));})()")
        expectNativePanel(title: "Save Keynope Presentation", destination: saved, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Save As…")
        try await waitForNativeAcceptance("native table Save As") { !self.savingDocument && FileManager.default.fileExists(atPath: saved.path) && !self.documentDirty }
        let markdown = try String(contentsOf: saved, encoding: .utf8)
        try requireNativeAcceptance(markdown.contains("keynope-objects") && markdown.contains("[table]"), "native Save embeds table data and the stable table reference", checks: &checks)
        let savedCaret = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectedTable.data.cells[0][2].runs.map(r=>r.text).join('')==='Saved from caret'&&!KeynopeTables.isDirty()&&!document.querySelector('dialog[aria-label=\"Edit table cells\"]');", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(savedCaret, "native Save As commits the active canvas caret before writing the deck", checks: &checks)
        view.reload()
        try await waitForNativeJavaScript("native saved table reload", in: view, expression: "document.querySelector('#stage .keynope-scene-table')?.textContent.includes('Prototype')")
        try requireNativeAcceptance(true, "native WebView reload preserves the authored table projection", checks: &checks)
        try await postNativeAcceptanceEditorAction(["action": "undo"])
        try await waitForNativeJavaScript("native canvas table document undo", in: view, expression: "document.querySelector('#stage .keynope-scene-table')?.textContent.includes('Prototype') && !document.querySelector('#stage .keynope-scene-table').textContent.includes('Saved from caret')")
        try await postNativeAcceptanceEditorAction(["action": "undo"])
        try await waitForNativeJavaScript("native table document undo", in: view, expression: "!!document.querySelector('#stage .keynope-scene-table') && !document.querySelector('#stage .keynope-scene-table').textContent.includes('Prototype')")
        try await postNativeAcceptanceEditorAction(["action": "redo"])
        try await waitForNativeJavaScript("native table document redo", in: view, expression: "document.querySelector('#stage .keynope-scene-table')?.textContent.includes('Prototype')")
        try await postNativeAcceptanceEditorAction(["action": "redo"])
        try await waitForNativeJavaScript("native canvas table document redo", in: view, expression: "document.querySelector('#stage .keynope-scene-table')?.textContent.includes('Saved from caret')")
        try requireNativeAcceptance(true, "native document undo and redo restore the full table payload and paint", checks: &checks)
        let powerpoint = root.appendingPathComponent("table-editable.pptx")
        expectNativePanel(title: "Export Keynope Presentation as PowerPoint", destination: powerpoint, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Export Presentation as PowerPoint…")
        try await waitForNativeAcceptance("native editable table PowerPoint export", timeout: 60) { FileManager.default.fileExists(atPath: powerpoint.path) }
        try requireNativePanelConsumed("native table PowerPoint export used NSSavePanel", checks: &checks)
        let powerpointBytes = try Data(contentsOf: powerpoint)
        try requireNativeAcceptance(powerpointBytes.starts(with: Array("PK".utf8)), "native table PowerPoint export produced package bytes", checks: &checks)
        expectNativePanel(title: "Open a Keynope or PowerPoint Presentation", destination: powerpoint, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Open…")
        try await waitForNativeJavaScript("native editable table PowerPoint import", in: view, expression: "document.querySelector('#stage .keynope-scene-table')?.textContent.includes('Prototype') && [...document.querySelectorAll('#stage [data-object-id]')].some(n=>n.dataset.objectId?.startsWith('pptx-'))")
        let importedTable = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());const index=s.slides[0].elements.findIndex(e=>e.kind==='table'&&e.objectId?.startsWith('pptx-table-'));if(index<0)return false;const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'select-element',element:index})});if(!r.ok)return false;const state=await r.json();return state.selectedTable?.data.cells[1][1].style.color.toLowerCase()==='#55aaff'&&state.selectedTable.data.cells[2][3].text==='Prototype';", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(importedTable, "native Open imports the PowerPoint table as editable cells with colour and content", checks: &checks)
        _ = try await view.callAsyncJavaScript("await Promise.race([Promise.all([modernPageView.ready,document.fonts.ready]),new Promise((_,reject)=>setTimeout(()=>reject(Error('Imported table readiness timed out')),10000))]);await new Promise(resolve=>setTimeout(resolve,500));return true;", contentWorld: .page)
        let roundTripImage = try await view.takeSnapshot(configuration: nil)
        if let tiff = roundTripImage.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-pptx-roundtrip.png"))
        }
        let fitRevision = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),data=structuredClone(s.selectedTable.data);data.cells[1][1].text='One\\nTwo\\nThree\\nFour\\nFive';delete data.cells[1][1].runs;const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'set-table',slide:0,sceneRevision:s.version,objectId:s.selectedTable.objectId,tableData:data,geometry:{x:24,y:8,width:160,height:12}})});if(!r.ok)throw Error(await r.text());return (await r.json()).version;", contentWorld: .page) as? Int ?? -1
        view.reload()
        try await waitForNativeJavaScript("native table fit control", in: view, expression: "!!document.querySelector('[aria-label=\"Fit table to content\"]')&&document.querySelector('#stage .keynope-scene-table')?.textContent.includes('Five')")
        _ = try await view.evaluateJavaScript("renderEditorCanvasOverlay();(()=>{const h=document.querySelector('.keynope-canvas-element.active'),b=h.getBoundingClientRect();h.dispatchEvent(new MouseEvent('dblclick',{bubbles:true,clientX:b.x+b.width/2,clientY:b.y+b.height/2}));})()")
        try await waitForNativeJavaScript("native overflow caret", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table cells\"] [contenteditable=true]')")
        _ = try await view.evaluateJavaScript("document.querySelector('dialog[aria-label=\"Edit table cells\"] [contenteditable=true]').dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}))")
        do {
            try await waitForNativeJavaScript("native clipped-cell warning", in: view, expression: "!!document.querySelector('dialog[aria-label=\"Edit table cells\"] .keynope-table-canvas-cell.overflow')&&document.querySelector('[aria-label=\"Table content warnings\"]')?.textContent.includes('text clipped')")
        } catch {
            let detail = try? await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),d=document.querySelector('dialog[aria-label=\"Edit table cells\"]'),scene=await fetch('/api/editor/scene?slide=0').then(r=>r.json());return JSON.stringify({table:s.selectedTable,object:scene.objects.find(o=>o.kind==='table'),warnings:d?.querySelector('[aria-label=\"Table content warnings\"]')?.textContent,shaped:[...d.querySelectorAll('.keynope-shaped-text')].map(n=>({text:n.textContent,size:n.style.fontSize,height:n.offsetHeight,width:n.offsetWidth,visibility:getComputedStyle(n).visibility})),alerts:[...d.querySelectorAll('[role=alert]')].map(n=>n.textContent)});", contentWorld: .page) as? String
            if let detail { try detail.write(to: root.appendingPathComponent("table-overflow-diagnostics.json"), atomically: true, encoding: .utf8) }
            let image = try? await view.takeSnapshot(configuration: nil)
            if let tiff = image?.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
                try png.write(to: root.appendingPathComponent("table-overflow-failure.png"))
            }
            throw error
        }
        try requireNativeAcceptance(true, "native table editing diagnoses clipped cells without shrinking typography", checks: &checks)
        _ = try await view.evaluateJavaScript("[...document.querySelector('dialog[aria-label=\"Edit table cells\"]').querySelectorAll('button')].find(b=>b.textContent==='Cancel').click();document.querySelector('[aria-label=\"Fit table to content\"]').click()")
        let fitAccepted = try await view.callAsyncJavaScript("for(let i=0;i<100;i++){const s=await fetch('/api/editor/state').then(r=>r.json());if(s.version===\(fitRevision + 1)){const scene=await fetch('/api/editor/scene?slide=0').then(r=>r.json()),host=document.createElement('div');Object.assign(host.style,{position:'fixed',left:'-100000px',visibility:'hidden'});document.body.append(host);const v=KeynopeScene.mount(host,scene);try{await v.ready;window.nativeFittedTable=structuredClone(s.selectedTable.data);return s.selectionCapabilities.geometry.height>12&&s.selectedTable.data.cells[1][1].style.fontSize===46&&v.tableContentMetrics(s.selectedTable.objectId).every(c=>!c.overflow);}finally{v.dispose();host.remove();}}await new Promise(resolve=>setTimeout(resolve,50));}return false;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(fitAccepted, "native Fit table grows tracks and bounds in one revision while preserving text size", checks: &checks)
        try await waitForNativeJavaScript("native fitted table painted", in: view, expression: "document.querySelector('#stage .keynope-scene-table')?.offsetHeight>300")
        // The isolated background acceptance WebView throttles animation
        // frames. Explicitly paint the overlay after scene/state readiness,
        // as for the existing native canvas-entry checks above.
        try await waitForNativeJavaScript("native fitted table selection bounds", in: view, expression: "(()=>{renderEditorCanvasOverlay();const table=document.querySelector('#stage .keynope-scene-table')?.getBoundingClientRect(),hit=document.querySelector('.keynope-canvas-element.active')?.getBoundingClientRect();return !!table&&!!hit&&Math.abs(table.height-hit.height)<2;})()")
        let fitImage = try await view.takeSnapshot(configuration: nil)
        if let tiff = fitImage.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-content-fit.png"))
        }
        try await postNativeAcceptanceEditorAction(["action": "undo"])
        let fitUndo = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities.geometry.height===12&&s.selectedTable.data.cells[1][1].text.includes('Five');", contentWorld: .page) as? Bool ?? false
        try await postNativeAcceptanceEditorAction(["action": "redo"])
        let fitRedo = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.selectionCapabilities.geometry.height>12&&JSON.stringify(s.selectedTable.data)===JSON.stringify(window.nativeFittedTable);", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(fitUndo && fitRedo, "native fitted table undo and redo restore both tracks and bounds", checks: &checks)
        let portableIndex = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),data={version:1,rows:[{height:1},{height:1},{height:1}],columns:[{width:2},{width:3},{width:2}],style:{header:true,banded:true,headerFill:'#163c62',bandFill:'#243044',cell:{fontId:'mono',fontSize:48,color:'#ffffff',padding:8}},cells:[[{colSpan:3,runs:[{text:'Workshop ',bold:true},{text:'plan',fontId:'c64',fontSize:64,color:'#55aaff'}]},{hidden:true},{hidden:true}],[{rowSpan:2,text:'Shared owner'},{runs:[{text:'Discuss ',fontSize:50},{text:'together',italic:true,color:'#ff0055'}]},{text:'10 min'}],[{hidden:true},{text:'Build'},{text:'20 min'}]]};const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'set-table',slide:s.current,sceneRevision:s.version,objectId:s.selectedTable.objectId,tableData:data})});if(!r.ok)throw Error(await r.text());window.nativePortableTable=JSON.stringify(data);return s.slides[s.current].elements.findIndex(e=>e.id===s.selectedTable.objectId);", contentWorld: .page) as? Int ?? -1
        try requireNativeAcceptance(portableIndex >= 0, "native portable merged table fixture uses the production table command", checks: &checks)
        try await waitForNativeJavaScript("native portable merged table paint", in: view, expression: "document.querySelector('#stage .keynope-scene-table')?.textContent.includes('Workshop')===true")
        try await verifyNativeObjectClipboard(checks: &checks, element: portableIndex, useMenuShortcuts: false)
        let portableCopy = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),d=s.selectedTable?.data;if(!d)return false;return d.cells[0][0].colSpan===3&&d.cells[1][0].rowSpan===2&&d.cells[0][0].runs[1].fontId==='c64'&&d.cells[0][0].runs[1].fontSize===64&&d.cells[1][1].runs[1].color==='#ff0055'&&d.columns[1].width===3;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(portableCopy, "native HTML clipboard preserves merged spans, tracks and mixed run typography", checks: &checks)
        let nativeThemeTable = try await view.callAsyncJavaScript("""
        const send=async data=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});if(!r.ok)throw Error(await r.text());return r.json();};
        let s=await fetch('/api/editor/state').then(r=>r.json());const id=s.selectedTable.objectId;
        // Clipboard acceptance intentionally pasted overlapping copies. Keep
        // only the target for this independent native theme/paint fixture.
        const others=s.slides[s.current].elements.filter(e=>e.kind==='table'&&e.id!==id).map(e=>e.id);
        if(others.length)await send({action:'delete-objects',slide:s.current,sceneRevision:s.version,objectIds:others});
        s=await fetch('/api/editor/state').then(r=>r.json());const slide=structuredClone(s.slides[s.current]);slide.fg='';slide.bg='';slide.headerFg='';
        await send({action:'update-slide',slideData:slide});
        s=await fetch('/api/editor/state').then(r=>r.json());await send({action:'select-element',element:s.slides[s.current].elements.findIndex(e=>e.id===id)});
        s=await fetch('/api/editor/state').then(r=>r.json());const data=structuredClone(s.selectedTable.data);
        data.style={header:true,banded:true,cell:{}};
        data.cells[1][2].style={color:'#ff0055',fill:'none',fontId:'c64',fontSize:51};
        await send({action:'set-table',slide:s.current,sceneRevision:s.version,objectId:s.selectedTable.objectId,tableData:data});
        window.nativeThemeTableData=JSON.stringify(data);
        const font={fontId:'mono',size:43,width:81,lineHeight:1.4};
        const theme={version:1,name:'Native table theme',colors:{background:'#ffffff',surface:'#eef2f6',text:'#263445',heading:'#172033',muted:'#64748b',line:'#94a3b8',accent:['#713fd0','#0e7490','#b45309','#be123c','#15803d','#4338ca']},fonts:{body:font,heading:font,mono:font}};
        await send({action:'set-deck-theme',themeData:theme});
        const scene=await fetch('/api/editor/scene?slide='+s.current).then(r=>r.json()),table=scene.objects.find(o=>o.id===s.selectedTable.objectId).table;
        window.nativeThemeTableId=s.selectedTable.objectId;
        window.nativeThemeTableBefore=JSON.stringify(table);
        return table.cells[0].fill==='#713fd0'&&table.cells[1].fill==='#eef2f6'&&table.cells[1].text.size===43&&table.cells[1].text.fontId==='mono'&&table.cells[1].borderColor==='#94a3b8'&&table.cells[3].paint.color==='#ff0055'&&table.cells[3].text.size===51;
        """, contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(nativeThemeTable, "native tables inherit theme header, band, border and typography while explicit cell styles remain intact", checks: &checks)
        try await waitForNativeJavaScript("native themed table paint", in: view, expression: "[...document.querySelectorAll('#stage [data-table-cell=\"0:0\"]')].some(e=>getComputedStyle(e).backgroundColor==='rgb(113, 63, 208)')")
        let nativeSharedBorders = try await view.callAsyncJavaScript("""
        const s=await fetch('/api/editor/state').then(r=>r.json()),scene=await fetch('/api/editor/scene?slide='+s.current).then(r=>r.json()),table=scene.objects.find(o=>o.kind==='table').table,painted=document.querySelector('#stage .keynope-scene-table'),lines=[...painted.querySelectorAll('.keynope-table-borders line')];
        const expected=table.borders.map(e=>[e.axis+':'+e.track+':'+e.from+':'+e.to,e.color,e.width]);
        const actual=lines.map(n=>[n.dataset.tableBorder,n.getAttribute('stroke'),Number(n.getAttribute('stroke-width'))]);
        return JSON.stringify(expected)===JSON.stringify(actual)&&!lines.some(n=>n.dataset.tableBorder.startsWith('vertical:1:0:'))&&[...painted.querySelectorAll('td,th')].every(n=>parseFloat(getComputedStyle(n).borderLeftWidth)===0);
        """, contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(nativeSharedBorders, "native merged tables paint each production-resolved seam exactly once with no merged interior edge", checks: &checks)
        let themeImage = try await view.takeSnapshot(configuration: nil)
        if let tiff = themeImage.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-theme-inheritance.png"))
        }
        let nativeThemeHistory = try await view.callAsyncJavaScript("""
        const send=async data=>{const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(data)});if(!r.ok)throw Error(await r.text());};
        const s=await fetch('/api/editor/state').then(r=>r.json()),id=s.selectedTable.objectId,original=JSON.stringify(s.selectedTable.data),theme=structuredClone(s.theme);
        const get=async()=>{const scene=await fetch('/api/editor/scene?slide='+s.current).then(r=>r.json());const object=scene.objects.find(o=>o.id===id);if(!object)throw Error('Missing theme table '+id+'; projected '+scene.objects.map(o=>o.id).join(','));return object.table;};
        const before=JSON.stringify(await get());theme.colors.accent[0]='#ffbf00';theme.colors.surface='#222c3a';theme.fonts.body.size=47;
        await send({action:'set-deck-theme',themeData:theme});
        const changed=await get();await send({action:'undo'});const undone=await get();await send({action:'redo'});const redone=await get(),state=await fetch('/api/editor/state').then(r=>r.json());
        return changed.cells[0].fill==='#ffbf00'&&changed.cells[1].text.size===47&&changed.cells[3].text.size===51&&JSON.stringify(undone)===before&&JSON.stringify(changed)===JSON.stringify(redone)&&JSON.stringify(state.selectedTable.data)===original;
        """, contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(nativeThemeHistory, "native table theme changes undo and redo without materializing styles into the authored cells", checks: &checks)
        let nativeFourEdges = try await view.callAsyncJavaScript("""
        const s=await fetch('/api/editor/state').then(r=>r.json()),data={version:1,rows:[{height:1}],columns:[{width:1}],style:{cell:{fontId:'mono',fontSize:43,color:'#ffffff',fill:'#172033',padding:12}},cells:[[{text:'Independent editable edges',style:{borders:{left:{color:'none',width:0},right:{color:'#ff0055',width:4},top:{color:'#00ffaa',width:2},bottom:{color:'#4455ff',width:1}}}}]]};
        const r=await fetch('/api/editor/action',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({action:'set-table',slide:s.current,sceneRevision:s.version,objectId:s.selectedTable.objectId,tableData:data,geometry:{x:24,y:10,width:140,height:28}})});if(!r.ok)throw Error(await r.text());return true;
        """, contentWorld: .page) as? Bool ?? false
        try await waitForNativeJavaScript("native four border edges", in: view, expression: "(()=>{const t=document.querySelector('#stage .keynope-scene-table'),lines=[...t?.querySelectorAll('.keynope-table-borders line')||[]];return t?.textContent.includes('Independent editable edges')&&lines.length===3&&lines.some(n=>n.getAttribute('stroke')==='#ff0055'&&n.getAttribute('stroke-width')==='4')&&lines.some(n=>n.getAttribute('stroke')==='#00ffaa'&&n.getAttribute('stroke-width')==='2');})()")
        try requireNativeAcceptance(nativeFourEdges, "native table scene preserves four independent edge styles including an explicit Off side", checks: &checks)
        let edgePowerPoint = root.appendingPathComponent("table-four-edges.pptx")
        expectNativePanel(title: "Export Keynope Presentation as PowerPoint", destination: edgePowerPoint, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Export Presentation as PowerPoint…")
        try await waitForNativeAcceptance("native four-sided border PowerPoint export", timeout: 60) { FileManager.default.fileExists(atPath: edgePowerPoint.path) }
        // Export deliberately does not save the document. Save the owned
        // fixture before Open rather than racing the asynchronous dirty-state
        // bridge and accidentally depending on an unsaved-changes modal.
        let edgeMarkdown = root.appendingPathComponent("table-four-edges.md")
        expectNativePanel(title: "Save Keynope Presentation", destination: edgeMarkdown, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Save As…")
        try await waitForNativeAcceptance("native four-sided border Markdown Save") { !self.savingDocument && FileManager.default.fileExists(atPath: edgeMarkdown.path) && !self.documentDirty }
        try requireNativeAcceptance(try String(contentsOf: edgeMarkdown, encoding: .utf8).contains("keynope-objects"), "native Save embeds independent table border metadata before the PPTX Open round trip", checks: &checks)
        expectNativePanel(title: "Open a Keynope or PowerPoint Presentation", destination: edgePowerPoint, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Open…")
        try await waitForNativeJavaScript("native four-sided PowerPoint Open", in: view, expression: "document.querySelector('#stage .keynope-scene-table')?.textContent.includes('Independent editable edges') && [...document.querySelectorAll('#stage [data-object-id]')].some(n=>n.dataset.objectId?.startsWith('pptx-'))")
        let nativeEdgeRoundTrip = try await view.callAsyncJavaScript("""
        const s=await fetch('/api/editor/state').then(r=>r.json()),e=s.slides[s.current].elements.find(e=>e.kind==='table');if(!e)return false;
        const scene=await fetch('/api/editor/scene?slide='+s.current).then(r=>r.json()),table=scene.objects.find(o=>o.kind==='table').table,edges=table.borders;
        return edges.length===3&&edges.some(e=>e.axis==='vertical'&&e.track===1&&e.color.toLowerCase()==='#ff0055'&&e.width===4)&&edges.some(e=>e.axis==='horizontal'&&e.track===0&&e.color.toLowerCase()==='#00ffaa'&&e.width===2)&&edges.some(e=>e.axis==='horizontal'&&e.track===1&&e.color.toLowerCase()==='#4455ff'&&e.width===1);
        """, contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(nativeEdgeRoundTrip, "native NSSavePanel export and Open preserve independent editable table edge colours and widths", checks: &checks)
        let edgeImage = try await view.takeSnapshot(configuration: nil)
        if let tiff = edgeImage.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) {
            try png.write(to: root.appendingPathComponent("table-four-sided-pptx.png"))
        }
    }

    private func verifyNativeBuilds(root: URL, checks: inout [String]) async throws {
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing native editor")}
        try await waitForNativeJavaScript("Builds editor readiness",in:view,expression:"typeof KeynopeBuilds==='object'&&!!document.querySelector('[aria-label=\"Edit slide builds\"]')")
        _ = try await view.callAsyncJavaScript("await editorAction({action:'select-element',element:0});if(document.querySelector('[aria-label=\"Edit slide builds\"]').offsetParent===null)[...document.querySelectorAll('button')].find(b=>b.textContent==='Slide').click();document.querySelector('[aria-label=\"Edit slide builds\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native build dialog",in:view,expression:"!!document.querySelector('dialog[aria-label=\"Slide builds\"][open]')")
        _ = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Slide builds\"]');d.querySelector('[aria-label=\"Add build for selected objects\"]').click();const input=d.querySelector('[aria-label=\"Build effect\"]');input.value='fade';input.dispatchEvent(new Event('change',{bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native build preview",in:view,expression:"!!document.querySelector('.keynope-build-preview [data-build-pending=\"true\"]')")
        let version=try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version;",arguments:[:],in:nil,contentWorld:.page) as? Int ?? -1
        try requireNativeAcceptance(true,"native Builds editor packages ordered settings and hidden initial targets",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('dialog[aria-label=\"Slide builds\"]').dispatchEvent(new KeyboardEvent('keydown',{key:'z',metaKey:true,bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        let localUndo=try await view.callAsyncJavaScript("return document.querySelector('dialog [aria-label=\"Build effect\"]').value==='appear'&&(await fetch('/api/editor/state').then(r=>r.json())).version===version;",arguments:["version":version],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(localUndo==true,"native build draft Undo does not change the document",checks:&checks)
        _ = try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Slide builds\"]');d.dispatchEvent(new KeyboardEvent('keydown',{key:'y',metaKey:true,bubbles:true}));[...d.querySelectorAll('button')].find(b=>b.textContent==='Next build').click();",arguments:[:],in:nil,contentWorld:.page)
        let forward=try await view.callAsyncJavaScript("return !!document.querySelector('.keynope-build-preview [data-build-pending=\"false\"]')&&document.querySelector('dialog [aria-label=\"Build effect\"]').value==='fade';",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(forward==true,"native draft Redo and forward preview reveal the selected object",checks:&checks)
        _ = try await view.callAsyncJavaScript("await new Promise(resolve=>setTimeout(resolve,650));",arguments:[:],in:nil,contentWorld:.page)
        let settled=try await view.callAsyncJavaScript("const n=document.querySelector('.keynope-build-preview [data-build-pending=\"false\"]');return !!n&&Number(getComputedStyle(n).opacity)>.99;",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(settled==true,"native occluded fade settles to the visible authored opacity",checks:&checks)
        let snapshot=try await view.takeSnapshot(configuration:nil)
        if let tiff=snapshot.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]){try png.write(to:root.appendingPathComponent("build-editor.png"))}
        _ = try await view.callAsyncJavaScript("[...document.querySelector('dialog[aria-label=\"Slide builds\"]').querySelectorAll('button')].find(b=>b.textContent==='Apply').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native build Apply",in:view,expression:"!document.querySelector('dialog[aria-label=\"Slide builds\"]')")
        let applied=try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return s.version===version+1&&s.slideBuild.steps.length===1&&s.slideBuild.steps[0].effect==='fade';",arguments:["version":version],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(applied==true,"native Apply commits one build transaction",checks:&checks)
        _ = try await view.callAsyncJavaScript("await editorAction({action:'undo'});",arguments:[:],in:nil,contentWorld:.page)
        let undone=try await view.callAsyncJavaScript("return ((await fetch('/api/editor/state').then(r=>r.json())).slideBuild.steps?.length||0)===0;",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(undone==true,"native document Undo restores the clean timeline",checks:&checks)
        _ = try await view.callAsyncJavaScript("await editorAction({action:'redo'});await fetch('/presenter-status',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode:'main',paused:false})});await syncPresenterState();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native initial build cursor",in:view,expression:"remoteBuildCursor?.step===0&&!!document.querySelector('#stage [data-build-pending=\"true\"]')")
        try await waitForNativeJavaScript("native isolated next-state preview",in:view,expression:"document.querySelector('.keynope-next-state')?.dataset.nextKind==='build'&&!!document.querySelector('.keynope-next-state [data-build-pending=\"false\"]')")
        let previewIsolated=try await view.callAsyncJavaScript("return remoteBuildCursor.step===0&&!!document.querySelector('#stage [data-build-pending=\"true\"]');",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(previewIsolated==true,"native next-state preview shows the upcoming reveal without advancing the audience",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('[aria-label=\"Hide next state preview\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native preview hidden preference",in:view,expression:"document.querySelector('.keynope-next-state')?.hidden===true")
        _ = try await view.callAsyncJavaScript("document.querySelector('[aria-label=\"Next state preview\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native preview restored",in:view,expression:"document.querySelector('.keynope-next-state')?.hidden===false")
        // A second real WebKit view follows the loopback cursor, without opening
        // a foreground/fullscreen window or touching the installed application.
        let follower=WKWebView(frame:NSRect(x:0,y:0,width:1280,height:800),configuration:WKWebViewConfiguration())
        follower.load(URLRequest(url:presenterURLForSurface("main")))
        defer {follower.stopLoading()}
        try await waitForNativeJavaScript("native external initial build",in:follower,expression:"remoteBuildCursor?.step===0&&!!document.querySelector('[data-build-pending=\"true\"]')")
        _ = try await view.callAsyncJavaScript("window.__buildProofVersion=(await fetch('/api/editor/state').then(r=>r.json())).version;document.querySelector('button[aria-label=\"Next presentation state\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native external reveal",in:follower,expression:"remoteBuildCursor?.step===1&&!!document.querySelector('[data-build-pending=\"false\"]')")
        let buildClean=try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version===window.__buildProofVersion&&remoteBuildCursor.step===1;",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(buildClean==true,"native editor and second WebKit display share reveals without document edits",checks:&checks)
        try await waitForNativeJavaScript("native preview skips tab-only page",in:view,expression:"document.querySelector('.keynope-next-state')?.dataset.nextKind==='slide'&&document.querySelector('.keynope-next-state strong')?.textContent==='Next slide · 2'&&getComputedStyle(document.querySelector('.keynope-next-state-canvas')).visibility==='visible'")
        let nextSnapshot=try await view.takeSnapshot(configuration:nil)
        if let tiff=nextSnapshot.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]){try png.write(to:root.appendingPathComponent("next-state-native.png"))}
        try requireNativeAcceptance(true,"native live preview shows the next non-tab slide after the final reveal",checks:&checks)
        _ = try await follower.callAsyncJavaScript("await navigatePresenterBuild('previous');",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native editor backward reveal",in:view,expression:"remoteBuildCursor?.step===0&&!!document.querySelector('#stage [data-build-pending=\"true\"]')")
        try requireNativeAcceptance(true,"native second-display navigation restores the same previous build in the editor",checks:&checks)
        _ = try await view.callAsyncJavaScript("await fetch('/presenter-status',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode:'none',paused:false})});await syncPresenterState();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native build stop",in:view,expression:"remoteBuildCursor===null&&![...document.querySelectorAll('#stage [data-object-id]')].some(n=>n.style.visibility==='hidden')")
        try requireNativeAcceptance(true,"native Stop clears runtime playback and restores full authoring visibility",checks:&checks)
    }

    private func verifyNativeActivityBuilds(root: URL, checks: inout [String]) async throws {
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing activity/build editor")}
        _ = try await view.callAsyncJavaScript("""
        window.__activityConnections=0;globalThis.keynopeConnectActivity=async options=>{window.__activityConnections++;window.__activityOptions=options;return {identity:'owned-presenter',usesState:true,async send(){},async close(){}};};
        await fetch('/presenter-status',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode:'external',paused:false})});await syncPresenterState();await navigatePresenterBuild('next');openEngagementRuntime();
        """, contentWorld:.page)
        try await waitForNativeJavaScript("native activity Ready preserves reveal",in:view,expression:"remoteBuildCursor?.step===1&&keynopeEngagementRuntime?.phase===0&&!!document.querySelector('.keynope-engagement-overlay')")
        try requireNativeAcceptance(true,"native opening an activity stays Ready and preserves the current reveal",checks:&checks)
        let follower=WKWebView(frame:NSRect(x:0,y:0,width:1280,height:800),configuration:WKWebViewConfiguration());follower.load(URLRequest(url:presenterURLForSurface("external")));defer{follower.stopLoading()}
        _ = try await view.callAsyncJavaScript("[...document.querySelectorAll('.keynope-engagement-overlay button')].find(b=>b.textContent==='Start').click();",contentWorld:.page)
        try await waitForNativeJavaScript("native running activity on external display",in:follower,expression:"remoteBuildCursor?.step===1&&keynopeEngagementRuntime?.phase===1&&!!keynopeEngagementOverlay")
        try await waitForNativeJavaScript("native owned participant channel",in:view,expression:"keynopeEngagementRuntime?.roomReady===true")
        try requireNativeAcceptance(true,"native explicit Start displays activity on the external view without restarting builds",checks:&checks)
        _ = try await view.callAsyncJavaScript("[...document.querySelectorAll('.keynope-engagement-overlay button')].find(b=>b.textContent==='Close').click();",contentWorld:.page)
        try await waitForNativeJavaScript("native activity Close restores presentation",in:follower,expression:"remoteBuildCursor?.step===1&&keynopeEngagementOverlay===null&&document.querySelector('[data-object-id=\"first\"]')?.dataset.buildPending==='false'&&document.querySelector('[data-object-id=\"second\"]')?.dataset.buildPending==='true'")
        let running=try await view.callAsyncJavaScript("return keynopeRunningActivity?.phase===1&&keynopeRunningActivity.deadlineMs>Date.now()&&__activityConnections===1;",contentWorld:.page) as? Bool ?? false
        try requireNativeAcceptance(running,"native Close keeps collection/channel/timer alive and restores the same audience reveal",checks:&checks)
        _ = try await view.callAsyncJavaScript("__activityOptions.onEvent({id:'owned-one',identity:'owned-participant',displayName:'Fixture participant',createdAt:new Date(),payload:{type:'response',activityId:'owned-pulse',response:{choice:3}}});openEngagementRuntime();",contentWorld:.page)
        try await waitForNativeJavaScript("native reopen running collection",in:view,expression:"keynopeEngagementRuntime?.counts?.[3]===1&&keynopeEngagementRuntime.phase===1&&__activityConnections===1&&remoteBuildCursor?.step===1")
        try requireNativeAcceptance(true,"native responses collected while closed remain visible when reopening without reconnecting",checks:&checks)
        _ = try await view.callAsyncJavaScript("[...document.querySelectorAll('.keynope-engagement-overlay button')].find(b=>b.textContent==='Stop').click();",contentWorld:.page)
        try await waitForNativeJavaScript("native activity Stop reveal",in:follower,expression:"keynopeEngagementRuntime?.phase===3&&keynopeEngagementRuntime.counts?.[3]===1&&remoteBuildCursor?.step===1")
        _ = try await view.callAsyncJavaScript("await window.keynopeFlushActivityResults?.();await syncPresenterState();",contentWorld:.page)
        let preserved=try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return remoteBuildCursor.step===1&&s.slideBuild.steps.length===2;",contentWorld:.page) as? Bool ?? false
        try requireNativeAcceptance(preserved,"native completion/result archive keeps authored builds and current playback position",checks:&checks)
        let image=try await follower.takeSnapshot(configuration:nil)
        if let tiff=image.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]){try png.write(to:root.appendingPathComponent("native-activity-build-reveal.png"))}
        _ = try await view.callAsyncJavaScript("[...document.querySelectorAll('.keynope-engagement-overlay button')].find(b=>b.textContent==='Reopen').click();",contentWorld:.page)
        try await waitForNativeJavaScript("native completed activity Reopen",in:follower,expression:"keynopeEngagementRuntime?.phase===1&&remoteBuildCursor?.step===1")
        let resumed=try await view.callAsyncJavaScript("return keynopeEngagementRuntime.counts[3]===1&&keynopeEngagementRuntime.deadlineMs>Date.now()+59000&&__activityConnections===1;",contentWorld:.page) as? Bool ?? false
        try requireNativeAcceptance(resumed,"native Reopen preserves submissions/channel, adds timer allowance and retains the reveal",checks:&checks)
        _ = try await view.callAsyncJavaScript("[...document.querySelectorAll('.keynope-engagement-overlay button')].find(b=>b.textContent==='Reset').click();",contentWorld:.page)
        try await waitForNativeJavaScript("native activity Reset",in:view,expression:"keynopeEngagementRuntime?.phase===0&&keynopeEngagementRuntime.counts.every(n=>n===0)&&remoteBuildCursor?.step===1")
        try requireNativeAcceptance(true,"native Reset clears activity results but not presentation builds",checks:&checks)
        _ = try await view.callAsyncJavaScript("closeEngagementRuntime();await navigatePresenterBuild('next');",contentWorld:.page)
        try await waitForNativeJavaScript("native builds continue after activity",in:follower,expression:"keynopeEngagementOverlay===null&&remoteBuildCursor?.step===2&&document.querySelector('[data-object-id=\"second\"]')?.dataset.buildPending==='false'")
        try requireNativeAcceptance(true,"native reveal navigation continues normally after the activity lifecycle",checks:&checks)
    }

    private func verifyNativePresentationTools(root: URL, checks: inout [String]) async throws {
        guard let view = editorWebView else { throw NativeAcceptanceError(message: "Missing native presenter tools editor") }
        _ = try await view.callAsyncJavaScript("await fetch('/presenter-status',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode:'external',paused:false})});await syncPresenterState();", contentWorld: .page)
        try await waitForNativeJavaScript("native presenter tools ready", in: view, expression: "!!presentationToolsView&&document.querySelector('[aria-label=\"Presenter tools\"]')?.hidden===false")
        let follower = WKWebView(frame: NSRect(x: 0, y: 0, width: 1280, height: 800), configuration: WKWebViewConfiguration())
        follower.load(URLRequest(url: presenterURLForSurface("external")))
        defer { follower.stopLoading() }
        _ = try await view.callAsyncJavaScript("""
        window.__toolsDocumentVersion=(await fetch('/api/editor/state').then(r=>r.json())).version;
        window.__toolsRetainedObject=document.querySelector('#stage [data-object-id]');
        document.querySelector('[aria-label="Presenter tools"]').click();document.querySelector('[aria-label="Laser pointer"]').click();document.querySelector('[aria-label="Close tools"]').click();
        const svg=presentationToolsView.element,r=svg.getBoundingClientRect();svg.dispatchEvent(new PointerEvent('pointermove',{clientX:r.left+r.width*.4,clientY:r.top+r.height*.5,pointerId:41,pointerType:'mouse',bubbles:true}));
        """, contentWorld: .page)
        try await waitForNativeJavaScript("native external laser", in: follower, expression: "remotePresentationOverlay?.mode==='laser'&&Math.abs(remotePresentationOverlay.pointer?.[0]-.4)<.01&&document.querySelector('.keynope-presentation-overlay circle:last-child')?.getAttribute('cx')==='768'")
        try requireNativeAcceptance(true, "native toolbar laser follows normalized pointer position on a second WebKit display", checks: &checks)
        let retained = try await view.callAsyncJavaScript("return window.__toolsRetainedObject===document.querySelector('#stage [data-object-id]')&&(await fetch('/api/editor/state').then(r=>r.json())).version===window.__toolsDocumentVersion;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(retained, "native pointer updates retain slide nodes without authoring history changes", checks: &checks)
        _ = try await view.callAsyncJavaScript("""
        document.querySelector('[aria-label="Presenter tools"]').click();document.querySelector('[aria-label="Spotlight"]').click();document.querySelector('[aria-label="Close tools"]').click();const svg=presentationToolsView.element,r=svg.getBoundingClientRect();svg.dispatchEvent(new PointerEvent('pointermove',{clientX:r.left+r.width*.5,clientY:r.top+r.height*.5,pointerId:41,pointerType:'mouse',bubbles:true}));
        """, contentWorld: .page)
        try await waitForNativeJavaScript("native external spotlight", in: follower, expression: "remotePresentationOverlay?.mode==='spotlight'&&getComputedStyle(document.querySelector('.keynope-presentation-overlay > path')).display!=='none'")
        try requireNativeAcceptance(true, "native spotlight dims the slide with a transparent circular focus region", checks: &checks)
        _ = try await view.callAsyncJavaScript("""
        document.querySelector('[aria-label="Presenter tools"]').click();document.querySelector('[aria-label="Temporary ink"]').click();const width=document.querySelector('[aria-label="Ink width"]');width.value='12';width.dispatchEvent(new Event('change',{bubbles:true}));document.querySelector('[aria-label="Close tools"]').click();
        const svg=presentationToolsView.element,r=svg.getBoundingClientRect(),capture=svg.setPointerCapture;svg.setPointerCapture=()=>{};
        try{for(const [type,x,y,buttons]of [['pointerdown',.2,.7,1],['pointermove',.4,.6,1],['pointermove',.7,.7,1],['pointerup',.7,.7,0]])svg.dispatchEvent(new PointerEvent(type,{clientX:r.left+r.width*x,clientY:r.top+r.height*y,pointerId:42,pointerType:'mouse',button:0,buttons,bubbles:true}));}finally{svg.setPointerCapture=capture;}
        """, contentWorld: .page)
        try await waitForNativeJavaScript("native external ink", in: follower, expression: "remotePresentationOverlay?.strokes?.[0]?.points.length===3&&document.querySelector('.keynope-presentation-overlay g path')?.getAttribute('stroke-width')==='12'")
        try requireNativeAcceptance(true, "native temporary ink with chosen thickness reaches the external display", checks: &checks)
        _ = try await view.callAsyncJavaScript("window.dispatchEvent(new KeyboardEvent('keydown',{key:'z',metaKey:true,bubbles:true,cancelable:true}));", contentWorld: .page)
        try await waitForNativeJavaScript("native ink keyboard undo", in: follower, expression: "remotePresentationOverlay?.strokes?.length===0")
        _ = try await view.callAsyncJavaScript("window.dispatchEvent(new KeyboardEvent('keydown',{key:'y',metaKey:true,bubbles:true,cancelable:true}));", contentWorld: .page)
        try await waitForNativeJavaScript("native ink keyboard redo", in: follower, expression: "remotePresentationOverlay?.strokes?.[0]?.points.length===3")
        let keyboardClean = try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version===window.__toolsDocumentVersion;", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(keyboardClean, "native Cmd-Z/Cmd-Y operate only on temporary ink while the pen is active", checks: &checks)
        let image = try await follower.takeSnapshot(configuration: nil)
        if let tiff = image.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) { try png.write(to: root.appendingPathComponent("native-temporary-ink.png")) }
        _ = try await view.callAsyncJavaScript("document.querySelector('[aria-label=\"Presenter tools\"]').click();document.querySelector('[aria-label=\"Undo ink\"]').click();", contentWorld: .page)
        try await waitForNativeJavaScript("native external ink undo", in: follower, expression: "remotePresentationOverlay?.strokes?.length===0&&document.querySelectorAll('.keynope-presentation-overlay g path').length===0")
        try requireNativeAcceptance(true, "native Undo ink changes transient annotations rather than document history", checks: &checks)
        let exported = try await view.callAsyncJavaScript("const r=await fetch('/api/editor/export-document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'tools-clean-export.html'})});if(!r.ok)throw Error(await r.text());const p=await r.json();return p.content;", contentWorld: .page) as? String ?? ""
        let exportURL = root.appendingPathComponent("tools-clean-export.html")
        try exported.write(to: exportURL, atomically: true, encoding: .utf8)
        let exportView = WKWebView(frame: NSRect(x: 0, y: 0, width: 1280, height: 800), configuration: WKWebViewConfiguration())
        exportView.loadFileURL(exportURL, allowingReadAccessTo: root)
        defer { exportView.stopLoading() }
        try await waitForNativeJavaScript("native annotation-free export", in: exportView, expression: "typeof keynopePrepareSnapshot==='function'")
        let clean = try await exportView.callAsyncJavaScript("await keynopePrepareSnapshot(0);return !JSON.parse(document.querySelector('#keynope-data').textContent).overlay&&(!presentationToolsView||getComputedStyle(presentationToolsView.element).display==='none');", contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(clean, "native export/snapshot carries no temporary artwork or active tools", checks: &checks)
        _ = try await view.callAsyncJavaScript("await fetch('/presenter-status',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode:'none',paused:false})});await syncPresenterState();", contentWorld: .page)
        try await waitForNativeJavaScript("native tools stop", in: follower, expression: "remotePresentationOverlay===null&&getComputedStyle(presentationToolsView.element).display==='none'")
        try requireNativeAcceptance(true, "native Stop clears shared tool state and restores navigation", checks: &checks)
    }

    private func verifyNativePPTXTiming(root: URL, checks: inout [String]) async throws {
        guard let view = editorWebView else { throw NativeAcceptanceError(message: "Missing native timing editor") }
        try await waitForNativeJavaScript("native mixed timing source", in: view, expression: "!!document.querySelector('#stage .keynope-scene-table')&&!!document.querySelector('#stage .keynope-scene-chart')")
        let source = try await view.callAsyncJavaScript("""
        const s=await fetch('/api/editor/state').then(r=>r.json());
        const kinds=new Map(s.slides[s.current].elements.map(e=>[e.id,e.kind]));
        window.__nativeTimingSignature=JSON.stringify({transition:s.slideBuild.transition,steps:s.slideBuild.steps.map(step=>({...step,targetIds:step.targetIds.map(id=>kinds.get(id))}))});
        return s.slideBuild.steps.length===4&&JSON.stringify(s.slideBuild.steps.map(step=>step.targetIds.map(id=>kinds.get(id))))===JSON.stringify([['text'],['shape','table'],['chart'],['image']]);
        """, contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(source, "native mixed text/shape/table/chart/image targets reach the authored timeline", checks: &checks)
        let pptx = root.appendingPathComponent("native-timing.pptx")
        expectNativePanel(title: "Export Keynope Presentation as PowerPoint", destination: pptx, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Export Presentation as PowerPoint…")
        try await waitForNativeAcceptance("native timing PowerPoint export", timeout: 60) { FileManager.default.fileExists(atPath: pptx.path) }
        try requireNativeAcceptance(!documentDirty, "native PowerPoint export leaves the owned document clean", checks: &checks)
        expectNativePanel(title: "Open a Keynope or PowerPoint Presentation", destination: pptx, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Open…")
        try await waitForNativeJavaScript("native timing PowerPoint Open", in: view, expression: "(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slideBuild?.steps?.length===4&&s.slides[s.current].elements.some(e=>e.kind==='table'&&e.objectId?.startsWith('pptx-table-'));})()")
        let remapped = try await view.callAsyncJavaScript("""
        const s=await fetch('/api/editor/state').then(r=>r.json()),kinds=new Map(s.slides[s.current].elements.map(e=>[e.id,e.kind]));
        const signature={transition:s.slideBuild.transition,steps:s.slideBuild.steps.map(step=>({...step,targetIds:step.targetIds.map(id=>kinds.get(id))}))};
        return signature.transition.effect==='fade'&&signature.transition.durationMs===1200&&JSON.stringify(signature.steps.map(s=>[s.targetIds,s.effect,s.durationMs,s.advance,s.delayMs||0]))===JSON.stringify([[['text'],'fade',500,'click',0],[['shape','table'],'appear',100,'click',0],[['chart'],'wipe',400,'after',200],[['image'],'zoom',700,'after',150]]);
        """, contentWorld: .page) as? Bool ?? false
        try requireNativeAcceptance(remapped, "native Save panel export/Open remaps all entrance targets and preserves transition, effects, durations and triggers", checks: &checks)
        _ = try await view.callAsyncJavaScript("if(document.querySelector('[aria-label=\"Edit slide builds\"]').offsetParent===null)[...document.querySelectorAll('button')].find(b=>b.textContent==='Slide').click();document.querySelector('[aria-label=\"Edit slide builds\"]').click();", contentWorld: .page)
        try await waitForNativeJavaScript("native imported initial build preview", in: view, expression: "document.querySelectorAll('.keynope-build-preview [data-build-pending=\"true\"]').length===5")
        _ = try await view.callAsyncJavaScript("[...document.querySelector('dialog[aria-label=\"Slide builds\"]').querySelectorAll('button')].find(b=>b.textContent==='Next build').click();", contentWorld: .page)
        try await waitForNativeJavaScript("native imported first reveal", in: view, expression: "document.querySelectorAll('.keynope-build-preview [data-build-pending=\"false\"]').length===1")
        _ = try await view.callAsyncJavaScript("[...document.querySelector('dialog[aria-label=\"Slide builds\"]').querySelectorAll('button')].find(b=>b.textContent==='Next build').click();", contentWorld: .page)
        try await waitForNativeJavaScript("native imported automatic grouped reveals", in: view, expression: "document.querySelectorAll('.keynope-build-preview [data-build-pending=\"true\"]').length===0&&document.querySelectorAll('.keynope-build-preview [data-build-pending=\"false\"]').length===5")
        try requireNativeAcceptance(true, "native imported preview reveals grouped targets followed by timed chart/image entrances", checks: &checks)
        let snapshot = try await view.takeSnapshot(configuration: nil)
        if let tiff = snapshot.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) { try png.write(to: root.appendingPathComponent("timing-native-preview.png")) }
        _ = try await view.callAsyncJavaScript("[...document.querySelector('dialog[aria-label=\"Slide builds\"]').querySelectorAll('button')].find(b=>b.textContent==='Cancel').click();", contentWorld: .page)
        let markdown = root.appendingPathComponent("native-timing.md")
        expectNativePanel(title: "Save Keynope Presentation", destination: markdown, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Save As…")
        try await waitForNativeAcceptance("native imported timing Markdown Save") { !self.savingDocument && FileManager.default.fileExists(atPath: markdown.path) && !self.documentDirty }
        expectNativePanel(title: "Open a Keynope or PowerPoint Presentation", destination: markdown, response: .OK)
        try performNativeMenuItem(menu: "File", item: "Open…")
        try await waitForNativeJavaScript("native timing Markdown reopen", in: view, expression: "(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());return s.slideBuild?.steps?.length===4&&s.slideBuild.transition.durationMs===1200;})()")
        try requireNativeAcceptance(true, "native imported timing remains portable through Markdown Save/Open", checks: &checks)
    }

    private func verifyNativeTransitions(root: URL, checks: inout [String]) async throws {
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing native transition editor")}
        try await waitForNativeJavaScript("native transition source",in:view,expression:"!!document.querySelector('#stage [data-object-id=\"outgoing-title\"]')")
        _ = try await view.callAsyncJavaScript("await fetch('/presenter-status',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode:'main',paused:false})});await syncPresenterState();",arguments:[:],in:nil,contentWorld:.page)
        let follower=WKWebView(frame:NSRect(x:0,y:0,width:1280,height:800),configuration:WKWebViewConfiguration())
        follower.load(URLRequest(url:presenterURLForSurface("main")));defer{follower.stopLoading()}
        try await waitForNativeJavaScript("native outgoing transition ready",in:follower,expression:"!!document.querySelector('[data-object-id=\"outgoing-title\"]')&&getComputedStyle(document.querySelector('.keynope-modern-presentation')).visibility==='visible'")
        _ = try await view.callAsyncJavaScript("await navigatePresenterBuild('next');",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native whole-slide push",in:follower,expression:"document.querySelector('.keynope-slide-viewport')?.dataset.transitionState==='push'")
        let composed=try await follower.callAsyncJavaScript("""
        const host=document.querySelector('.keynope-slide-viewport');for(const a of host.getAnimations({subtree:true})){a.pause();a.currentTime=750;}
        await new Promise(r=>setTimeout(r,60));
        const group=host.querySelector('.keynope-slide-group'),old=[...host.children].find(n=>n!==group),background=group.querySelector('.keynope-scene-backdrop'),a=group.getBoundingClientRect(),b=background.getBoundingClientRect();
        const colour=n=>Array.from(n.getContext('2d').getImageData(1,1,1,1).data).join(',');
        return host.children.length===2&&host.querySelectorAll('[data-object-id]').length===1&&colour(background)==='36,92,200,255'&&colour(old.querySelector('canvas'))==='192,64,48,255'&&Math.abs(a.left-b.left)<.5&&Math.abs(a.top-b.top)<.5&&Math.abs(a.width-b.width)<.5&&Math.abs(a.height-b.height)<.5;
        """,arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(composed==true,"native push composes outgoing canvas pixels and incoming background/text as one slide",checks:&checks)
        let snapshot=try await follower.takeSnapshot(configuration:nil)
        if let tiff=snapshot.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]){try png.write(to:root.appendingPathComponent("transition-native.png"))}
        _ = try await follower.callAsyncJavaScript("await navigatePresenterBuild('previous');",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native transition cancellation",in:follower,expression:"!!document.querySelector('[data-object-id=\"outgoing-title\"]')&&!document.querySelector('.keynope-slide-viewport')?.dataset.transitionState&&document.querySelector('.keynope-slide-viewport').children.length===1")
        try requireNativeAcceptance(true,"native backward navigation cancels transition and removes its frozen snapshot",checks:&checks)
        let html=try await view.callAsyncJavaScript("const response=await fetch('/api/editor/export-document',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'Transitions.html'})});if(!response.ok)throw Error(await response.text());return (await response.json()).content;",arguments:[:],in:nil,contentWorld:.page) as? String ?? ""
        let exportView=WKWebView(frame:NSRect(x:0,y:0,width:1280,height:800),configuration:WKWebViewConfiguration())
        exportView.loadHTMLString(html,baseURL:presenterURLForSurface("external"));defer{exportView.stopLoading()}
        try await waitForNativeJavaScript("native standalone transition document",in:exportView,expression:"!!document.querySelector('[data-object-id=\"outgoing-title\"]')&&getComputedStyle(document.querySelector('.keynope-modern-presentation')).visibility==='visible'")
        _ = try await exportView.callAsyncJavaScript("window.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native export transition",in:exportView,expression:"document.querySelector('.keynope-slide-viewport')?.dataset.transitionState==='push'")
        _ = try await exportView.callAsyncJavaScript("await keynopePrepareSnapshot(1);",arguments:[:],in:nil,contentWorld:.page)
        let final=try await exportView.callAsyncJavaScript("return document.querySelector('.keynope-slide-viewport').children.length===1&&!document.querySelector('.keynope-slide-viewport').dataset.transitionState&&document.querySelector('.keynope-slide-group').getAnimations().length===0;",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(final==true,"native static export settles full-slide animations to the authored final state",checks:&checks)
        _ = try await view.callAsyncJavaScript("await fetch('/presenter-status',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({mode:'none',paused:false})});await syncPresenterState();return true;",arguments:[:],in:nil,contentWorld:.page)
        let clean=try await view.callAsyncJavaScript("return !(await fetch('/api/editor/state').then(r=>r.json())).dirty;",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(clean==true,"native transition navigation and capture do not dirty the document",checks:&checks)
    }

    private func verifyNativeSystemFonts(root: URL, checks: inout [String]) async throws {
        guard let view = editorWebView else { throw NativeAcceptanceError(message: "Missing native font editor") }
        try await waitForNativeJavaScript("system font editor", in: view, expression: "typeof window.keynopeImportTypeface==='function' && typeof editorAction==='function' && !!document.querySelector('#stage .keynope-modern-scene')")
        let outcome = try await view.callAsyncJavaScript("""
        try {
        const catalogue=await webkit.messageHandlers.keynopeFonts.postMessage({action:'catalogue'});
        if(catalogue.length<10)throw Error('Installed font catalogue is empty');
        const selected=['ArialMT','Menlo-Regular','Helvetica'].map(name=>catalogue.find(font=>font.postScriptName===name)).find(Boolean);
        if(!selected)throw Error('No installed test face');
        const face=await webkit.messageHandlers.keynopeFonts.postMessage({action:'read',postScriptName:selected.postScriptName});
        if(!face.data||face.localOnly)throw Error('Test face unexpectedly refuses embedding');
        const state=await fetch('/api/editor/state').then(r=>r.json()),element=state.slides[state.current].elements.find(e=>e.kind==='heading'||e.kind==='text');
        await editorAction({action:'select-element',elementData:{id:element.id}});
        const version=(await fetch('/api/editor/state').then(r=>r.json())).version;
        const id=await keynopeImportTypeface(face,keynopeFontOwner());
        if((await fetch('/api/editor/state').then(r=>r.json())).version!==version)throw Error('Import invalidated text draft');
        await editorAction({action:'set-modern-text-style',objectIds:[element.id],modernFont:id,sceneRevision:version,sceneMaster:false});
        await modernPageView.ready;
        const after=await fetch('/api/editor/state').then(r=>r.json());
        if(!after.slides[after.current].elements.find(e=>e.id===element.id).query.includes(id))throw Error('Font was not applied');
        if(!after.typefaces[id]||after.typefaces[id].localOnly)throw Error('Font was not retained');
        if(!document.fonts.check('24px KeynopeFont_'+id.slice(5)))throw Error('Font did not load');
        if(KeynopeFonts.missing().length)throw Error('Native face used a substitute');
        return selected.name+'; '+catalogue.length+' faces; '+id;
        } catch(error) { return 'ERROR: '+(error.message||String(error)); }
        """, arguments: [:], in: nil, contentWorld: .page) as? String
        try requireNativeAcceptance(outcome != nil && outcome?.hasPrefix("ERROR:") != true, "native catalogue, CoreText SFNT extraction, permission checks, draft ownership, embedding and WK font loading: " + (outcome ?? ""), checks: &checks)
        _ = try await view.callAsyncJavaScript("const select=document.querySelector('[aria-label=\"Font family\"]');select.value='__more_fonts__';select.dispatchEvent(new Event('change',{bubbles:true}));return true;", arguments: [:], in: nil, contentWorld: .page)
        try await waitForNativeJavaScript("full installed font dropdown", in: view, expression: "document.querySelectorAll('.keynope-font-dropdown [role=option]').length>160 && !!document.querySelector('.keynope-font-dropdown [data-font=ArialMT]')")
        _ = try await view.callAsyncJavaScript("document.querySelector('.keynope-font-dropdown [data-font=ArialMT]').focus();return true;", arguments: [:], in: nil, contentWorld: .page)
        try await waitForNativeJavaScript("native live font preview", in: view, expression: "(()=>{const p=document.querySelector('.keynope-font-preview');return p?.style.fontFamily.startsWith('KeynopeFontPreview_')&&getComputedStyle(p).fontFamily===p.style.fontFamily&&document.fonts.check('28px '+p.style.fontFamily)})()")
        try requireNativeAcceptance(true, "all installed fonts share one expandable dropdown with an actual font-file preview and no Use Font confirmation", checks: &checks)
        let snapshot = try await view.takeSnapshot(configuration: nil)
        if let tiff = snapshot.tiffRepresentation, let bitmap = NSBitmapImageRep(data: tiff), let png = bitmap.representation(using: .png, properties: [:]) { try png.write(to: root.appendingPathComponent("system-font-native.png")) }
    }

    private func verifyNativeImageFirstPaint(root: URL, checks: inout [String]) async throws {
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing native image editor")}
        try requireNativeAcceptance(workspaceMenu?.title == "View" && workspaceMenu?.items.contains(where: { $0.identifier?.rawValue == "format" && $0.action == #selector(performWorkspaceCommand(_:)) }) == true, "native View menu exposes the shared Format inspector command", checks: &checks)
        try requireNativeAcceptance(!NSWindow.allowsAutomaticWindowTabbing && editorWindow?.tabbingMode == .disallowed && workspaceMenu?.items.contains(where: { $0.action == #selector(NSWindow.toggleTabBar(_:)) || $0.action == #selector(NSWindow.toggleTabOverview(_:)) }) == false, "unsupported document tab bar and tab overview are not offered", checks: &checks)
        try await waitForNativeJavaScript("cold image editor",in:view,expression:"typeof editorAction==='function' && !!document.querySelector('#stage .keynope-modern-scene')")
        let originalAppearance = savedInterfaceAppearance
        defer { applyInterfaceAppearance(originalAppearance) }
        try requireNativeAcceptance(Set(appearanceMenuItems.keys) == Set(["system", "light", "dark"]), "native Settings exposes System, Light and Dark appearance", checks: &checks)
        for mode in ["light", "dark", "system"] {
            guard let item = appearanceMenuItems[mode] else { throw NativeAcceptanceError(message: "Missing appearance menu item") }
            changeInterfaceAppearance(item)
            try await waitForNativeJavaScript("native appearance " + mode, in: view, expression: "document.documentElement.dataset.keynopeInterfaceAppearance === '" + mode + "'")
            let expected: NSAppearance.Name? = mode == "system" ? nil : mode == "dark" ? .darkAqua : .aqua
            try requireNativeAcceptance(NSApp.appearance?.name == expected && savedInterfaceAppearance == mode && appearanceMenuItems.allSatisfy { $0.value.state == ($0.key == mode ? .on : .off) }, "native " + mode + " appearance updates chrome, preference and menu state", checks: &checks)
            try requireNativeAcceptance(view.configuration.userContentController.userScripts.contains { $0.source == "window.KEYNOPE_INTERFACE_APPEARANCE = '" + mode + "';" }, "native " + mode + " appearance survives editor reload", checks: &checks)
        }
        for kind in ["png", "jpeg"] {
            let id=try await view.callAsyncJavaScript("""
            const s=await fetch('/api/editor/state').then(r=>r.json());
            const prior=new Set(s.slides[s.current].elements.map(e=>e.id));
            const canvas=document.createElement('canvas');canvas.width=1800;canvas.height=1200;
            const c=canvas.getContext('2d');c.fillStyle=kind==='png'?'#f52878':'#1982f5';c.fillRect(0,0,1800,1200);
            const blob=await new Promise(resolve=>canvas.toBlob(resolve,'image/'+kind,.92));
            const transfer=new DataTransfer();transfer.items.add(new File([blob],'cold.'+kind,{type:'image/'+kind}));
            const input=document.querySelector('input[type="file"][accept^="image/"]');
            input.files=transfer.files;input.dispatchEvent(new Event('change',{bubbles:true}));
            const deadline=Date.now()+15000;
            while(Date.now()<deadline){
              const state=await fetch('/api/editor/state').then(r=>r.json());
              const image=state.slides[state.current].elements.find(e=>e.kind==='image'&&!prior.has(e.id));
              if(image)return image.id;
              await new Promise(resolve=>setTimeout(resolve,30));
            }
            throw Error('Native upload did not finish');
            """,arguments:["kind":kind],in:nil,contentWorld:.page) as? String
            guard let id else {throw NativeAcceptanceError(message:"No inserted image")}
            try await waitForNativeJavaScript("cold \(kind) mounted",in:view,expression:"!!document.querySelector('#stage [data-object-id=\"\(id)\"] img')")
            _ = try await view.callAsyncJavaScript("""
            await Promise.race([modernPageView.ready,new Promise((_,reject)=>setTimeout(()=>reject(Error('Cold image readiness timed out')),10000))]);
            await Promise.race([new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))),new Promise(resolve=>setTimeout(resolve,500))]);
            return true;
            """,arguments:[:],in:nil,contentWorld:.page)
            let capture=try await view.takeSnapshot(configuration:nil)
            guard let tiff=capture.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]) else {throw NativeAcceptanceError(message:"Missing native image pixels")}
            try png.write(to:root.appendingPathComponent("cold-\(kind)-before-interaction.png"))
            var ink=0
            for y in stride(from:0,to:bitmap.pixelsHigh,by:2) {
                for x in stride(from:0,to:bitmap.pixelsWide,by:2) {
                    guard let color=bitmap.colorAt(x:x,y:y)?.usingColorSpace(.deviceRGB) else {continue}
                    if kind=="png" ? color.redComponent>0.7 && color.greenComponent<0.4 && color.blueComponent>0.3 : color.redComponent<0.3 && color.greenComponent>0.35 && color.blueComponent>0.8 {ink+=1}
                }
            }
            try requireNativeAcceptance(ink>2000,"native \(kind) paints on insertion without moving, cropping or reselecting (\(ink) sampled pixels)",checks:&checks)
        }
    }

    private func verifyNativeSVG(root: URL, checks: inout [String]) async throws {
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing native editor")}
        try await waitForNativeJavaScript("SVG editor readiness",in:view,expression:"typeof editorAction==='function' && !!document.querySelector('#stage .keynope-modern-scene')")
        let sourcePath=ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_SVG_FIXTURE"] ?? "/tmp/keynope-svg-proof/passive-logo.svg"
        let source=try String(contentsOfFile:sourcePath,encoding:.utf8)
        _ = try await view.callAsyncJavaScript("""
        const input=document.querySelector('input[type=file][accept*="image/svg+xml"]');if(!input)throw new Error('SVG chooser missing');
        const transfer=new DataTransfer();transfer.items.add(new File([source],'logo.svg',{type:'image/svg+xml'}));input.files=transfer.files;
        input.dispatchEvent(new Event('change',{bubbles:true}));
        """,arguments:["source":source],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native SVG import",in:view,expression:"(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());const e=s.slides[s.current].elements[s.selected];if(e?.kind==='image'&&e.assetId){window.__nativeSVGID=e.id;return true;}return false;})()")
        try requireNativeAcceptance(true,"native upload imports SVG as an editable image",checks:&checks)
        do {
            try await waitForNativeJavaScript("native vector image decode",in:view,expression:"(()=>{const img=document.querySelector('#stage [data-object-id=\"'+window.__nativeSVGID+'\"] img');const media=deck.pages[pageIndex]?.scene?.objects.find(o=>o.id===window.__nativeSVGID)?.media;return img?.src.startsWith('data:image/svg+xml;base64,')&&img.complete&&img.naturalWidth>0&&media?.width===320&&media.height===180;})()")
        } catch {
            let detail=try? await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());const scene=await fetch('/api/editor/scene?slide='+s.current).then(r=>r.json());return JSON.stringify({id:window.__nativeSVGID,objects:scene.objects.map(o=>({id:o.id,source:o.media?.source?.slice(0,30),retro:!!o.retroLines})),images:[...document.querySelectorAll('#stage img')].map(i=>({id:i.closest('[data-object-id]')?.dataset.objectId,src:i.src.slice(0,30),width:i.naturalWidth,height:i.naturalHeight,complete:i.complete})),rendered:deck.pages[pageIndex]?.scene?.objects?.map(o=>({id:o.id,source:o.media?.source?.slice(0,30),retro:!!o.retroLines}))});",arguments:[:],in:nil,contentWorld:.page)
            throw NativeAcceptanceError(message:"SVG decode diagnostics: \(detail as? String ?? "unavailable")")
        }
        try requireNativeAcceptance(true,"native WebKit renders the self-contained vector source",checks:&checks)
        let rasterSample=try await view.callAsyncJavaScript("const img=document.querySelector('#stage [data-object-id=\"'+window.__nativeSVGID+'\"] img'),canvas=document.createElement('canvas');canvas.width=320;canvas.height=180;const c=canvas.getContext('2d');c.drawImage(img,0,0,320,180);return Array.from(c.getImageData(248,90,1,1).data);",arguments:[:],in:nil,contentWorld:.page) as? [Int]
        try requireNativeAcceptance(rasterSample?.count==4 && rasterSample![0]>200 && rasterSample![1]>150 && rasterSample![2]<100 && rasterSample![3]>240,"native SVG pixels match the owned source geometry",checks:&checks)
        let result=try await view.callAsyncJavaScript("""
        const before=await fetch('/api/editor/state').then(r=>r.json());
        const scene=await fetch('/api/editor/scene?slide='+before.current).then(r=>r.json());
        const object=scene.objects.find(o=>o.id===window.__nativeSVGID);
        const original=before.slides[before.current].elements[before.selected];
        const query=new URLSearchParams(original.query);query.set('image-style','retro');query.set('glyph','blocks');
        const retro={...original,query:query.toString()};
        await editorAction({action:'update-element',element:before.selected,elementData:retro});
        const after=await fetch('/api/editor/scene?slide='+before.current).then(r=>r.json());
        window.__nativeSVGModernSource=object.media.source;
        return !!after.objects.find(o=>o.id===original.id)?.retroLines?.lines.length;
        """,arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(result==true,"native Retro projection uses the portable PNG fallback",checks:&checks)
        _ = try await view.callAsyncJavaScript("await editorAction({action:'undo'});",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("SVG style undo",in:view,expression:"(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());const scene=await fetch('/api/editor/scene?slide='+s.current).then(r=>r.json());return scene.objects.find(o=>o.id===window.__nativeSVGID)?.media.source===window.__nativeSVGModernSource;})()")
        try requireNativeAcceptance(true,"native Undo restores the same vector source",checks:&checks)
        let rejected=try await view.callAsyncJavaScript("""
        const before=await fetch('/api/editor/state').then(r=>r.json());const body=new FormData();
        body.append('image',new Blob(['<svg width="10" height="10"><script/></svg>'],{type:'image/svg+xml'}),'unsafe.svg');
        const response=await fetch('/api/editor/upload',{method:'POST',body});const after=await fetch('/api/editor/state').then(r=>r.json());
        return response.status===400&&before.version===after.version&&JSON.stringify(before.slides)===JSON.stringify(after.slides);
        """,arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(rejected==true,"native unsafe SVG rejection preserves the document and history",checks:&checks)
        _ = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());const index=s.slides[s.current].elements.findIndex(e=>e.id===window.__nativeSVGID);const e={...s.slides[s.current].elements[index]};e.query='image-style=modern&left=25&top=24&width=130&height=25&stretch=1';await editorAction({action:'update-element',element:index,elementData:e});await Promise.race([modernPageView.ready,new Promise((_,reject)=>setTimeout(()=>reject(Error('SVG snapshot readiness timeout')),10000))]);",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native SVG geometry refresh",in:view,expression:"(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());const scene=await fetch('/api/editor/scene?slide='+s.current).then(r=>r.json());const o=scene.objects.find(o=>o.id===window.__nativeSVGID),node=document.querySelector('#stage [data-object-id=\"'+window.__nativeSVGID+'\"]');return node&&Math.abs(parseFloat(node.style.width)-o.bounds.width)<.01&&Math.abs(parseFloat(node.style.left)-o.bounds.x)<.01;})()")
        try requireNativeAcceptance(true,"native SVG geometry changes reach the mounted image",checks:&checks)
        _ = try await view.callAsyncJavaScript("await Promise.race([new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))),new Promise(resolve=>setTimeout(resolve,500))]);",arguments:[:],in:nil,contentWorld:.page)
        let snapshot=try await view.takeSnapshot(configuration:nil)
        if let tiff=snapshot.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]) {
            try png.write(to:root.appendingPathComponent("svg-editor.png"))
        }
    }

    private func verifyNativeTableTokens(root: URL, checks: inout [String]) async throws {
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing native editor")}
        try await waitForNativeJavaScript("native table token fixture",in:view,expression:"deck.pages[pageIndex]?.scene?.objects.some(o=>o.id==='portable-table')&&typeof editorAction==='function'")
        _ = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),i=s.slides[s.current].elements.findIndex(e=>e.id==='portable-table');window.__nativeTableTheme=s.theme;await editorAction({action:'select-element',element:i,elementData:s.slides[s.current].elements[i]});window.__nativeTableVersion=(await fetch('/api/editor/state').then(r=>r.json())).version;",arguments:[:],in:nil,contentWorld:.page)
        _ = try await view.evaluateJavaScript("document.querySelector('.keynope-canvas-element.active').dispatchEvent(new MouseEvent('dblclick',{bubbles:true,cancelable:true}));")
        try await waitForNativeJavaScript("native table role controls",in:view,expression:"!!document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Canvas cell fill theme\"]')")
        _ = try await view.evaluateJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]'),caret=d.querySelector('[contenteditable=true]');if(caret)caret.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));d.querySelector('[aria-label=\"Cell B2\"]').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,cancelable:true}));")
        let initial=try await view.callAsyncJavaScript("const d=document.querySelector('dialog[aria-label=\"Edit table cells\"]');return d.querySelector('[aria-label=\"Canvas cell fill theme\"]').value==='theme:accent-2'&&d.querySelector('[aria-label=\"Canvas cell text theme\"]').value==='theme:heading';",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(initial==true,"native cell controls expose authored fill/text roles",checks:&checks)
        _ = try await view.evaluateJavaScript("document.querySelector('dialog[aria-label=\"Edit table cells\"] [aria-label=\"Cell C2\"]').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,shiftKey:true,cancelable:true}));")
        let mixed=try await view.callAsyncJavaScript("return document.querySelector('[aria-label=\"Canvas cell fill theme\"]').value==='mixed'&&document.querySelector('[aria-label=\"Canvas cell text theme\"]').value==='mixed';",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(mixed==true,"native range selection preserves mixed semantic role state",checks:&checks)
        for (label,value) in [("fill","theme:accent-4"),("text","theme:accent-5"),("border","theme:line")] {
            _ = try await view.callAsyncJavaScript("const field=document.querySelector('[aria-label=\"Canvas cell '+label+' theme\"]');field.value=value;field.dispatchEvent(new Event('change',{bubbles:true}));",arguments:["label":label,"value":value],in:nil,contentWorld:.page)
            try await waitForNativeJavaScript("native \(label) role preview",in:view,expression:"!document.querySelector('dialog[aria-label=\"Edit table cells\"]')?.hasAttribute('aria-busy')&&document.querySelector('[aria-label=\"Canvas cell \(label) theme\"]')?.value==='\(value)'")
        }
        let uncommitted=try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version===window.__nativeTableVersion;",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(uncommitted==true,"native table role previews do not commit document history",checks:&checks)
        _ = try await view.evaluateJavaScript("[...document.querySelectorAll('dialog[aria-label=\"Edit table cells\"] button')].find(b=>b.textContent.trim()==='Done').click();")
        try await waitForNativeJavaScript("native table roles committed",in:view,expression:"!document.querySelector('dialog[aria-label=\"Edit table cells\"]')&&(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),cell=s.selectedTable?.data.cells[1][1],other=s.selectedTable?.data.cells[1][2];return s.version===window.__nativeTableVersion+1&&cell?.style.fill==='theme:accent-4'&&cell.style.color==='theme:accent-5'&&other?.style.fill==='theme:accent-4'&&other.style.color==='theme:accent-5'&&cell.runs[1].color==='#ff0055';})()")
        try requireNativeAcceptance(true,"native cell role edits commit once and preserve explicit rich text colour",checks:&checks)
        _ = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),theme=JSON.parse(JSON.stringify(s.theme));theme.colors.accent[3]='#003366';theme.colors.accent[4]='#ffdd66';await editorAction({action:'set-deck-theme',themeData:theme});",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native linked cell palette paint",in:view,expression:"(()=>{const cell=deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='portable-table')?.table.cells.find(c=>c.row===1&&c.column===1),node=document.querySelector('#stage [data-table-cell=\"1:1\"]');return cell?.fill==='#003366'&&cell.paint.color==='#ffdd66'&&node&&getComputedStyle(node).backgroundColor==='rgb(0, 51, 102)';})()")
        try requireNativeAcceptance(true,"native palette edits resolve role-linked cell paint immediately",checks:&checks)
        let image=try await view.takeSnapshot(configuration:nil)
        if let tiff=image.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]) {try png.write(to:root.appendingPathComponent("native-table-tokens.png"))}
    }

    private func verifyNativeThemeColours(root: URL, checks: inout [String]) async throws {
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing native editor")}
        try await waitForNativeJavaScript("native master-bound colour fixture",in:view,expression:"deck.pages[pageIndex]?.scene?.objects.some(o=>o.id==='instance')&&typeof editorAction==='function'")
        _ = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),i=s.slides[s.current].elements.findIndex(e=>e.id==='instance');await editorAction({action:'select-element',element:i,elementData:s.slides[s.current].elements[i]});",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native colour Style tab",in:view,expression:"[...document.querySelectorAll('[role=tab]')].some(b=>b.textContent.trim()==='Style')")
        _ = try await view.callAsyncJavaScript("[...document.querySelectorAll('[role=tab]')].find(b=>b.textContent.trim()==='Style').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native theme colour selector",in:view,expression:"document.querySelector('[data-selection-property=\"paint.colorToken\"]')?.value==='custom'")
        _ = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());window.__nativeColourExpected=s.theme.colors.accent[0];window.__nativeColourOwner=document.querySelector('#stage [data-object-id=\"instance\"]');const field=document.querySelector('[data-selection-property=\"paint.colorToken\"]');field.value='accent-1';field.dispatchEvent(new Event('change',{bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native assigned theme role beats master colours",in:view,expression:"document.querySelector('[data-selection-property=\"paint.colorToken\"]')?.value==='accent-1'&&deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color===window.__nativeColourExpected")
        let scoped=try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),e=s.slides[s.current].elements.find(e=>e.id==='instance'),q=new URLSearchParams(e.query);return q.get('theme-fg')==='accent-1'&&!q.has('fg')&&!q.has('header')&&q.get('master-clear')==='fg,header,shadow';",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(scoped==true,"native role selection overrides master aliases while preserving unrelated clears",checks:&checks)
        _ = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),theme=JSON.parse(JSON.stringify(s.theme));theme.colors.accent[0]='#0055aa';await editorAction({action:'set-deck-theme',themeData:theme});",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native role follows palette edit",in:view,expression:"deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color==='#0055aa'&&document.querySelector('[data-selection-property=\"paint.colorToken\"] option[value=\"accent-1\"]')?.textContent.includes('#0055aa')")
        let retained=try await view.callAsyncJavaScript("return window.__nativeColourOwner===document.querySelector('#stage [data-object-id=\"instance\"]')",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(retained==true,"native palette edit updates role colour while retaining text paint owner",checks:&checks)
        _ = try await view.callAsyncJavaScript("const field=document.querySelector('[data-selection-property=\"paint.colorToken\"]');field.value='';field.dispatchEvent(new Event('change',{bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native Inherit restores master colour",in:view,expression:"deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color==='#aa0055'&&document.querySelector('[data-selection-property=\"paint.colorToken\"]')?.value==='custom'")
        try requireNativeAcceptance(true,"native Inherit restores master heading colour rather than freezing the palette",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('button[aria-label=\"Undo\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native theme role Undo",in:view,expression:"deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color==='#0055aa'&&document.querySelector('[data-selection-property=\"paint.colorToken\"]')?.value==='accent-1'")
        try requireNativeAcceptance(true,"native Undo restores the semantic role in one command",checks:&checks)
        _ = try await view.callAsyncJavaScript("[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='Reset style').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native Reset restores cleared master styles",in:view,expression:"(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json()),q=new URLSearchParams(s.slides[s.current].elements.find(e=>e.id==='instance').query);return !q.has('master-clear')&&!q.has('theme-fg')&&s.selectionCapabilities?.properties['paint.shadow']?.value==='solid'&&deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color==='#aa0055';})()")
        try requireNativeAcceptance(true,"native Reset style restores suppressed master paint without deleting content or geometry",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('button[aria-label=\"Undo\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        do {
            try await waitForNativeJavaScript("native Reset style Undo",in:view,expression:"deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint.color==='#0055aa'&&document.querySelector('[data-selection-property=\"paint.colorToken\"]')?.value==='accent-1'&&getComputedStyle(document.querySelector('[data-selection-property=\"paint.color\"]'),'::before').backgroundColor==='rgb(0, 85, 170)'")
        } catch {
            let diagnostic=try? await view.callAsyncJavaScript("const n=document.querySelector('[data-selection-property=\"paint.color\"]'),s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify({scene:deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='instance')?.paint,property:s.selectionCapabilities?.properties['paint.color'],role:document.querySelector('[data-selection-property=\"paint.colorToken\"]')?.value,swatch:{text:n?.textContent,variable:n?.style.getPropertyValue('--paint-color'),border:n&&getComputedStyle(n).borderLeftColor,transition:n&&getComputedStyle(n).transition,focus:n?.matches(':focus-visible'),hover:n?.matches(':hover')}});",arguments:[:],in:nil,contentWorld:.page)
            throw NativeAcceptanceError(message:"Native theme swatch mismatch: \(diagnostic as? String ?? "unavailable")")
        }
        try requireNativeAcceptance(true,"native reset Undo restores the role and selective master suppression",checks:&checks)
        let image=try await view.takeSnapshot(configuration:nil)
        if let tiff=image.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]) {try png.write(to:root.appendingPathComponent("native-theme-colours.png"))}
    }

    private func verifyNativeStrokeRoutes(root: URL, checks: inout [String]) async throws {
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing native editor")}
        try await waitForNativeJavaScript("native stroke route fixture",in:view,expression:"!!document.querySelector('#stage polyline[data-object-id=\"line\"]')&&deck.pages[pageIndex]?.scene?.objects.some(o=>o.id==='source')")
        let boundary=try await view.callAsyncJavaScript("const page=deck.pages[pageIndex],scene=page.scene,source=scene.objects.find(o=>o.id==='source'),line=scene.objects.find(o=>o.id==='line'),port=page.shapePorts.find(p=>p.id==='source'&&p.side==='right');window.__nativeRouteBefore=JSON.stringify(line.points);window.__nativeRouteOwner=document.querySelector('#stage polyline[data-object-id=\"line\"]');return Math.abs(line.points[0].x-source.bounds.x-source.bounds.width-24)<.00001&&Math.abs(port.x*1920/deck.cols-line.points[0].x)<.00001&&line.points.length>3;",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(boundary==true,"native route starts on the outer 48 px stroke and detours around the outlined obstacle",checks:&checks)
        _ = try await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),i=s.slides[s.current].elements.findIndex(e=>e.id==='source');await editorAction({action:'select-element',element:i,elementData:s.slides[s.current].elements[i]});",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native route Style tab",in:view,expression:"[...document.querySelectorAll('[role=tab]')].some(b=>b.textContent.trim()==='Style')")
        _ = try await view.callAsyncJavaScript("[...document.querySelectorAll('[role=tab]')].find(b=>b.textContent.trim()==='Style').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native route stroke field",in:view,expression:"!!document.querySelector('[data-selection-property=\"paint.strokeWidth\"]')")
        _ = try await view.callAsyncJavaScript("const field=document.querySelector('[data-selection-property=\"paint.strokeWidth\"]');field.value='0';field.dispatchEvent(new Event('change',{bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native zero-width stroke and updated endpoint",in:view,expression:"(()=>{const s=deck.pages[pageIndex]?.scene,source=s?.objects.find(o=>o.id==='source'),line=s?.objects.find(o=>o.id==='line');return source?.paint.strokeWidth===0&&Math.abs(line.points[0].x-source.bounds.x-source.bounds.width)<.00001&&document.querySelector('#stage [data-object-id=\"source\"]>svg>path')?.getAttribute('stroke-width')==='0'&&document.querySelector('[data-selection-property=\"paint.strokeWidth\"]')?.value==='0';})()")
        try requireNativeAcceptance(true,"native width zero disables the visible stroke and moves the endpoint without stale field values",checks:&checks)
        let retained=try await view.callAsyncJavaScript("return window.__nativeRouteOwner===document.querySelector('#stage polyline[data-object-id=\"line\"]')",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(retained==true,"native stroke edit retains the mounted connector",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('button[aria-label=\"Undo\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native route undo",in:view,expression:"JSON.stringify(deck.pages[pageIndex]?.scene?.objects.find(o=>o.id==='line')?.points)===window.__nativeRouteBefore&&document.querySelector('[data-selection-property=\"paint.strokeWidth\"]')?.value==='48'")
        try requireNativeAcceptance(true,"native Undo restores the original outlined route in one command",checks:&checks)
        let image=try await view.takeSnapshot(configuration:nil)
        if let tiff=image.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]) {
            try png.write(to:root.appendingPathComponent("native-stroke-routes.png"))
        }
    }

    private func verifyNativeShapes(checks: inout [String]) async throws {
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing native editor")}
        try await waitForNativeJavaScript("shape editor readiness",in:view,expression:"typeof editorAction==='function' && typeof keynopeShapeCatalog!=='undefined' && !!document.querySelector('#stage .keynope-modern-scene')")
        let count=try await view.callAsyncJavaScript("return keynopeShapeCatalog.length",arguments:[:],in:nil,contentWorld:.page) as? Int
        try requireNativeAcceptance(count==29,"native editor packages the 29-shape catalogue",checks:&checks)
        _ = try await view.callAsyncJavaScript("Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='Insert').click();document.querySelector('[aria-label=\"Add shape\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        let choices=try await view.callAsyncJavaScript("return document.querySelector('[role=\"menu\"][aria-label=\"Add shape\"]').querySelectorAll('[role=\"menuitem\"]').length",arguments:[:],in:nil,contentWorld:.page) as? Int
        try requireNativeAcceptance(choices==29,"native Insert gallery has every curated shape",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('[aria-label=\"Add right arrow\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("arrow adjustment controls",in:view,expression:"!!document.querySelector('input[aria-label=\"Head depth\"]')")
        _ = try await view.callAsyncJavaScript("const input=document.querySelector('input[aria-label=\"Head depth\"]');input.value='55';input.dispatchEvent(new Event('change',{bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        do {
            try await waitForNativeJavaScript("native arrow adjustment",in:view,expression:"(async()=>{const s=await fetch('/api/editor/state').then(r=>r.json());const e=s.slides[s.current].elements.find(e=>new URLSearchParams(e.query).get('shape')==='arrow-right');window.__nativeArrowID=e?.id;return e&&new URLSearchParams(e.query).get('shape-depth')==='0.55';})()")
        } catch {
            let diagnostic=try? await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify({current:s.current,selected:s.selected,queries:s.slides[s.current].elements.map(e=>[e.id,e.kind,e.query]),inputs:Array.from(document.querySelectorAll('input[aria-label=\"Head depth\"]')).map(i=>({value:i.value,valid:i.checkValidity(),min:i.min,max:i.max})),status:document.querySelector('.keynope-editor-status')?.textContent});",arguments:[:],in:nil,contentWorld:.page)
            throw NativeAcceptanceError(message:"\(error.localizedDescription); geometry diagnostics: \(diagnostic as? String ?? "unavailable")")
        }
        try requireNativeAcceptance(true,"native Head depth updates the document immediately",checks:&checks)
        try await waitForNativeJavaScript("native adjusted vector path",in:view,expression:"(async()=>{const s=await fetch('/api/editor/scene?slide=0').then(r=>r.json());const o=s.objects.find(o=>o.id===window.__nativeArrowID);const node=document.querySelector('#stage [data-object-id=\"'+window.__nativeArrowID+'\"]>svg>path');return o?.shapeGeometry?.spec.adjustments.depth===.55&&node?.getAttribute('d')===o.shapeGeometry.path;})()")
        try requireNativeAcceptance(true,"native visible vector uses the adjusted production geometry",checks:&checks)
        let displayed=try await view.callAsyncJavaScript("return document.querySelector('input[aria-label=\"Head depth\"]').value",arguments:[:],in:nil,contentWorld:.page) as? String
        try requireNativeAcceptance(displayed=="55","native adjustment field retains its current value",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('button[aria-label=\"Undo\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native geometry undo",in:view,expression:"(async()=>{const s=await fetch('/api/editor/scene?slide=0').then(r=>r.json());return s.objects.find(o=>o.id===window.__nativeArrowID)?.shapeGeometry.spec.adjustments.depth===.35;})()")
        try requireNativeAcceptance(true,"native Undo restores the geometry default in one command",checks:&checks)
        _ = try await view.callAsyncJavaScript("const select=document.querySelector('select[aria-label=\"Shape type\"]');select.value='speech-bubble';select.dispatchEvent(new Event('change',{bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native speech bubble controls",in:view,expression:"!!document.querySelector('input[aria-label=\"Tail depth\"]') && !!document.querySelector('input[aria-label=\"Corner radius\"]')")
        try requireNativeAcceptance(true,"native type conversion surfaces the matching adjustment controls",checks:&checks)
        do {
            try await waitForNativeJavaScript("native shape adjustment handles",in:view,expression:"(()=>{renderEditorCanvasOverlay();return !!document.querySelector('.keynope-shape-adjustment[aria-label=\"Adjust tail depth\"]') && !!document.querySelector('.keynope-shape-adjustment[aria-label=\"Adjust corner radius\"]');})()")
        } catch {
            let diagnostic=try? await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json());return JSON.stringify({current:s.current,selected:s.selected,selection:s.selection,object:deck.pages[pageIndex]?.scene?.objects.find(o=>o.id===window.__nativeArrowID),handles:[...document.querySelectorAll('.keynope-shape-adjustment')].map(n=>n.getAttribute('aria-label'))});",arguments:[:],in:nil,contentWorld:.page)
            throw NativeAcceptanceError(message:"Native handle check failed; owned-fixture diagnostic: \(diagnostic as? String ?? "unavailable")")
        }
        try requireNativeAcceptance(true,"native selected shapes expose geometry handles distinct from bounds corners",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('.keynope-shape-adjustment[aria-label=\"Adjust tail depth\"]').dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native keyboard shape adjustment",in:view,expression:"(async()=>{const s=await fetch('/api/editor/scene?slide=0').then(r=>r.json());return s.objects.find(o=>o.id===window.__nativeArrowID)?.shapeGeometry.spec.adjustments.depth===.23 && document.querySelector('input[aria-label=\"Tail depth\"]').value==='23';})()")
        try requireNativeAcceptance(true,"native keyboard handle updates its geometry and accepted numerical field",checks:&checks)
        let draftImmutable=try await view.callAsyncJavaScript("const before=await fetch('/api/editor/state').then(r=>r.json());const response=await fetch('/api/editor/shape-adjustment-draft',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({sceneRevision:before.version,sceneMaster:false,slide:0,objectId:window.__nativeArrowID,shapeAdjustment:{key:'depth',value:.31}})});const draft=await response.json();const after=await fetch('/api/editor/state').then(r=>r.json());return before.version===after.version && draft.objects.find(o=>o.id===window.__nativeArrowID)?.shapeGeometry.spec.adjustments.depth===.31;",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(draftImmutable==true,"native geometry draft endpoint previews without committing an edit",checks:&checks)
        _ = try await view.callAsyncJavaScript("Array.from(document.querySelectorAll('[role=tab]')).find(b=>b.textContent.trim()==='Style').click();const s=await fetch('/api/editor/state').then(r=>r.json());await editorAction({action:'set-selection-property',slide:s.current,sceneRevision:s.version,objectIds:[window.__nativeArrowID],property:'paint.strokeColor',propertyValue:'#aa55ff'});",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native stroke inspector",in:view,expression:"!!document.querySelector('[data-selection-property=\"paint.strokeWidth\"]')&&!document.querySelector('[data-selection-property=\"paint.strokeWidth\"]').disabled")
        _ = try await view.callAsyncJavaScript("const input=document.querySelector('[data-selection-property=\"paint.strokeWidth\"]');input.value='8.25';input.dispatchEvent(new Event('change',{bubbles:true}));",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native fractional stroke",in:view,expression:"document.querySelector('[data-selection-property=\"paint.strokeWidth\"]')?.value==='8.25'&&document.querySelector('#stage [data-object-id=\"'+window.__nativeArrowID+'\"]>svg>path')?.getAttribute('stroke-width')==='8.25'")
        let nativeStroke=try await view.callAsyncJavaScript("return document.querySelector('#stage [data-object-id=\"'+window.__nativeArrowID+'\"]>svg>path')?.getAttribute('stroke')",arguments:[:],in:nil,contentWorld:.page) as? String
        try requireNativeAcceptance(nativeStroke=="#aa55ff","native stroke colour and fractional width paint the vector and accepted fields",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('[aria-label=\"Turn shape stroke off\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native stroke Off",in:view,expression:"document.querySelector('[data-selection-property=\"paint.strokeColor\"]')?.textContent==='Off'&&!document.querySelector('#stage [data-object-id=\"'+window.__nativeArrowID+'\"]>svg>path')?.hasAttribute('stroke')")
        try requireNativeAcceptance(true,"native stroke Off suppresses the visible outline",checks:&checks)
        _ = try await view.callAsyncJavaScript("document.querySelector('button[aria-label=\"Undo\"]').click();",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native stroke undo",in:view,expression:"document.querySelector('[data-selection-property=\"paint.strokeColor\"]')?.textContent==='#aa55ff'&&document.querySelector('[data-selection-property=\"paint.strokeWidth\"]')?.value==='8.25'")
        try requireNativeAcceptance(true,"native stroke undo restores colour without losing width",checks:&checks)
        _ = try await view.callAsyncJavaScript("const state=await fetch('/api/editor/state').then(r=>r.json());window.__nativeSelectionOriginalIndex=state.slides[state.current].elements.findIndex(e=>e.id===window.__nativeArrowID);window.__nativeSelectionMismatches=[];window.__nativeSelectionObservedTurns=0;window.__nativeSelectionObserver=new MutationObserver(()=>{const scene=deck.pages[pageIndex]?.scene,o=scene?.objects.find(o=>o.id===window.__nativeArrowID),h=document.querySelector('.keynope-canvas-element.active[data-element-id=\"'+window.__nativeArrowID+'\"]');if(!o)return;window.__nativeSelectionObservedTurns++;const expected=[o.bounds.x/scene.width*100,o.bounds.y/scene.height*100,o.bounds.width/scene.width*100,o.bounds.height/scene.height*100],actual=h?[h.style.left,h.style.top,h.style.width,h.style.height].map(parseFloat):[];if((actual.length!==4||actual.some((v,i)=>Math.abs(v-expected[i])>.00001))&&window.__nativeSelectionMismatches.length<16)window.__nativeSelectionMismatches.push({id:o.id,expected,actual});});window.__nativeSelectionObserver.observe(document.querySelector('#stage'),{subtree:true,childList:true,attributes:true,attributeFilter:['style','class','d','width','height','data-element-id']});",arguments:[:],in:nil,contentWorld:.page)
        _ = try await view.callAsyncJavaScript("const insert=Array.from(document.querySelectorAll('button')).find(b=>b.textContent.trim()==='Insert');if(insert?.getAttribute('aria-expanded')==='true')insert.click();const s=await fetch('/api/editor/state').then(r=>r.json());const i=s.slides[s.current].elements.findIndex(e=>e.id===window.__nativeArrowID),e=s.slides[s.current].elements[i],q=new URLSearchParams(e.query);for(const [key,value]of Object.entries({left:'40',top:'15',width:'90',height:'18',fg:'#234359'}))q.set(key,value);const label=btoa(JSON.stringify({text:'Stroke',query:'render=truetype&modern-font=c64&modern-size=40&fg=%23ffffff'}));q.set('shape-label',label);const updated={...e,query:q.toString()};await editorAction({action:'update-element',element:i,elementData:updated});await editorAction({action:'select-element',element:i,elementData:updated});",arguments:[:],in:nil,contentWorld:.page)
        try await waitForNativeJavaScript("native visible stroke frame",in:view,expression:"(()=>{const n=document.querySelector('#stage [data-object-id=\"'+window.__nativeArrowID+'\"]>svg>path'),b=n?.getBoundingClientRect();return b?.width>200&&b?.height>80&&n.getAttribute('stroke')==='#aa55ff'&&n.getAttribute('stroke-width')==='8.25'&&document.querySelector('#keynope-insert-popover')?.hidden;})()")
        do {
            try await waitForNativeJavaScript("native selection frame follows stable shape ID",in:view,expression:"(async()=>{const state=await fetch('/api/editor/state').then(r=>r.json()),scene=deck.pages[pageIndex]?.scene,o=scene?.objects.find(o=>o.id===window.__nativeArrowID),h=document.querySelector('.keynope-canvas-element.active');if(!o||!h||state.slides[state.current].elements[Number(h.dataset.element)]?.id!==o.id)return false;return Math.abs(parseFloat(h.style.left)-o.bounds.x/scene.width*100)<.00001&&Math.abs(parseFloat(h.style.top)-o.bounds.y/scene.height*100)<.00001&&Math.abs(parseFloat(h.style.width)-o.bounds.width/scene.width*100)<.00001&&Math.abs(parseFloat(h.style.height)-o.bounds.height/scene.height*100)<.00001;})()")
        } catch {
            let diagnostic=try? await view.callAsyncJavaScript("const s=await fetch('/api/editor/state').then(r=>r.json()),scene=deck.pages[pageIndex]?.scene;return JSON.stringify({api:{version:s.version,selected:s.selected,selection:s.selection,ids:s.slides[s.current].elements.map(e=>e.id)},uiVersion:window.keynopePresentationDocumentVersion,objects:scene?.objects.map(o=>({id:o.id,bounds:o.bounds})),hits:[...document.querySelectorAll('.keynope-canvas-element')].map(h=>({index:h.dataset.element,active:h.classList.contains('active'),left:h.style.left,top:h.style.top,width:h.style.width,height:h.style.height}))});",arguments:[:],in:nil,contentWorld:.page)
            throw NativeAcceptanceError(message:"Native selection geometry mismatch; owned-fixture diagnostic: \(diagnostic as? String ?? "unavailable")")
        }
        try requireNativeAcceptance(true,"native selection outline follows the moved shape's stable ID and bounds",checks:&checks)
        let selectionFrames=try await view.callAsyncJavaScript("window.__nativeSelectionObserver.disconnect();const state=await fetch('/api/editor/state').then(r=>r.json());return {observed:window.__nativeSelectionObservedTurns,mismatches:JSON.stringify(window.__nativeSelectionMismatches),beforeIndex:window.__nativeSelectionOriginalIndex,afterIndex:state.slides[state.current].elements.findIndex(e=>e.id===window.__nativeArrowID)};",arguments:[:],in:nil,contentWorld:.page) as? [String:Any]
        let observed=selectionFrames?["observed"] as? Int ?? 0
        try requireNativeAcceptance(observed>0 && selectionFrames?["mismatches"] as? String == "[]","native shape paint and selection outline stay coherent through \(observed) observed turns",checks:&checks)
        try requireNativeAcceptance(selectionFrames?["beforeIndex"] as? Int != selectionFrames?["afterIndex"] as? Int,"native moved shape keeps its selection after authored indices reorder",checks:&checks)
        let backdropBounded=try await view.callAsyncJavaScript("window.__nativeBackdropOriginal=deck.pages[pageIndex];deck.pages[pageIndex]={...window.__nativeBackdropOriginal,bg:'#e9dec9',scene:{...window.__nativeBackdropOriginal.scene,background:'#e9dec9'}};render();const stage=document.querySelector('#stage'),canvas=document.querySelector('.keynope-scene-backdrop'),pixel=[...canvas.getContext('2d').getImageData(0,0,1,1).data];return stage.style.background===''&&getComputedStyle(stage).backgroundColor==='rgb(29, 32, 36)'&&pixel.join(',')==='233,222,201,255';",arguments:[:],in:nil,contentWorld:.page) as? Bool
        try requireNativeAcceptance(backdropBounded==true,"native slide background is confined to the canvas; pasteboard retains application colour",checks:&checks)
        if let root=ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_ACCEPTANCE_ROOT"] {
            let image=try await view.takeSnapshot(configuration:nil)
            if let tiff=image.tiffRepresentation,let bitmap=NSBitmapImageRep(data:tiff),let png=bitmap.representation(using:.png,properties:[:]) {
                try png.write(to:URL(fileURLWithPath:root).appendingPathComponent("native-shape-stroke.png"))
            }
        }
        _ = try await view.callAsyncJavaScript("deck.pages[pageIndex]=window.__nativeBackdropOriginal;delete window.__nativeBackdropOriginal;render();",arguments:[:],in:nil,contentWorld:.page)
    }

    private func verifyNativePDFExport(root: URL, checks: inout [String]) async throws {
        try FileManager.default.createDirectory(at:root,withIntermediateDirectories:true)
        guard let view=editorWebView else {throw NativeAcceptanceError(message:"Missing native editor")}
        try await waitForNativeJavaScript("PDF editor readiness",in:view,expression:"typeof editorAction==='function' && typeof window.keynopePrepareDocumentSave==='function' && !!document.querySelector('#stage .keynope-modern-scene')")
        let before = try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version",arguments:[:],in:nil,contentWorld:.page) as? Int
        let originalURL=activeDeckURLHandler?()
        let originalBytes=try originalURL.map {try Data(contentsOf:$0)}
        let sandboxed=ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_PDF_SANDBOX"]=="1"
        let external=ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_PDF_EXTERNAL_DEST"].flatMap{ $0.isEmpty ? nil : URL(fileURLWithPath:$0) }
        let outputRoot=sandboxed ? (external?.deletingLastPathComponent() ?? root) : root
        let cancelled=root.appendingPathComponent("pdf-panel-cancel.pdf")
        nativeSlideExportFailure=nil
        expectNativePanel(title:"Export Presentation as PDF",destination:cancelled,response:.cancel)
        try performNativeMenuItem(menu:"File",item:"Export Presentation as PDF…")
        try await waitForNativeAcceptance("PDF Save-panel cancellation",timeout:90){!self.exportingSlides && self.nativePanelAcceptanceStep==nil}
        try requireNativePanelConsumed("PDF Save-panel cancellation",checks:&checks)
        try requireNativeAcceptance(!FileManager.default.fileExists(atPath:cancelled.path) && nativeSlideExportFailure==nil,"native PDF Save Cancel creates no output",checks:&checks)

        for (index,name) in ["quality","exact"].enumerated() {
            let url=outputRoot.appendingPathComponent("native-\(name).pdf")
            nativeSlideExportFailure=nil
            nativePanelAcceptancePDFBytes=nil
            expectNativePanel(title:"Export Presentation as PDF",destination:url,response:.OK,accessoryIndex:index,manualConfirmation:sandboxed)
            try performNativeMenuItem(menu:"File",item:"Export Presentation as PDF…")
            try await waitForNativeAcceptance("native \(name) PDF",timeout:120){!self.exportingSlides && self.nativePanelAcceptanceStep==nil}
            try requireNativePanelConsumed("native \(name) PDF Save panel",checks:&checks)
            if let failure=nativeSlideExportFailure {throw NativeAcceptanceError(message:failure)}
            let pdf: PDFDocument?
            if sandboxed {
                guard let bytes=nativePanelAcceptancePDFBytes else {throw NativeAcceptanceError(message:"Missing committed PDF bytes for \(name)")}
                try bytes.write(to:root.appendingPathComponent("expected-native-\(name).pdf"))
                pdf=PDFDocument(data:bytes)
            } else { pdf=PDFDocument(url:url) }
            guard let pdf,pdf.pageCount==2 else {throw NativeAcceptanceError(message:"Wrong PDF page count for \(name)")}
            try requireNativeAcceptance((0..<pdf.pageCount).allSatisfy {let b=pdf.page(at:$0)!.bounds(for:.mediaBox);return abs(b.width/b.height-16.0/9.0)<0.005},"native \(name) PDF preserves landscape size and excludes tabs",checks:&checks)
            let text=(0..<pdf.pageCount).compactMap{pdf.page(at:$0)?.string}.joined()
            let normalized=text.replacingOccurrences(of:"\\s+",with:"",options:.regularExpression)
            try requireNativeAcceptance(index==0 ? normalized.contains("NativePresentationAcceptance") : normalized.isEmpty,"native \(name) Save choice selects the requested PDF renderer",checks:&checks)
        }

        if let url=external {
            nativeSlideExportFailure=nil
            nativePanelAcceptancePDFBytes=nil
            expectNativePanel(title:"Export Presentation as PDF",destination:url,response:.OK,manualConfirmation:true)
            try performNativeMenuItem(menu:"File",item:"Export Presentation as PDF…")
            try await waitForNativeAcceptance("sandboxed external PDF",timeout:120){!self.exportingSlides && self.nativePanelAcceptanceStep==nil}
            try requireNativePanelConsumed("native Save confirmation grants the external destination",checks:&checks)
            if let failure=nativeSlideExportFailure {throw NativeAcceptanceError(message:failure)}
            if sandboxed {
                guard let bytes=nativePanelAcceptancePDFBytes,let selected=nativePanelAcceptanceSelectedURL else {throw NativeAcceptanceError(message:"Missing committed external PDF snapshot")}
                try bytes.write(to:root.appendingPathComponent("expected-user-selected.pdf"))
                let writer=try SlideExportWriter(destination:selected)
                do {try await writer.stage(bytes){_,_ in throw CancellationError()};throw NativeAcceptanceError(message:"Cancelled staging unexpectedly succeeded")}
                catch is CancellationError {}
                writer.discard()
                try requireNativeAcceptance(true,"sandboxed staging cancellation completes before external parent readback",checks:&checks)
            } else { try requireNativeAcceptance(PDFDocument(url:url)?.pageCount==2,"external export creates a two-page PDF",checks:&checks) }
        }

        let sentinel=Data("DO NOT REPLACE CANCELLED EXPORT".utf8)
        for phase in (sandboxed ? [] : ["render","write","commit"]) {
            let url=root.appendingPathComponent("cancel-\(phase).pdf")
            try sentinel.write(to:url)
            nativeSlideExportCancelPhase=phase
            nativeSlideExportFailure=nil
            expectNativePanel(title:"Export Presentation as PDF",destination:url,response:.OK)
            try performNativeMenuItem(menu:"File",item:"Export Presentation as PDF…")
            try await waitForNativeAcceptance("PDF \(phase) cancellation",timeout:120){!self.exportingSlides && self.nativePanelAcceptanceStep==nil}
            nativeSlideExportCancelPhase=nil
            try requireNativePanelConsumed("PDF \(phase) cancellation Save panel",checks:&checks)
            try requireNativeAcceptance(try Data(contentsOf:url)==sentinel && nativeSlideExportFailure==nil,"native Cancel during \(phase) leaves the existing destination untouched",checks:&checks)
        }
        let after=try await view.callAsyncJavaScript("return (await fetch('/api/editor/state').then(r=>r.json())).version",arguments:[:],in:nil,contentWorld:.page) as? Int
        try requireNativeAcceptance(before==after && activeDeckURLHandler?()==originalURL && !documentDirty,"PDF export/cancel does not mutate editor revision, path or saved baseline",checks:&checks)
        if let originalURL,let originalBytes {try requireNativeAcceptance(try Data(contentsOf:originalURL)==originalBytes,"PDF export/cancel does not write the Markdown deck",checks:&checks)}
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
            if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_STYLE_MENU_ACCEPTANCE"] == "1" {
                try await verifyNativeStyleMenus(root: root, checks: &checks)
                finishNativePanelAcceptance(root: root, checks: checks, failure: nil)
                return
            }
            if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_GESTURE_ACCEPTANCE"] == "1" {
                try await verifyNativeAppKitGestures(root:root,checks:&checks)
                finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                return
            }
            if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_TABLE_ACCEPTANCE"] == "1" {
                try await verifyNativeTables(root: root, checks: &checks)
                finishNativePanelAcceptance(root: root, checks: checks, failure: nil)
                return
            }
            if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_CHART_ACCEPTANCE"] == "1" {
                try await verifyNativeCharts(root: root, checks: &checks)
                finishNativePanelAcceptance(root: root, checks: checks, failure: nil)
                return
            }
            if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_PDF_ACCEPTANCE"] == "1" {
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_SYSTEM_FONTS_ACCEPTANCE"] == "1" {
                    try await verifyNativeSystemFonts(root: root, checks: &checks)
                    finishNativePanelAcceptance(root: root, checks: checks, failure: nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_IMAGE_FIRST_PAINT_ACCEPTANCE"] == "1" {
                    try await verifyNativeImageFirstPaint(root:root,checks:&checks)
                    finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_ACTIVITY_BUILD_ACCEPTANCE"] == "1" {
                    try await verifyNativeActivityBuilds(root:root,checks:&checks)
                    finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_PRESENTATION_TOOLS_ACCEPTANCE"] == "1" {
                    try await verifyNativePresentationTools(root: root, checks: &checks)
                    finishNativePanelAcceptance(root: root, checks: checks, failure: nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_PPTX_TIMING_ACCEPTANCE"] == "1" {
                    try await verifyNativePPTXTiming(root: root, checks: &checks)
                    finishNativePanelAcceptance(root: root, checks: checks, failure: nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_TRANSITION_ACCEPTANCE"] == "1" {
                    try await verifyNativeTransitions(root:root,checks:&checks)
                    finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_BUILD_ACCEPTANCE"] == "1" {
                    try await verifyNativeBuilds(root:root,checks:&checks)
                    finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_SVG_ACCEPTANCE"] == "1" {
                    try await verifyNativeSVG(root:root,checks:&checks)
                    finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_SHAPE_ACCEPTANCE"] == "1" {
                    try await verifyNativeShapes(checks:&checks)
                    finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_STROKE_ROUTE_ACCEPTANCE"] == "1" {
                    try await verifyNativeStrokeRoutes(root:root,checks:&checks)
                    finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_THEME_COLOUR_ACCEPTANCE"] == "1" {
                    try await verifyNativeThemeColours(root:root,checks:&checks)
                    finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                    return
                }
                if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_TABLE_TOKEN_ACCEPTANCE"] == "1" {
                    try await verifyNativeTableTokens(root:root,checks:&checks)
                    finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                    return
                }
                try await verifyNativePDFExport(root:root,checks:&checks)
                finishNativePanelAcceptance(root:root,checks:checks,failure:nil)
                return
            }
            if ProcessInfo.processInfo.environment["KEYNOPE_NATIVE_STYLE_ACCEPTANCE"] == "1" {
                try await verifyNativeSelectionProperties(checks: &checks)
                finishNativePanelAcceptance(root: root, checks: checks, failure: nil)
                return
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

            expectNativePanel(title: "Open a Keynope or PowerPoint Presentation", destination: recovered, response: .OK)
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

            expectNativePanel(title: "Open a Keynope or PowerPoint Presentation", destination: original, response: .cancel)
            try performNativeMenuItem(menu: "File", item: "Open…")
            try requireNativePanelConsumed("cancelled Open through NSOpenPanel", checks: &checks)
            try requireNativeAcceptance(activeDeckURLHandler?()?.standardizedFileURL == recovered.standardizedFileURL && documentDirty == false, "cancelled Open retained document URL and dirty state", checks: &checks)

            try requireNativeAcceptance(activeDeckURLHandler?()?.standardizedFileURL == recovered.standardizedFileURL, "exports did not replace the active deck", checks: &checks)
            try performNativeMenuItem(menu: "File", item: "New Presentation")
            guard let templateView = editorWebView else { throw NativeAcceptanceError(message: "Missing template chooser view") }
            try await waitForNativeJavaScript("template gallery", in: templateView, expression: "!!document.querySelector('.keynope-template-card')")
            _ = try await templateView.evaluateJavaScript("document.querySelector('.keynope-template-dialog > button').click()")
            try await waitForNativeJavaScript("cancel template gallery", in: templateView, expression: "!document.querySelector('.keynope-template-dialog')")
            try requireNativeAcceptance(activeDeckURLHandler?()?.standardizedFileURL == recovered.standardizedFileURL && !documentDirty, "cancelled template chooser retained the saved document", checks: &checks)
            try performNativeMenuItem(menu: "File", item: "New Presentation")
            try await waitForNativeJavaScript("template selection", in: templateView, expression: "!!document.querySelector('.keynope-template-card')")
            _ = try await templateView.evaluateJavaScript("document.querySelector('[aria-label=\"Use Clean light template\"]').click()")
            try await waitForNativeAcceptance("new presentation", timeout: 60) {
                self.activeDeckURLHandler?() == nil && self.editorWebView?.isLoading == false && self.documentDirty == true
            }
            try requireNativeAcceptance(editorWindow?.title.contains("Untitled *") == true, "File → New created a dirty untitled deck and updated the native title", checks: &checks)
            try await verifyNativeObjectClipboard(checks: &checks)
            try await verifyNativeSelectionProperties(checks: &checks)
            finishNativePanelAcceptance(root: root, checks: checks, failure: nil)
        } catch {
            func location(_ url: URL?) -> String {
                guard let url, var parts = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return "nil" }
                // Acceptance diagnostics do not need the local editor token.
                parts.query = nil
                parts.fragment = nil
                return parts.string ?? "nil"
            }
            let transport = " WebView URL: \(location(editorWebView?.url)), loading: \(editorWebView?.isLoading ?? false). Native endpoint: \(location(editorEndpoint("/api/editor/state")))."
            finishNativePanelAcceptance(root: root, checks: checks, failure: error.localizedDescription + transport)
        }
    }
#endif

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return appMode
    }

    func applicationWillTerminate(_ notification: Notification) {
        automationService.stop()
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

    func windowDidBecomeKey(_ notification: Notification) {
        guard let changed = notification.object as? NSWindow, changed === editorWindow else { return }
        refreshEditorStyleCommands()
    }

    func windowDidResignKey(_ notification: Notification) {
        guard let changed = notification.object as? NSWindow, changed === editorWindow else { return }
        updateEditorStyleCommands()
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
        #if KEYNOPE_NATIVE_ACCEPTANCE
        if args.count == 2, args[1] == "--mcp-settings-test" { AutomationSettingsAcceptance.run(); return }
        #endif
        if args.count == 2, args[1] == "--mcp-render" { AutomationRenderer.run(); return }
        if args.count > 1, args[1] == "--mcp" { AutomationServer.run(arguments: Array(args.dropFirst())); return }
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
            importDeckHandler: appMode ? { path in startEngine(deckPath: path, untitled: true, importPPTX: true) } : nil,
            selectedDeckURLHandler: appMode ? { url in
                try rememberUserSelectedDeckAccess(url)
            } : nil,
            newDeckHandler: appMode ? { templateID in
                guard let path = bundledWelcomeDeckPath() else { return nil }
                return startEngine(deckPath: path, untitled: true, templateID: templateID)
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

    private static func startEngine(deckPath: String, untitled: Bool = false, importPPTX: Bool = false, restoring: Bool = false, templateID: String? = nil) -> URL? {
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
            + (importPPTX ? ["--import-pptx"] : [])
            + (templateID.map { ["--template=" + $0] } ?? [])
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
#if KEYNOPE_NATIVE_ACCEPTANCE
        // Keep engine diagnostics in the isolated acceptance directory. An
        // HTTP connection failure must not hide a panic behind an unread pipe.
        var acceptanceEngineLog: URL?
        if let root = environment["KEYNOPE_NATIVE_ACCEPTANCE_ROOT"] {
            let log = URL(fileURLWithPath: root).appendingPathComponent("engine.log")
            if !FileManager.default.fileExists(atPath: log.path) {
                FileManager.default.createFile(atPath: log.path, contents: nil)
            }
            if let handle = try? FileHandle(forWritingTo: log) {
                _ = try? handle.seekToEnd()
                process.standardError = handle
                acceptanceEngineLog = log
            }
        }
#endif
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
                    activeDeckPath = untitled || importPPTX ? nil : authorization.path
                    if !untitled && !importPPTX { recordRecentDeck(authorization.path) }
                    if let previousProcess, previousProcess.isRunning {
                        try? previousInput?.fileHandleForWriting.close()
                        previousProcess.terminate()
                    }
                    return url
                }
            }
        }
        if process.isRunning { process.terminate() }
        let errorData: Data
#if KEYNOPE_NATIVE_ACCEPTANCE
        if let log = acceptanceEngineLog {
            errorData = (try? Data(contentsOf: log)) ?? Data()
        } else {
            errorData = errors.fileHandleForReading.readDataToEndOfFile()
        }
#else
        errorData = errors.fileHandleForReading.readDataToEndOfFile()
#endif
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
