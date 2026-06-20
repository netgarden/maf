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

func newAuthServiceHandler(service AuthService) *AuthServiceHandler {

    serviceHandler := &AuthServiceHandler{}
    serviceHandler.name = "Auth"
    serviceHandler.path = rrpc.ParsePath("api/auth")
    serviceHandler.methods = []rrpc.MethodHolder{
        {
            Path:        rrpc.ParsePath("login"),
            Type:        "POST",
            ContentType: "application/json",
            HandlerFunc: serviceHandler.handleLogin,
        },
        {
            Path:        rrpc.ParsePath("logout"),
            Type:        "POST",
            ContentType: "",
            HandlerFunc: serviceHandler.handleLogout,
        },
        {
            Path:        rrpc.ParsePath("refresh"),
            Type:        "POST",
            ContentType: "",
            HandlerFunc: serviceHandler.handleRefresh,
        },
        {
            Path:        rrpc.ParsePath("me"),
            Type:        "GET",
            ContentType: "",
            HandlerFunc: serviceHandler.handleMe,
        },
    }
    serviceHandler.service = service

    return serviceHandler
}

type AuthServiceHandler struct {
    name    string
    path    *rrpc.Path
    methods []rrpc.MethodHolder

    service AuthService
}

func (h *AuthServiceHandler) Name() string {
    return h.name
}

func (h *AuthServiceHandler) Path() *rrpc.Path {
    return h.path
}

func (h *AuthServiceHandler) Methods() []rrpc.MethodHolder {
    return h.methods
}

func (h *AuthServiceHandler) handleLogin(ctx *rrpc.Context) error {

	reqPayload := &LoginRequest{}
	reqBody, err := h.readBody(ctx.Request())
	if err != nil {
		return err
	}
	if err := json.Unmarshal(reqBody, reqPayload); err != nil {
		return rrpc.ErrRrpcBadRequest.WithCausef("failed to unmarshal request data: %w", err)
	}
	// Call service method implementation.
    ret0, err1 := h.service.Login(ctx, reqPayload)
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

func (h *AuthServiceHandler) handleLogout(ctx *rrpc.Context) error {

	// Call service method implementation.
    err1 := h.service.Logout(ctx)
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

func (h *AuthServiceHandler) handleRefresh(ctx *rrpc.Context) error {

	// Call service method implementation.
    ret0, err1 := h.service.Refresh(ctx)
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

func (h *AuthServiceHandler) handleMe(ctx *rrpc.Context) error {

	// Call service method implementation.
    ret0, err1 := h.service.Me(ctx)
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



func (h *AuthServiceHandler) readBody(r *http.Request) ([]byte, error) {
    defer r.Body.Close()

    reqBody, err := io.ReadAll(r.Body)
	if err != nil {
	    return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to read request data: %w", err)
	}

    return reqBody, nil
}
func newAuthServiceClientHandler(client *Client) *AuthServiceClientHandler {
    return &AuthServiceClientHandler{
        client: client,
        name: "Auth",
        path: "api/auth",
    }
}

type AuthServiceClientHandler struct {
    name string
    path string
    client *Client
}

func (h *AuthServiceClientHandler) Login(ctx context.Context, data *LoginRequest) (*LoginResponse, error) {

    methodType := "POST"
    methodPath := "login"
    url := h.client.url + "/" + h.path + "/" + methodPath

    jsonBody, err := json.Marshal(data)
    if err != nil {
        return nil, rrpc.ErrRrpcBadRequest.WithCausef("failed to marshal request: %w", err)
    }
    r, err := h.doHttpRequest(ctx, methodType, url, "application/json",bytes.NewBuffer(jsonBody))
    if err != nil {
        return nil, err
    }

    out := &LoginResponse{}
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

func (h *AuthServiceClientHandler) Logout(ctx context.Context)error {

    methodType := "POST"
    methodPath := "logout"
    url := h.client.url + "/" + h.path + "/" + methodPath

    _, err := h.doHttpRequest(ctx, methodType, url, "",nil)
    if err != nil {
        return err
    }

    return nil
}

func (h *AuthServiceClientHandler) Refresh(ctx context.Context) (*RefreshResponse, error) {

    methodType := "POST"
    methodPath := "refresh"
    url := h.client.url + "/" + h.path + "/" + methodPath

    r, err := h.doHttpRequest(ctx, methodType, url, "",nil)
    if err != nil {
        return nil, err
    }

    out := &RefreshResponse{}
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

func (h *AuthServiceClientHandler) Me(ctx context.Context) (*MeResponse, error) {

    methodType := "GET"
    methodPath := "me"
    url := h.client.url + "/" + h.path + "/" + methodPath

    r, err := h.doHttpRequest(ctx, methodType, url, "",nil)
    if err != nil {
        return nil, err
    }

    out := &MeResponse{}
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



func (h *AuthServiceClientHandler) doHttpRequest(ctx context.Context, method string, url string, contentType string, body io.Reader) (*SizedReadCloser, error) {

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


func (h *AuthServiceClientHandler) readAll(r io.Reader) ([]byte, error) {
    b, err := io.ReadAll(r)
    if err != nil {
        return nil, rrpc.ErrRrpcBadResponse.WithCausef("failed to read server response body: %w", err)
    }
    return b, nil
}
