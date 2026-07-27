import Foundation
import Testing
@testable import TranswarpApp

@Suite
struct NoRedirectURLSessionTests {
	@Test
	func redirectDelegateRejectsRedirectRequests() async throws {
		let delegate = NoRedirectURLSessionDelegate()
		let originalURL = try #require(URL(string: "https://transwarp.example.com/status"))
		let redirectURL = try #require(URL(string: "https://redirect.example.com/status"))
		let response = try #require(HTTPURLResponse(
			url: originalURL,
			statusCode: 307,
			httpVersion: "HTTP/1.1",
			headerFields: ["Location": redirectURL.absoluteString]
		))
		let task = URLSession.shared.dataTask(with: originalURL)
		defer {
			task.cancel()
		}

		let redirected = await delegate.urlSession(
			URLSession.shared,
			task: task,
			willPerformHTTPRedirection: response,
			newRequest: URLRequest(url: redirectURL)
		)

		#expect(redirected == nil)
	}
}
