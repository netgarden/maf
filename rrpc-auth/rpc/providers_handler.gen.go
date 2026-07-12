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

func newProvidersServiceHandler(service ProvidersService) *ProvidersServiceHandler {

	serviceHandler := &ProvidersServiceHandler{}
	serviceHandler.name = "Providers"
	serviceHandler.path = rrpc.ParsePath("api/admin/providers")
	serviceHandler.methods = []rrpc.MethodHolder{
		{
			Path:        rrpc.ParsePath(""),
			Type:        "GET",
			ContentType: "application/json",
			HandlerFunc: serviceHandler.handleList,
		},
		{
			Path:        rrpc.ParsePath("{id:string}/enabled"),
			Type:        "POST",
			ContentType: "application/json",
			HandlerFunc: serviceHandler.handleSetEnabled,
		},
		{
			Path:        rrpc.ParsePath("{id:string}"),
			Type:        "DELETE",
			ContentType: "",
			HandlerFunc: serviceHandler.handleDelete,
		},
	}
	serviceHandler.service = service

	return serviceHandler
}

type ProvidersServiceHandler struct {
	name    string
	path    *rrpc.Path
	methods []rrpc.MethodHolder

	service ProvidersService
}

func (h *ProvidersServiceHandler) Name() string {
	return h.name
}

func (h *ProvidersServiceHandler) Path() *rrpc.Path {
	return h.path
}

func (h *ProvidersServiceHandler) Methods() []rrpc.MethodHolder {
	return h.methods
}

func (h *ProvidersServiceHandler) handleList(ctx *rrpc.Context) error {

	reqPayload := &DatatableRequest{}
	if err := rrpc.DecodeQueryParams(ctx.Request().URL.Query(), reqPayload); err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to decode query params: %w", err)
	}
	// Call service method implementation.
	ret0, err1 := h.service.List(ctx, reqPayload)
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

func (h *ProvidersServiceHandler) handleSetEnabled(ctx *rrpc.Context) error {

	reqPayload := &SetProviderEnabledRequest{}
	reqBody, err := h.readBody(ctx.Request())
	if err != nil {
		return err
	}
	if err := json.Unmarshal(reqBody, reqPayload); err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to unmarshal request data: %w", err)
	}
	// Call service method implementation.
	err1 := h.service.SetEnabled(ctx, reqPayload)
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

func (h *ProvidersServiceHandler) handleDelete(ctx *rrpc.Context) error {

	// Call service method implementation.
	err1 := h.service.Delete(ctx)
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

func (h *ProvidersServiceHandler) readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to read request data: %w", err)
	}

	return reqBody, nil
}
func newProvidersServiceClientHandler(client *Client) *ProvidersServiceClientHandler {
	return &ProvidersServiceClientHandler{
		client: client,
		name:   "Providers",
		path:   "api/admin/providers",
	}
}

type ProvidersServiceClientHandler struct {
	name   string
	path   string
	client *Client
}

func (h *ProvidersServiceClientHandler) List(ctx context.Context, data *DatatableRequest) (*ListProvidersResponse, error) {

	methodType := "GET"
	url := h.client.url + "/" + h.path

	jsonBody, err := json.Marshal(data)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to marshal request: %w", err)
	}
	r, err := h.doHttpRequest(ctx, methodType, url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	out := &ListProvidersResponse{}
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

func (h *ProvidersServiceClientHandler) SetEnabled(ctx context.Context, id string, data *SetProviderEnabledRequest) error {

	methodType := "POST"
	methodPath := fmt.Sprintf("%s/enabled", url.PathEscape(id))
	url := h.client.url + "/" + h.path + "/" + methodPath

	jsonBody, err := json.Marshal(data)
	if err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to marshal request: %w", err)
	}
	_, err = h.doHttpRequest(ctx, methodType, url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}

	return nil
}

func (h *ProvidersServiceClientHandler) Delete(ctx context.Context, id string) error {

	methodType := "DELETE"
	methodPath := fmt.Sprintf("%s", url.PathEscape(id))
	url := h.client.url + "/" + h.path + "/" + methodPath

	_, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return err
	}

	return nil
}

func (h *ProvidersServiceClientHandler) doHttpRequest(ctx context.Context, method string, url string, contentType string, body io.Reader) (*SizedReadCloser, error) {

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

func (h *ProvidersServiceClientHandler) readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to read server response body: %w", err)
	}
	return b, nil
}
func newPublicProvidersServiceHandler(service PublicProvidersService) *PublicProvidersServiceHandler {

	serviceHandler := &PublicProvidersServiceHandler{}
	serviceHandler.name = "PublicProviders"
	serviceHandler.path = rrpc.ParsePath("api/auth/providers")
	serviceHandler.methods = []rrpc.MethodHolder{
		{
			Path:        rrpc.ParsePath(""),
			Type:        "GET",
			ContentType: "",
			HandlerFunc: serviceHandler.handleList,
		},
	}
	serviceHandler.service = service

	return serviceHandler
}

type PublicProvidersServiceHandler struct {
	name    string
	path    *rrpc.Path
	methods []rrpc.MethodHolder

	service PublicProvidersService
}

func (h *PublicProvidersServiceHandler) Name() string {
	return h.name
}

func (h *PublicProvidersServiceHandler) Path() *rrpc.Path {
	return h.path
}

func (h *PublicProvidersServiceHandler) Methods() []rrpc.MethodHolder {
	return h.methods
}

func (h *PublicProvidersServiceHandler) handleList(ctx *rrpc.Context) error {

	// Call service method implementation.
	ret0, err1 := h.service.List(ctx)
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

func (h *PublicProvidersServiceHandler) readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to read request data: %w", err)
	}

	return reqBody, nil
}
func newPublicProvidersServiceClientHandler(client *Client) *PublicProvidersServiceClientHandler {
	return &PublicProvidersServiceClientHandler{
		client: client,
		name:   "PublicProviders",
		path:   "api/auth/providers",
	}
}

type PublicProvidersServiceClientHandler struct {
	name   string
	path   string
	client *Client
}

func (h *PublicProvidersServiceClientHandler) List(ctx context.Context) (*ListPublicProvidersResponse, error) {

	methodType := "GET"
	url := h.client.url + "/" + h.path

	r, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return nil, err
	}

	out := &ListPublicProvidersResponse{}
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

func (h *PublicProvidersServiceClientHandler) doHttpRequest(ctx context.Context, method string, url string, contentType string, body io.Reader) (*SizedReadCloser, error) {

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

func (h *PublicProvidersServiceClientHandler) readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to read server response body: %w", err)
	}
	return b, nil
}
