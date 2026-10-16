import Cocoa

// Owned, optimized-build regressions for PDF picture sampling. No application,
// user deck, Save dialog, external resource or foreground activation is used.
@main struct PDFMediaSamplingCheck {
    @MainActor static func main() {
        let app = NSApplication.shared
        app.setActivationPolicy(.accessory)
        Task { @MainActor in
            do {
                try await pixels()
                try await missingPicture()
                for phase in ["plan", "black", "white", "install"] {
                    try await cancellation(phase)
                }
                try await badBounds()
                print("PASS: matte alpha/colour recovery, invalid inputs, work limits, cancellation and capture ownership")
                app.terminate(nil)
            } catch { print("FAIL: \(error)"); exit(1) }
        }
        app.run()
    }

    private static func failure(_ message: String) -> NSError {
        NSError(domain:"sh.keynope.pdf-media-test",code:1,userInfo:[NSLocalizedDescriptionKey:message])
    }

    private static let alphas = [0,1,66,128,153,254,255]
    private static let colours = [[255,0,0],[0,255,0],[0,0,255],[255,255,255],[0,0,0]]

    private static func matte(_ white: Bool, height: Int = 64, opaque: Bool = true) throws -> Data {
        guard let bitmap = NSBitmapImageRep(bitmapDataPlanes:nil,pixelsWide:140,pixelsHigh:height,
            bitsPerSample:8,samplesPerPixel:4,hasAlpha:true,isPlanar:false,colorSpaceName:.deviceRGB,
            bitmapFormat:.alphaNonpremultiplied,bytesPerRow:560,bitsPerPixel:32), let bytes = bitmap.bitmapData else {
            throw failure("Cannot allocate owned matte")
        }
        for y in 0..<height {
            for x in 0..<140 {
                let a = alphas[x % alphas.count], rgb = colours[y % colours.count], offset = y*bitmap.bytesPerRow+x*4
                for c in 0..<3 { bytes[offset+c] = UInt8((rgb[c]*a+(white ? 255*(255-a) : 0)+127)/255) }
                bytes[offset+3] = opaque ? 255 : 128
            }
        }
        guard let data = bitmap.representation(using:.png,properties:[:]) else { throw failure("Cannot encode owned matte") }
        return data
    }

    private static func pixels() async throws {
        let sampler = SlidePDFMediaSampler(), black = try matte(false), white = try matte(true)
        // Repeated decoding catches storage/lifetime mistakes that can be
        // hidden by logging or debug builds around unowned bitmap pointers.
        for _ in 0..<12 {
            let data = try await sampler.recover(black:black,white:white,pixelLimit:140*64)
            guard let result = NSBitmapImageRep(data:data), let bytes = result.bitmapData,
                  result.pixelsWide == 140, result.pixelsHigh == 64, result.samplesPerPixel == 4 else { throw failure("Invalid recovered PNG") }
            for y in 0..<64 {
                for x in 0..<140 {
                    let a = alphas[x % alphas.count], rgb = colours[y % colours.count], offset = y*result.bytesPerRow+x*4
                    guard Int(bytes[offset+3]) == a else { throw failure("Recovered alpha changed at \(x),\(y)") }
                    for c in 0..<3 where a > 0 {
                        guard abs(Int(bytes[offset+c])-rgb[c]) <= 1 else { throw failure("Recovered colour changed at \(x),\(y)") }
                    }
                }
            }
        }
        for (b,w,limit) in [(Data("bad".utf8),white,8960),(black,white,8959),(black,white,0),
                            (black,white,-1),(black,try matte(true,height:63),8960),
                            (try matte(false,opaque:false),white,8960)] {
            do { _ = try await sampler.recover(black:b,white:w,pixelLimit:limit); throw failure("Invalid matte was accepted") }
            catch let error as NSError where error.domain == NSCocoaErrorDomain {}
        }
        let cancelled = await Task {
            withUnsafeCurrentTask { $0?.cancel() }
            do { _ = try await sampler.recover(black:black,white:white,pixelLimit:8960); return false }
            catch is CancellationError { return true }
            catch { return false }
        }.value
        guard cancelled else { throw failure("Cancelled recovery was accepted") }
        print("PASS: optimized matte pixel recovery and admission")
    }

