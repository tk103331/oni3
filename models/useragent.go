package models

import (
	"net/http"
)

// UserAgent is the User-Agent header sent with every model request.
//
// Provider SDKs ship their own values (for example "OpenAI/Go v1.x"), so the
// client stamps this one on every outgoing request instead.
const UserAgent = "Oni3"

// userAgentTransport sets the User-Agent header on every outgoing request.
type userAgentTransport struct {
	base http.RoundTripper
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone so the SDK-owned request is not mutated: the same request may be
	// retried by the SDK with different headers.
	out := req.Clone(req.Context())
	out.Header.Set("User-Agent", UserAgent)
	return t.base.RoundTrip(out)
}

// userAgentClient builds an HTTP client that stamps the fixed User-Agent on
// every request.
func userAgentClient() *http.Client {
	return &http.Client{
		Transport: &userAgentTransport{base: http.DefaultTransport},
	}
}
