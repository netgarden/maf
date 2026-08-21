package rpc

import (
	"io"
	"net/http"
	"strings"
)

var DataMethods = []string{"POST", "PUT"}

func NewClient(url string) *Client {

	if string(url[len(url)-1]) == "/" {
		url = url[:len(url)-1]
	}

	c := &Client{
		url:    url,
		client: &http.Client{},
	}

	c.Auth = newAuthServiceClientHandler(c)
	c.OIDCAuth = newOIDCAuthServiceClientHandler(c)
	c.OIDCProviders = newOIDCProvidersServiceClientHandler(c)
	c.Profile = newProfileServiceClientHandler(c)
	c.Providers = newProvidersServiceClientHandler(c)
	c.PublicProviders = newPublicProvidersServiceClientHandler(c)
	c.Users = newUsersServiceClientHandler(c)

	return c
}

type Client struct {
	url    string
	client *http.Client

	Auth            *AuthServiceClientHandler
	OIDCAuth        *OIDCAuthServiceClientHandler
	OIDCProviders   *OIDCProvidersServiceClientHandler
	Profile         *ProfileServiceClientHandler
	Providers       *ProvidersServiceClientHandler
	PublicProviders *PublicProvidersServiceClientHandler
	Users           *UsersServiceClientHandler
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

// toWebSocketURL swaps an "http"/"https" base URL for its "ws"/"wss"
// counterpart - used by generated streaming client methods to dial, since
// websocket.Dial requires a ws(s):// URL, not http(s)://.
func toWebSocketURL(httpURL string) string {
	if strings.HasPrefix(httpURL, "https://") {
		return "wss://" + strings.TrimPrefix(httpURL, "https://")
	}
	if strings.HasPrefix(httpURL, "http://") {
		return "ws://" + strings.TrimPrefix(httpURL, "http://")
	}
	return httpURL
}

// joinURLPath joins base with path segments, collapsing any run of "/"
// the joined path portion picks up along the way (e.g. a service's own
// Path already starting with "/" plus the "/" a generated client method
// inserts between segments) down to one - the scheme separator ("://")
// is left untouched. Mirrors the TS generator's equivalent
// .replace(/\/+/g, '/') normalization of its built URI.
func joinURLPath(base string, segments ...string) string {
	joined := base
	for _, s := range segments {
		joined += "/" + s
	}
	schemeEnd := strings.Index(joined, "://")
	if schemeEnd == -1 {
		return collapseSlashes(joined)
	}
	return joined[:schemeEnd+3] + collapseSlashes(joined[schemeEnd+3:])
}

func collapseSlashes(s string) string {
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

func NewSizedReader(r io.Reader, size int64) *SizedReader {
	return &SizedReader{
		size: size,
		r:    r,
	}
}

type SizedReader struct {
	size int64
	r    io.Reader
}

func (r *SizedReader) Size() int64 {
	return r.size
}

func (r *SizedReader) Read(p []byte) (n int, err error) {
	return r.r.Read(p)
}

func NewSizedReadCloser(r io.ReadCloser, size int64) *SizedReadCloser {
	return &SizedReadCloser{
		SizedReader: SizedReader{
			size: size,
			r:    r,
		},
		r: r,
	}
}

type SizedReadCloser struct {
	SizedReader
	r io.ReadCloser
}

func (r *SizedReadCloser) Close() {
	r.r.Close()
}