    @MainActor private static func cancellation(_ phase: String) async throws {
        let renderer = SlideExportRenderer()
        defer { renderer.close() }
        try await renderer.load("""
          <html><body style="margin:0;background:black"><script>
          const target='\(phase)';window.entered='';
          const bounds={x:0,y:0,width:80,height:60};
          const wait=phase=>{window.entered=phase;return new Promise(()=>{});};
          keynopePrepareSnapshot=()=>bounds;
          keynopePDFMediaPlan=()=>target==='plan'?wait('plan'):[{id:'one',x:0,y:0,width:80,height:60}];
          keynopePDFMediaMatte=(id,matte)=>{const phase=matte==='#000000'?'black':'white';document.body.style.background=matte;return target===phase?wait(phase):bounds;};
          keynopePDFMediaInstall=()=>wait('install');
          </script></body></html>
          """)
        try renderer.setPageSize(width:80,height:60)
        var finished = false
        let pending = Task { @MainActor in
            defer { finished = true }
            do { _ = try await renderer.pdfData(page:0); return false }
            catch is CancellationError { return true }
            catch { return false }
        }
        var entered = false
        for _ in 0..<200 {
            if try await renderer.view.evaluateJavaScript("window.entered") as? String == phase { entered = true; break }
            try await Task.sleep(for:.milliseconds(10))
        }
        guard entered, !finished else { renderer.close(); _ = await pending.value; throw failure("Capture did not reach \(phase)") }
        do { _ = try await renderer.imageData(page:0); throw failure("Concurrent capture accepted during \(phase)") }
        catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 6 {}
        guard !finished else { throw failure("Busy rejection interrupted \(phase)") }
        pending.cancel()
        for _ in 0..<100 {
            if finished { break }
            try await Task.sleep(for:.milliseconds(10))
        }
        guard finished else { renderer.close(); _ = await pending.value; throw failure("Cancellation stalled during \(phase)") }
        guard await pending.value else { throw failure("Wrong cancellation result during \(phase)") }
        print("PASS: \(phase) cancellation and exclusive capture")
    }

    @MainActor private static func missingPicture() async throws {
        let renderer = SlideExportRenderer()
        defer { renderer.close() }
        try await renderer.load("""
          <html><body style="margin:0"><div style="position:absolute;inset:0;background:#ff0055"></div><script>
          window.prepares=0;window.installs=0;
          const bounds={x:0,y:0,width:80,height:60};
          keynopePrepareSnapshot=()=>{prepares++;return bounds;};
          keynopePDFMediaPlan=()=>[{id:'one',...bounds}];
          keynopePDFMediaMatte=(id,matte)=>{document.body.style.background=matte;return bounds;};
          // Deliberate dropped picture: keep vector art but omit the sampled PNG.
          keynopePDFMediaInstall=()=>{installs++;return true;};
          </script></body></html>
          """)
        try renderer.setPageSize(width:80,height:60)
        do { _ = try await renderer.pdfData(page:0); throw failure("Missing sampled image was accepted") }
        catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 12 {}
        let counts = try await renderer.view.evaluateJavaScript("[prepares,installs]") as? [Int]
        guard counts == [1,1] else { throw failure("Quality retry resampled or reset the prepared page") }
        let fallback = try await renderer.imageData(page:0)
        guard let bitmap = NSBitmapImageRep(data:fallback), bitmap.pixelsWide >= 80, bitmap.pixelsHigh >= 60 else {
            throw failure("Exact fallback failed after rejected quality capture")
        }
        print("PASS: persistent missing image rejects after bounded same-page retry; exact fallback remains usable")
    }

    @MainActor private static func badBounds() async throws {
        for variant in ["changed-bounds", "outside-page", "too-many", "pixel-budget"] {
          let renderer = SlideExportRenderer()
          defer { renderer.close() }
          let oversized = variant == "pixel-budget"
          let plan: String
          switch variant {
          case "too-many": plan = "Array.from({length:257},(_,i)=>({id:String(i),x:0,y:0,width:1,height:1}))"
          case "outside-page": plan = "[{id:'one',x:80,y:0,width:1,height:1}]"
          case "pixel-budget": plan = "[{id:'one',x:0,y:0,width:10000,height:10000}]"
          default: plan = "[{id:'one',x:0,y:0,width:1,height:1}]"
          }
          try await renderer.load("""
          <html><script>
          keynopePrepareSnapshot=()=>({x:0,y:0,width:\(oversized ? 10000 : 80),height:\(oversized ? 10000 : 60)});
          keynopePDFMediaPlan=()=>\(plan);
          keynopePDFMediaMatte=()=>({x:0,y:0,width:80,height:60});
          </script></html>
          """)
          try renderer.setPageSize(width:80,height:60)
          do { _ = try await renderer.pdfData(page:0); throw failure("Invalid \(variant) capture accepted") }
          catch let error as NSError where error.domain == NSCocoaErrorDomain && !oversized {}
          catch let error as NSError where error.domain == "sh.keynope.export" && error.code == 11 && oversized {}
          print("PASS: \(variant) rejected before snapshot")
        }
    }
}
