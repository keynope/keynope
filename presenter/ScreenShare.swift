import AVFoundation
import Cocoa
import CoreMedia
import CoreVideo
@preconcurrency import ScreenCaptureKit
import WebKit

final class ShareableContentResult: @unchecked Sendable {
    let content: SCShareableContent?
    let error: Error?

    init(content: SCShareableContent?, error: Error?) {
        self.content = content
        self.error = error
    }
}

private final class SampleBufferBox: @unchecked Sendable {
    let value: CMSampleBuffer

    init(_ value: CMSampleBuffer) {
        self.value = value
    }
}

private final class StoppedStreamResult: @unchecked Sendable {
    let stream: SCStream
    let error: Error

    init(stream: SCStream, error: Error) {
        self.stream = stream
        self.error = error
    }
}

enum ShareTargetKind: String {
    case application
    case window
    case display
}

final class ShareTarget: NSObject {
    let kind: ShareTargetKind
    let identifier: Int64
    let title: String

    init(kind: ShareTargetKind, identifier: Int64, title: String) {
        self.kind = kind
        self.identifier = identifier
        self.title = title
    }

    var key: String {
        "\(kind.rawValue):\(identifier)"
    }
}

enum ScreenShareError: LocalizedError {
    case sourceUnavailable
    case displayUnavailable

    var errorDescription: String? {
        switch self {
        case .sourceUnavailable:
            return "The selected application or window is no longer available."
        case .displayUnavailable:
            return "No display is available for this capture."
        }
    }
}

@MainActor
final class CapturePreviewView: NSView {
    private let displayLayer = AVSampleBufferDisplayLayer()

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        wantsLayer = true
        let backingLayer = CALayer()
        backingLayer.backgroundColor = NSColor.black.cgColor
        backingLayer.cornerRadius = 8
        backingLayer.masksToBounds = true
        backingLayer.borderColor = NSColor(calibratedWhite: 1, alpha: 0.28).cgColor
        backingLayer.borderWidth = 1
        backingLayer.addSublayer(displayLayer)
        layer = backingLayer
        displayLayer.videoGravity = .resizeAspect
        displayLayer.backgroundColor = NSColor.black.cgColor
    }

    required init?(coder: NSCoder) {
        nil
    }

    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        displayLayer.frame = bounds
        CATransaction.commit()
    }

    func enqueue(_ sampleBuffer: CMSampleBuffer) {
        guard sampleBuffer.isValid, CMSampleBufferGetImageBuffer(sampleBuffer) != nil else {
            return
        }
        let renderer = displayLayer.sampleBufferRenderer
        if renderer.status == .failed {
            renderer.flush(removingDisplayedImage: true, completionHandler: nil)
        }
        renderer.enqueue(sampleBuffer)
    }

    func clear() {
        displayLayer.sampleBufferRenderer.flush(removingDisplayedImage: true, completionHandler: nil)
    }
}

@MainActor
final class PresenterCompositeView: NSView {
    let webView: WKWebView
    let captureView = CapturePreviewView(frame: .zero)

    init(frame: NSRect, configuration: WKWebViewConfiguration) {
        webView = WKWebView(frame: frame, configuration: configuration)
        super.init(frame: frame)
        autoresizingMask = [.width, .height]
        webView.autoresizingMask = [.width, .height]
        captureView.autoresizingMask = []
        captureView.isHidden = true
        addSubview(webView)
        addSubview(captureView)
    }

    required init?(coder: NSCoder) {
        nil
    }

    override func layout() {
        super.layout()
        webView.frame = bounds
        let horizontalMargin = max(24, bounds.width * 0.05)
        let verticalMargin = max(24, bounds.height * 0.06)
        captureView.frame = bounds.insetBy(dx: horizontalMargin, dy: verticalMargin)
    }
}

@MainActor
final class ScreenShareController: NSObject, SCStreamDelegate, SCStreamOutput, @unchecked Sendable {
    weak var previewView: CapturePreviewView? {
        didSet {
            previewView?.isHidden = activeTarget == nil
        }
    }
    var onStopped: ((Error) -> Void)?

    private let outputQueue = DispatchQueue(label: "io.keynope.presenter.screen-share", qos: .userInteractive)
    private var stream: SCStream?
    private(set) var activeTarget: ShareTarget?
    private var operationGeneration = 0

