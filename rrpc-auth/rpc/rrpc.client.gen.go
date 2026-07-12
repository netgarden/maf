package rpc

import (
	"io"
	"net/http"
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
