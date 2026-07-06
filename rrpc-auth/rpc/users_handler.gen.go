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

func newUsersServiceHandler(service UsersService) *UsersServiceHandler {

	serviceHandler := &UsersServiceHandler{}
	serviceHandler.name = "Users"
	serviceHandler.path = rrpc.ParsePath("api/admin/users")
	serviceHandler.methods = []rrpc.MethodHolder{
		{
			Path:        rrpc.ParsePath(""),
			Type:        "GET",
			ContentType: "application/json",
			HandlerFunc: serviceHandler.handleList,
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

type UsersServiceHandler struct {
	name    string
	path    *rrpc.Path
	methods []rrpc.MethodHolder

	service UsersService
}

func (h *UsersServiceHandler) Name() string {
	return h.name
}

func (h *UsersServiceHandler) Path() *rrpc.Path {
	return h.path
}

func (h *UsersServiceHandler) Methods() []rrpc.MethodHolder {
	return h.methods
}

func (h *UsersServiceHandler) handleList(ctx *rrpc.Context) error {

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

func (h *UsersServiceHandler) handleCreate(ctx *rrpc.Context) error {

	reqPayload := &CreateUserRequest{}
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

func (h *UsersServiceHandler) handleUpdate(ctx *rrpc.Context) error {

	reqPayload := &UpdateUserRequest{}
	reqBody, err := h.readBody(ctx.Request())
	if err != nil {
		return err
	}
	if err := json.Unmarshal(reqBody, reqPayload); err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to unmarshal request data: %w", err)
	}
	// Call service method implementation.
	err1 := h.service.Update(ctx, reqPayload)
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

func (h *UsersServiceHandler) handleDelete(ctx *rrpc.Context) error {

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

func (h *UsersServiceHandler) readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to read request data: %w", err)
	}

	return reqBody, nil
}
func newUsersServiceClientHandler(client *Client) *UsersServiceClientHandler {
	return &UsersServiceClientHandler{
		client: client,
		name:   "Users",
		path:   "api/admin/users",
	}
}

type UsersServiceClientHandler struct {
	name   string
	path   string
	client *Client
}

func (h *UsersServiceClientHandler) List(ctx context.Context, data *DatatableRequest) (*ListUsersResponse, error) {

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

	out := &ListUsersResponse{}
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

func (h *UsersServiceClientHandler) Create(ctx context.Context, data *CreateUserRequest) (*UserItem, error) {

	methodType := "POST"
	url := h.client.url + "/" + h.path

	jsonBody, err := json.Marshal(data)
	if err != nil {
		return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to marshal request: %w", err)
	}
	r, err := h.doHttpRequest(ctx, methodType, url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	out := &UserItem{}
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

func (h *UsersServiceClientHandler) Update(ctx context.Context, id string, data *UpdateUserRequest) error {

	methodType := "POST"
	methodPath := fmt.Sprintf("%s", url.PathEscape(id))
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

func (h *UsersServiceClientHandler) Delete(ctx context.Context, id string) error {

	methodType := "DELETE"
	methodPath := fmt.Sprintf("%s", url.PathEscape(id))
	url := h.client.url + "/" + h.path + "/" + methodPath

	_, err := h.doHttpRequest(ctx, methodType, url, "", nil)
	if err != nil {
		return err
	}

	return nil
}

func (h *UsersServiceClientHandler) doHttpRequest(ctx context.Context, method string, url string, contentType string, body io.Reader) (*SizedReadCloser, error) {

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

func (h *UsersServiceClientHandler) readAll(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to read server response body: %w", err)
	}
	return b, nil
}
