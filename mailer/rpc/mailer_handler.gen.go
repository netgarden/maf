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

func newMailerServiceHandler(service MailerService) *MailerServiceHandler {

	serviceHandler := &MailerServiceHandler{}
	serviceHandler.name = "Mailer"
	serviceHandler.path = rrpc.ParsePath("api/admin/mailer")
	serviceHandler.methods = []rrpc.MethodHolder{
		{
			Path:        rrpc.ParsePath(""),
			Type:        "GET",
			ContentType: "application/json",
			HandlerFunc: serviceHandler.handleList,
		},
		{
			Path:        rrpc.ParsePath("{id:string}"),
			Type:        "GET",
			ContentType: "",
			HandlerFunc: serviceHandler.handleGet,
		},
		{
			Path:        rrpc.ParsePath("{id:string}/retry"),
			Type:        "POST",
			ContentType: "",
			HandlerFunc: serviceHandler.handleRetry,
		},
		{
			Path:        rrpc.ParsePath("{id:string}/cancel"),
			Type:        "POST",
			ContentType: "",
			HandlerFunc: serviceHandler.handleCancel,
		},
		{
			Path:        rrpc.ParsePath("send-test"),
			Type:        "POST",
			ContentType: "application/json",
			HandlerFunc: serviceHandler.handleSendTest,
		},
		{
			Path:        rrpc.ParsePath("templates"),
			Type:        "GET",
			ContentType: "",
			HandlerFunc: serviceHandler.handleListTemplates,
		},
		{
			Path:        rrpc.ParsePath("templates/{id:string}"),
			Type:        "GET",
			ContentType: "",
			HandlerFunc: serviceHandler.handleGetTemplate,
		},
		{
			Path:        rrpc.ParsePath("templates/{id:string}"),
			Type:        "POST",
			ContentType: "application/json",
			HandlerFunc: serviceHandler.handleUpdateTemplate,
		},
		{
			Path:        rrpc.ParsePath("templates/{id:string}"),
			Type:        "DELETE",
			ContentType: "",
			HandlerFunc: serviceHandler.handleResetTemplate,
		},
	}
	serviceHandler.service = service

	return serviceHandler
}

type MailerServiceHandler struct {
	name    string
	path    *rrpc.Path
	methods []rrpc.MethodHolder

	service MailerService
}

func (h *MailerServiceHandler) Name() string {
	return h.name
}

func (h *MailerServiceHandler) Path() *rrpc.Path {
	return h.path
}

func (h *MailerServiceHandler) Methods() []rrpc.MethodHolder {
	return h.methods
}

