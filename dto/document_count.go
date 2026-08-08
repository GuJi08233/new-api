package dto

// GetRequestDocumentCount returns the number of top-level documents/inputs
// represented by a request and whether that request type exposes a document
// metric. A known zero remains distinguishable from an unavailable metric.
func GetRequestDocumentCount(request Request) (int, bool) {
	switch req := request.(type) {
	case *RerankRequest:
		if req == nil {
			return 0, false
		}
		return len(req.Documents), true
	case *EmbeddingRequest:
		if req == nil {
			return 0, false
		}
		return req.GetInputCount(), true
	case *GeminiEmbeddingRequest:
		if req == nil {
			return 0, false
		}
		return 1, true
	case *GeminiBatchEmbeddingRequest:
		if req == nil {
			return 0, false
		}
		return len(req.Requests), true
	default:
		return 0, false
	}
}
