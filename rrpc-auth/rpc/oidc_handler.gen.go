package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/netgarden/rrpc"
)

func newOIDCAuthServiceHandler(service OIDCAuthService) *OIDCAuthServiceHandler {

	serviceHandler := &OIDCAuthServiceHandler{}
	serviceHandler.name = "OIDCAuth"
	serviceHandler.path = rrpc.ParsePath("api/auth/oidc")
	serviceHandler.methods = []rrpc.MethodHolder{
		{
			Path:        rrpc.ParsePath("{slug:string}/login"),
			Type:        "GET",
			ContentType: "",
			HandlerFunc: serviceHandler.handleLogin,
		},
		{
			Path:        rrpc.ParsePath("{slug:string}/callback"),
			Type:        "GET",
			ContentType: "",
			HandlerFunc: serviceHandler.handleCallback,
		},
	}
	serviceHandler.service = service

	return serviceHandler
}

type OIDCAuthServiceHandler struct {
	name    string
	path    *rrpc.Path
	methods []rrpc.MethodHolder

	service OIDCAuthService
}

func (h *OIDCAuthServiceHandler) Name() string {
	return h.name
}

func (h *OIDCAuthServiceHandler) Path() *rrpc.Path {
	return h.path
}

func (h *OIDCAuthServiceHandler) Methods() []rrpc.MethodHolder {
	return h.methods
}

func (h *OIDCAuthServiceHandler) handleLogin(ctx *rrpc.Context) error {

	// Call service method implementation.
	err1 := h.service.Login(ctx)
	if err1 != nil {
		rpcErr, ok := err1.(rrpc.RRPCError)
		if !ok {
			rpcErr = rrpc.ErrRrpcEndpoint.WithCause(err1)
		}
		return rpcErr
	}

	// ctx.Response().WriteHeader(http.StatusOK)

	return nil
}

func (h *OIDCAuthServiceHandler) handleCallback(ctx *rrpc.Context) error {

	// Call service method implementation.
	err1 := h.service.Callback(ctx)
	if err1 != nil {
		rpcErr, ok := err1.(rrpc.RRPCError)
		if !ok {
			rpcErr = rrpc.ErrRrpcEndpoint.WithCause(err1)
		}
		return rpcErr
	}

	// ctx.Response().WriteHeader(http.StatusOK)

	return nil
}

func (h *OIDCAuthServiceHandler) readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to read request data: %w", err)
	}

	return reqBody, nil
}
func newOIDCAuthServiceClientHandler(client *Client) *OIDCAuthServiceClientHandler {
	return &OIDCAuthServiceClientHandler{
		client: client,
		name:   "OIDCAuth",
		path:   "api/auth/oidc",
	}
}

type OIDCAuthServiceClientHandler struct {
	name   string
	path   string
	client *Client
}

func (h *OIDCAuthServiceClientHandler) Login(ctx context.Context, slug string) error {

	methodType := "GET"
	methodPath := fmt.Sprintf("%s/login", url.PathEscape(slug))
	url := joinURLPath(h.client.url, h.path, methodPath)

	_, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return err
	}

	return nil
}

func (h *OIDCAuthServiceClientHandler) Callback(ctx context.Context, slug string) error {

	methodType := "GET"
	methodPath := fmt.Sprintf("%s/callback", url.PathEscape(slug))
	url := joinURLPath(h.client.url, h.path, methodPath)

	_, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return err
	}

	return nil
}

func (h *OIDCAuthServiceClientHandler) doHttpRequest(ctx context.Context, method string, url string, contentType string, body io.Reader) (*SizedReadCloser, error) {

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to create http request: %w", err)
	}

	if ctx != nil {
		req = req.WithContext(ctx)
	}

	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, rrpc.ErrRrpcRequestFailed.WithCause(err)
	}

	if resp.StatusCode != 200 {
		defer resp.Body.Close()

		respBody, err := h.readAll(resp.Body)
		if err != nil {
			return nil, err
		}

		var rpcErr rrpc.RRPCError
		if err := json.Unmarshal(respBody, &rpcErr); err != nil {
			return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to unmarshal server error: %w", err)
		}

		return nil, rpcErr.WithCauseString()
	}

	var ret *SizedReadCloser

	contentLengthStr := resp.Header.Get("Content-Length")
	if contentLengthStr != "" {

		contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64)
		if err != nil {
			return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to parse Content-Length header: %w", err)
		}

		ret = NewSizedReadCloser(resp.Body, contentLength)
	}

	return ret, nil
}

