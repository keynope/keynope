import Foundation

private final class EndpointProbe {
    var presenterURL: URL
    init(_ url: URL) { presenterURL = url }
    func endpoint(_ path: String) -> URL { editorEndpoint(path)! }

    // PRODUCTION_ENDPOINT_BUILDER
}

@main
private struct TransportRegression {
    struct Failure: Error { let message: String }

    static func request(_ session: URLSession, _ url: URL, body: [String: Any]? = nil) async throws -> Data {
        var request = URLRequest(url: url)
        request.timeoutInterval = 10
        if let body {
            request.httpMethod = "POST"
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        let (data, response) = try await session.data(for: request)
        guard (response as? HTTPURLResponse)?.statusCode == 200 else {
            throw Failure(message: "Native request failed: \((response as? HTTPURLResponse)?.statusCode ?? 0) \(String(decoding: data, as: UTF8.self))")
        }
        return data
    }

    static func main() async throws {
        let oldURL = URL(string: CommandLine.arguments[1])!
        let newURL = URL(string: CommandLine.arguments[2])!
        let path = CommandLine.arguments[3]
        let configuration = URLSessionConfiguration.ephemeral
        let session = URLSession(configuration: configuration)
        defer { session.invalidateAndCancel() }
        let probe = EndpointProbe(oldURL)
        var warm = URLComponents(url: oldURL, resolvingAgainstBaseURL: false)!
        warm.path = "/api/editor/state"
        _ = try await request(session, warm.url!)
        _ = try await request(session, probe.endpoint("/api/editor/state"))

        // Reopening the same file changes the engine URL/token. Deliberately
        // retain the old cookie, as the real native URLSession does.
        probe.presenterURL = newURL
        _ = try await request(session, probe.endpoint("/api/editor/state"))
        // A late old-engine response must not invalidate subsequent saves.
        _ = try await request(session, warm.url!)
        let data = try await request(session, probe.endpoint("/api/editor/document"), body: ["path": path])
        let document = try JSONSerialization.jsonObject(with: data) as! [String: Any]
        try Data((document["content"] as! String).utf8).write(to: URL(fileURLWithPath: path), options: .atomic)
        _ = try await request(session, probe.endpoint("/api/editor/action"), body: ["action": "confirm-save", "path": path, "value": document["version"]!])

        let queryData = try await request(session, probe.endpoint("/probe?format=png&name=A%26B&token=wrong#unused"))
        let query = try JSONSerialization.jsonObject(with: queryData) as! [String: [String]]
        let token = URLComponents(url: newURL, resolvingAgainstBaseURL: false)!.queryItems!.first { $0.name == "token" }!.value!
        guard query["format"] == ["png"], query["name"] == ["A&B"], query["token"] == [token], query["keynopeSurface"] == nil else {
            throw Failure(message: "Endpoint builder corrupted the request query or reused a stale token")
        }
        guard probe.endpoint("/probe").fragment == nil else { throw Failure(message: "Request retained a fragment") }

        let noCookies = URLSessionConfiguration.ephemeral
        noCookies.httpCookieStorage = nil
        noCookies.httpShouldSetCookies = false
        let fresh = URLSession(configuration: noCookies)
        defer { fresh.invalidateAndCancel() }
        _ = try await request(fresh, probe.endpoint("/api/editor/state"))
        print("PASS: native same-deck reopen, stale/no cookies, save confirmation and query preservation")
    }
}