func (h *MailerServiceHandler) handleList(ctx *rrpc.Context) error {

	reqPayload := &ListEmailsRequest{}
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

func (h *MailerServiceHandler) handleGet(ctx *rrpc.Context) error {

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

func (h *MailerServiceHandler) handleRetry(ctx *rrpc.Context) error {

	// Call service method implementation.
	ret0, err1 := h.service.Retry(ctx)
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

func (h *MailerServiceHandler) handleCancel(ctx *rrpc.Context) error {

	// Call service method implementation.
	ret0, err1 := h.service.Cancel(ctx)
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

func (h *MailerServiceHandler) handleSendTest(ctx *rrpc.Context) error {

	reqPayload := &SendTestEmailRequest{}
	reqBody, err := h.readBody(ctx.Request())
	if err != nil {
		return err
	}
	if err := json.Unmarshal(reqBody, reqPayload); err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to unmarshal request data: %w", err)
	}
	// Call service method implementation.
	ret0, err1 := h.service.SendTest(ctx, reqPayload)
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

func (h *MailerServiceHandler) handleListTemplates(ctx *rrpc.Context) error {

	// Call service method implementation.
	ret0, err1 := h.service.ListTemplates(ctx)
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

func (h *MailerServiceHandler) handleGetTemplate(ctx *rrpc.Context) error {

	// Call service method implementation.
	ret0, err1 := h.service.GetTemplate(ctx)
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

func (h *MailerServiceHandler) handleUpdateTemplate(ctx *rrpc.Context) error {

	reqPayload := &UpdateTemplateRequest{}
	reqBody, err := h.readBody(ctx.Request())
	if err != nil {
		return err
	}
	if err := json.Unmarshal(reqBody, reqPayload); err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to unmarshal request data: %w", err)
	}
	// Call service method implementation.
	ret0, err1 := h.service.UpdateTemplate(ctx, reqPayload)
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

func (h *MailerServiceHandler) handleResetTemplate(ctx *rrpc.Context) error {

	// Call service method implementation.
	err1 := h.service.ResetTemplate(ctx)
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

func (h *MailerServiceHandler) readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to read request data: %w", err)
	}

	return reqBody, nil
}
func newMailerServiceClientHandler(client *Client) *MailerServiceClientHandler {
	return &MailerServiceClientHandler{
		client: client,
		name:   "Mailer",
		path:   "api/admin/mailer",
	}
}

type MailerServiceClientHandler struct {
	name   string
	path   string
	client *Client
}

func (h *MailerServiceClientHandler) List(ctx context.Context, data *ListEmailsRequest) (*ListEmailsResponse, error) {

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

	out := &ListEmailsResponse{}
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

func (h *MailerServiceClientHandler) Get(ctx context.Context, id string) (*EmailItem, error) {

	methodType := "GET"
	methodPath := fmt.Sprintf("%s", url.PathEscape(id))
	url := h.client.url + "/" + h.path + "/" + methodPath

	r, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return nil, err
	}

	out := &EmailItem{}
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

func (h *MailerServiceClientHandler) Retry(ctx context.Context, id string) (*EmailItem, error) {

	methodType := "POST"
	methodPath := fmt.Sprintf("%s/retry", url.PathEscape(id))
	url := h.client.url + "/" + h.path + "/" + methodPath

	r, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return nil, err
	}

	out := &EmailItem{}
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

func (h *MailerServiceClientHandler) Cancel(ctx context.Context, id string) (*EmailItem, error) {

	methodType := "POST"
	methodPath := fmt.Sprintf("%s/cancel", url.PathEscape(id))
	url := h.client.url + "/" + h.path + "/" + methodPath

	r, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return nil, err
	}

	out := &EmailItem{}
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

func (h *MailerServiceClientHandler) SendTest(ctx context.Context, data *SendTestEmailRequest) (*EmailItem, error) {

	methodType := "POST"
	methodPath := "send-test"
	url := h.client.url + "/" + h.path + "/" + methodPath

	jsonBody, err := json.Marshal(data)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to marshal request: %w", err)
	}
	r, err := h.doHttpRequest(ctx, methodType, url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	out := &EmailItem{}
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

func (h *MailerServiceClientHandler) ListTemplates(ctx context.Context) (*ListTemplatesResponse, error) {

	methodType := "GET"
	methodPath := "templates"
	url := h.client.url + "/" + h.path + "/" + methodPath

	r, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return nil, err
	}

	out := &ListTemplatesResponse{}
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

func (h *MailerServiceClientHandler) GetTemplate(ctx context.Context, id string) (*TemplateItem, error) {

	methodType := "GET"
	methodPath := fmt.Sprintf("templates/%s", url.PathEscape(id))
	url := h.client.url + "/" + h.path + "/" + methodPath

	r, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return nil, err
	}

	out := &TemplateItem{}
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

func (h *MailerServiceClientHandler) UpdateTemplate(ctx context.Context, id string, data *UpdateTemplateRequest) (*TemplateItem, error) {

	methodType := "POST"
	methodPath := fmt.Sprintf("templates/%s", url.PathEscape(id))
	url := h.client.url + "/" + h.path + "/" + methodPath

	jsonBody, err := json.Marshal(data)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to marshal request: %w", err)
	}
	r, err := h.doHttpRequest(ctx, methodType, url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	out := &TemplateItem{}
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

func (h *MailerServiceClientHandler) ResetTemplate(ctx context.Context, id string) error {

	methodType := "DELETE"
	methodPath := fmt.Sprintf("templates/%s", url.PathEscape(id))
	url := h.client.url + "/" + h.path + "/" + methodPath

	_, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return err
	}

	return nil
}

func (h *MailerServiceClientHandler) doHttpRequest(ctx context.Context, method string, url string, contentType string, body io.Reader) (*SizedReadCloser, error) {

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

func (h *MailerServiceClientHandler) readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to read server response body: %w", err)
	}
	return b, nil
}