func (h *OIDCAuthServiceClientHandler) readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to read server response body: %w", err)
	}
	return b, nil
}
func newOIDCProvidersServiceHandler(service OIDCProvidersService) *OIDCProvidersServiceHandler {

	serviceHandler := &OIDCProvidersServiceHandler{}
	serviceHandler.name = "OIDCProviders"
	serviceHandler.path = rrpc.ParsePath("api/admin/oidc/providers")
	serviceHandler.methods = []rrpc.MethodHolder{
		{
			Path:        rrpc.ParsePath("{id:string}"),
			Type:        "GET",
			ContentType: "",
			HandlerFunc: serviceHandler.handleGet,
		},
		{
			Path:        rrpc.ParsePath(""),
			Type:        "POST",
			ContentType: "application/json",
			HandlerFunc: serviceHandler.handleCreate,
		},
		{
			Path:        rrpc.ParsePath("{id:string}"),
			Type:        "POST",
			ContentType: "application/json",
			HandlerFunc: serviceHandler.handleUpdate,
		},
	}
	serviceHandler.service = service

	return serviceHandler
}

type OIDCProvidersServiceHandler struct {
	name    string
	path    *rrpc.Path
	methods []rrpc.MethodHolder

	service OIDCProvidersService
}

func (h *OIDCProvidersServiceHandler) Name() string {
	return h.name
}

func (h *OIDCProvidersServiceHandler) Path() *rrpc.Path {
	return h.path
}

func (h *OIDCProvidersServiceHandler) Methods() []rrpc.MethodHolder {
	return h.methods
}

func (h *OIDCProvidersServiceHandler) handleGet(ctx *rrpc.Context) error {

	// Call service method implementation.
	ret0, err1 := h.service.Get(ctx)
	if err1 != nil {
		rpcErr, ok := err1.(rrpc.RRPCError)
		if !ok {
			rpcErr = rrpc.ErrRrpcEndpoint.WithCause(err1)
		}
		return rpcErr
	}

	// ctx.Response().WriteHeader(http.StatusOK)

	respBody, err := json.Marshal(ret0)
	if err != nil {
		return rrpc.ErrRrpcBadResponse.WithCausef("failed to marshal json response: %w", err)
	}

	ctx.Response().Header().Set("Content-Type", "application/json")
	ctx.Response().Write(respBody)

	return nil
}

func (h *OIDCProvidersServiceHandler) handleCreate(ctx *rrpc.Context) error {

	reqPayload := &CreateOIDCProviderRequest{}
	reqBody, err := h.readBody(ctx.Request())
	if err != nil {
		return err
	}
	if err := json.Unmarshal(reqBody, reqPayload); err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to unmarshal request data: %w", err)
	}
	// Call service method implementation.
	ret0, err1 := h.service.Create(ctx, reqPayload)
	if err1 != nil {
		rpcErr, ok := err1.(rrpc.RRPCError)
		if !ok {
			rpcErr = rrpc.ErrRrpcEndpoint.WithCause(err1)
		}
		return rpcErr
	}

	// ctx.Response().WriteHeader(http.StatusOK)

	respBody, err := json.Marshal(ret0)
	if err != nil {
		return rrpc.ErrRrpcBadResponse.WithCausef("failed to marshal json response: %w", err)
	}

	ctx.Response().Header().Set("Content-Type", "application/json")
	ctx.Response().Write(respBody)

	return nil
}

