package rpc

import (
    "bytes"
    "context"
    "encoding/json"
    "io"
    "net/http"
    "strconv"

    "github.com/netgarden/rrpc"
)

func newProfileServiceHandler(service ProfileService) *ProfileServiceHandler {

    serviceHandler := &ProfileServiceHandler{}
    serviceHandler.name = "Profile"
    serviceHandler.path = rrpc.ParsePath("api/profile")
    serviceHandler.methods = []rrpc.MethodHolder{
        {
            Path:        rrpc.ParsePath("changePassword"),
            Type:        "POST",
            ContentType: "application/json",
            HandlerFunc: serviceHandler.handleChangePassword,
        },
    }
    serviceHandler.service = service

    return serviceHandler
}

type ProfileServiceHandler struct {
    name    string
    path    *rrpc.Path
    methods []rrpc.MethodHolder

    service ProfileService
}

func (h *ProfileServiceHandler) Name() string {
    return h.name
}

func (h *ProfileServiceHandler) Path() *rrpc.Path {
    return h.path
}

func (h *ProfileServiceHandler) Methods() []rrpc.MethodHolder {
    return h.methods
}

func (h *ProfileServiceHandler) handleChangePassword(ctx *rrpc.Context) error {

	reqPayload := &ChangePasswordRequest{}
	reqBody, err := h.readBody(ctx.Request())
	if err != nil {
		return err
	}
	if err := json.Unmarshal(reqBody, reqPayload); err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to unmarshal request data: %w", err)
	}
	// Call service method implementation.
    err1 := h.service.ChangePassword(ctx, reqPayload)
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



func (h *ProfileServiceHandler) readBody(r *http.Request) ([]byte, error) {
    defer r.Body.Close()

    reqBody, err := io.ReadAll(r.Body)
	if err != nil {
	    return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to read request data: %w", err)
	}

    return reqBody, nil
}
func newProfileServiceClientHandler(client *Client) *ProfileServiceClientHandler {
    return &ProfileServiceClientHandler{
        client: client,
        name: "Profile",
        path: "api/profile",
    }
}

type ProfileServiceClientHandler struct {
    name string
    path string
    client *Client
}

func (h *ProfileServiceClientHandler) ChangePassword(ctx context.Context, data *ChangePasswordRequest)error {

    methodType := "POST"
    methodPath := "changePassword"
    url := h.client.url + "/" + h.path + "/" + methodPath

    jsonBody, err := json.Marshal(data)
    if err != nil {
        return rrpc.ErrRrpcBadRequest.WithCausef("failed to marshal request: %w", err)
    }
    _, err = h.doHttpRequest(ctx, methodType, url, "application/json",bytes.NewBuffer(jsonBody))
    if err != nil {
        return err
    }

    return nil
}



func (h *ProfileServiceClientHandler) doHttpRequest(ctx context.Context, method string, url string, contentType string, body io.Reader) (*SizedReadCloser, error) {

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


func (h *ProfileServiceClientHandler) readAll(r io.Reader) ([]byte, error) {
    b, err := io.ReadAll(r)
    if err != nil {
        return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to read server response body: %w", err)
    }
    return b, nil
}