    func start(
        target: ShareTarget,
        content: SCShareableContent,
        preferredDisplayID: CGDirectDisplayID?
    ) async throws {
        operationGeneration &+= 1
        let generation = operationGeneration
        await stopCurrentStream()
        let filter: SCContentFilter
        var fallbackSize = CGSize(width: 1920, height: 1080)

        switch target.kind {
        case .window:
            guard let window = content.windows.first(where: { Int64($0.windowID) == target.identifier }) else {
                throw ScreenShareError.sourceUnavailable
            }
            filter = SCContentFilter(desktopIndependentWindow: window)
            fallbackSize = window.frame.size
        case .application:
            guard let application = content.applications.first(where: { Int64($0.processID) == target.identifier }) else {
                throw ScreenShareError.sourceUnavailable
            }
            guard let display = preferredDisplay(
                in: content,
                displayID: preferredDisplayID,
                applicationPID: application.processID
            ) else {
                throw ScreenShareError.displayUnavailable
            }
            filter = SCContentFilter(display: display, including: [application], exceptingWindows: [])
            fallbackSize = CGSize(width: display.width, height: display.height)
        case .display:
            guard let display = content.displays.first(where: { Int64($0.displayID) == target.identifier }) else {
                throw ScreenShareError.displayUnavailable
            }
            let ownPID = ProcessInfo.processInfo.processIdentifier
            let ownBundleIdentifier = Bundle.main.bundleIdentifier
            let excludedApplications = content.applications.filter {
                $0.processID == ownPID || (ownBundleIdentifier != nil && $0.bundleIdentifier == ownBundleIdentifier)
            }
            filter = SCContentFilter(display: display, excludingApplications: excludedApplications, exceptingWindows: [])
            fallbackSize = CGSize(width: display.width, height: display.height)
        }

        let configuration = streamConfiguration(filter: filter, fallbackSize: fallbackSize)
        let newStream = SCStream(filter: filter, configuration: configuration, delegate: self)
        try newStream.addStreamOutput(self, type: .screen, sampleHandlerQueue: outputQueue)
        stream = newStream
        activeTarget = target
        await MainActor.run {
            self.previewView?.clear()
            self.previewView?.isHidden = false
        }
        do {
            try await newStream.startCapture()
        } catch {
            let wasSuperseded = generation != operationGeneration || stream !== newStream
            if stream === newStream {
                stream = nil
                activeTarget = nil
                previewView?.isHidden = true
            }
            if wasSuperseded {
                return
            }
            throw error
        }
    }

    func stop() async {
        operationGeneration &+= 1
        await stopCurrentStream()
    }

    private func stopCurrentStream() async {
        let oldStream = stream
        stream = nil
        activeTarget = nil
        if let oldStream {
            try? await oldStream.stopCapture()
        }
        await MainActor.run {
            self.previewView?.clear()
            self.previewView?.isHidden = true
        }
    }

    private func preferredDisplay(
        in content: SCShareableContent,
        displayID: CGDirectDisplayID?,
        applicationPID: pid_t
    ) -> SCDisplay? {
        let applicationWindows = content.windows.filter { $0.owningApplication?.processID == applicationPID }
        if !applicationWindows.isEmpty {
            let rankedDisplays = content.displays.map { display in
                let area = applicationWindows.reduce(CGFloat.zero) { total, window in
                    let intersection = display.frame.intersection(window.frame)
                    guard !intersection.isNull else {
                        return total
                    }
                    return total + intersection.width * intersection.height
                }
                return (display, area)
            }
            if let best = rankedDisplays.max(by: { $0.1 < $1.1 }), best.1 > 0 {
                return best.0
            }
        }
        if let displayID, let display = content.displays.first(where: { $0.displayID == displayID }) {
            return display
        }
        return content.displays.first
    }

    private func streamConfiguration(filter: SCContentFilter, fallbackSize: CGSize) -> SCStreamConfiguration {
        var sourceSize = fallbackSize
        if #available(macOS 14.0, *) {
            let info = SCShareableContent.info(for: filter)
            sourceSize = CGSize(
                width: info.contentRect.width * CGFloat(info.pointPixelScale),
                height: info.contentRect.height * CGFloat(info.pointPixelScale)
            )
        }
        sourceSize.width = max(1, sourceSize.width)
        sourceSize.height = max(1, sourceSize.height)
        let scale = min(1, 2560 / max(sourceSize.width, sourceSize.height))
        let configuration = SCStreamConfiguration()
        configuration.width = max(1, Int(sourceSize.width * scale))
        configuration.height = max(1, Int(sourceSize.height * scale))
        configuration.minimumFrameInterval = CMTime(value: 1, timescale: 30)
        configuration.queueDepth = 4
        configuration.pixelFormat = kCVPixelFormatType_32BGRA
        configuration.showsCursor = true
        configuration.scalesToFit = true
        if #available(macOS 13.0, *) {
            configuration.capturesAudio = false
        }
        return configuration
    }

    nonisolated func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of type: SCStreamOutputType) {
        guard type == .screen else {
            return
        }
        let sample = SampleBufferBox(sampleBuffer)
        Task { @MainActor [weak self] in
            self?.previewView?.enqueue(sample.value)
        }
    }

    nonisolated func stream(_ stream: SCStream, didStopWithError error: Error) {
        let result = StoppedStreamResult(stream: stream, error: error)
        Task { @MainActor [weak self] in
            guard let self, result.stream === self.stream else {
                return
            }
            self.stream = nil
            self.activeTarget = nil
            self.previewView?.clear()
            self.previewView?.isHidden = true
            self.onStopped?(result.error)
        }
    }
}
