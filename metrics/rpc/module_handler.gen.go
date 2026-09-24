package rpc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/netgarden/rrpc"
)

func newMetricsServiceHandler(service MetricsService) *MetricsServiceHandler {

	serviceHandler := &MetricsServiceHandler{}
	serviceHandler.name = "Metrics"
	serviceHandler.path = rrpc.ParsePath("metrics")
	serviceHandler.methods = []rrpc.MethodHolder{
		{
			Path:        rrpc.ParsePath(""),
			Type:        "GET",
			ContentType: "",
			HandlerFunc: serviceHandler.handleScrape,
		},
	}
	serviceHandler.service = service

	return serviceHandler
}

type MetricsServiceHandler struct {
	name    string
	path    *rrpc.Path
	methods []rrpc.MethodHolder

	service MetricsService
}

func (h *MetricsServiceHandler) Name() string {
	return h.name
}

func (h *MetricsServiceHandler) Path() *rrpc.Path {
	return h.path
}

func (h *MetricsServiceHandler) Methods() []rrpc.MethodHolder {
	return h.methods
}

func (h *MetricsServiceHandler) handleScrape(ctx *rrpc.Context) error {

	w1 := rrpc.NewResponseWriter(ctx.Response(), "text/plain; version=0.0.4; charset=utf-8")

	// Call service method implementation.
	err1 := h.service.Scrape(ctx, w1)
	if err1 != nil {
		rpcErr, ok := err1.(rrpc.RRPCError)
		if !ok {
			rpcErr = rrpc.ErrRrpcEndpoint.WithCause(err1)
		}
		return rpcErr
	}

	// ctx.Response().WriteHeader(http.StatusOK)

	if ctx.Response().Header().Get("Content-Type") == "" {
		ctx.Response().Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	}

	return nil
}

func (h *MetricsServiceHandler) readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to read request data: %w", err)
	}

	return reqBody, nil
}
func newMetricsServiceClientHandler(client *Client) *MetricsServiceClientHandler {
	return &MetricsServiceClientHandler{
		client: client,
		name:   "Metrics",
		path:   "metrics",
	}
}

type MetricsServiceClientHandler struct {
	name   string
	path   string
	client *Client
}

func (h *MetricsServiceClientHandler) Scrape(ctx context.Context) (*SizedReadCloser, error) {

	methodType := "GET"
	url := joinURLPath(h.client.url, h.path)

	r, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (h *MetricsServiceClientHandler) doHttpRequest(ctx context.Context, method string, url string, contentType string, body io.Reader) (*SizedReadCloser, error) {

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

func (h *MetricsServiceClientHandler) readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to read server response body: %w", err)
	}
	return b, nil
}