func (h *OIDCProvidersServiceHandler) handleUpdate(ctx *rrpc.Context) error {

	reqPayload := &UpdateOIDCProviderRequest{}
	reqBody, err := h.readBody(ctx.Request())
	if err != nil {
		return err
	}
	if err := json.Unmarshal(reqBody, reqPayload); err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to unmarshal request data: %w", err)
	}
	// Call service method implementation.
	ret0, err1 := h.service.Update(ctx, reqPayload)
	if err1 != nil {
		rpcErr, ok := err1.(rrpc.RRPCError)
		if !ok {
			rpcErr = rrpc.ErrRrpcEndpoint.WithCause(err1)
		}
		return rpcErr
	}

	// ctx.Response().WriteHeader(http.StatusOK)

	respBody, err := json.Marshal(ret0)
	if err != nil {
		return rrpc.ErrRrpcBadResponse.WithCausef("failed to marshal json response: %w", err)
	}

	ctx.Response().Header().Set("Content-Type", "application/json")
	ctx.Response().Write(respBody)

	return nil
}

func (h *OIDCProvidersServiceHandler) readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to read request data: %w", err)
	}

	return reqBody, nil
}
func newOIDCProvidersServiceClientHandler(client *Client) *OIDCProvidersServiceClientHandler {
	return &OIDCProvidersServiceClientHandler{
		client: client,
		name:   "OIDCProviders",
		path:   "api/admin/oidc/providers",
	}
}

type OIDCProvidersServiceClientHandler struct {
	name   string
	path   string
	client *Client
}

func (h *OIDCProvidersServiceClientHandler) Get(ctx context.Context, id string) (*OIDCProviderItem, error) {

	methodType := "GET"
	methodPath := fmt.Sprintf("%s", url.PathEscape(id))
	url := joinURLPath(h.client.url, h.path, methodPath)

	r, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return nil, err
	}

	out := &OIDCProviderItem{}
	defer r.Close()
	respBody, err := h.readAll(r)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to unmarshal response: %w", err)
	}

	return out, nil
}

func (h *OIDCProvidersServiceClientHandler) Create(ctx context.Context, data *CreateOIDCProviderRequest) (*OIDCProviderItem, error) {

	methodType := "POST"
	url := joinURLPath(h.client.url, h.path)

	jsonBody, err := json.Marshal(data)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to marshal request: %w", err)
	}
	r, err := h.doHttpRequest(ctx, methodType, url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	out := &OIDCProviderItem{}
	defer r.Close()
	respBody, err := h.readAll(r)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to unmarshal response: %w", err)
	}

	return out, nil
}

func (h *OIDCProvidersServiceClientHandler) Update(ctx context.Context, id string, data *UpdateOIDCProviderRequest) (*OIDCProviderItem, error) {

	methodType := "POST"
	methodPath := fmt.Sprintf("%s", url.PathEscape(id))
	url := joinURLPath(h.client.url, h.path, methodPath)

	jsonBody, err := json.Marshal(data)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to marshal request: %w", err)
	}
	r, err := h.doHttpRequest(ctx, methodType, url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	out := &OIDCProviderItem{}
	defer r.Close()
	respBody, err := h.readAll(r)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to unmarshal response: %w", err)
	}

	return out, nil
}

func (h *OIDCProvidersServiceClientHandler) doHttpRequest(ctx context.Context, method string, url string, contentType string, body io.Reader) (*SizedReadCloser, error) {

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to create http request: %w", err)
	}

	if ctx != nil {
		req = req.WithContext(ctx)
	}

	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, rrpc.ErrRrpcRequestFailed.WithCause(err)
	}

	if resp.StatusCode != 200 {
		defer resp.Body.Close()

		respBody, err := h.readAll(resp.Body)
		if err != nil {
			return nil, err
		}

		var rpcErr rrpc.RRPCError
		if err := json.Unmarshal(respBody, &rpcErr); err != nil {
			return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to unmarshal server error: %w", err)
		}

		return nil, rpcErr.WithCauseString()
	}

	var ret *SizedReadCloser

	contentLengthStr := resp.Header.Get("Content-Length")
	if contentLengthStr != "" {

		contentLength, err := strconv.ParseInt(contentLengthStr, 10, 64)
		if err != nil {
			return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to parse Content-Length header: %w", err)
		}

		ret = NewSizedReadCloser(resp.Body, contentLength)
	}

	return ret, nil
}

func (h *OIDCProvidersServiceClientHandler) readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to read server response body: %w", err)
	}
	return b, nil
}
