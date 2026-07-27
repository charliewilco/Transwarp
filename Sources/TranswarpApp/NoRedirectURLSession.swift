import Foundation

enum NoRedirectURLSession {
	static func data(for request: URLRequest) async throws -> (Data, URLResponse) {
		let delegate = NoRedirectURLSessionDelegate()
		let session = URLSession(configuration: .ephemeral, delegate: delegate, delegateQueue: nil)
		defer {
			session.finishTasksAndInvalidate()
		}
		return try await session.data(for: request)
	}
}

final class NoRedirectURLSessionDelegate: NSObject, URLSessionTaskDelegate {
	func urlSession(
		_ session: URLSession,
		task: URLSessionTask,
		willPerformHTTPRedirection response: HTTPURLResponse,
		newRequest request: URLRequest
	) async -> URLRequest? {
		nil
	}
}
