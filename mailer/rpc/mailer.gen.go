package rpc

import (
	"github.com/netgarden/rrpc"
)

// -- Structs ---------------------------------------------

type EmailItem struct {
	Id            string   `json:"id"`
	To            []string `json:"to"`
	Cc            []string `json:"cc"`
	Bcc           []string `json:"bcc"`
	Subject       string   `json:"subject"`
	Status        string   `json:"status"`
	Attempts      int      `json:"attempts"`
	LastError     string   `json:"lastError"`
	CreatedAt     string   `json:"createdAt"`
	NextAttemptAt string   `json:"nextAttemptAt"`
	SentAt        string   `json:"sentAt"`
	CancelledAt   string   `json:"cancelledAt"`
	GaveUpAt      string   `json:"gaveUpAt"`
}

type ListEmailsResponse struct {
	Items    []EmailItem       `json:"items"`
	PageInfo DatatablePageInfo `json:"pageInfo"`
}

type ListTemplatesResponse struct {
	Items []TemplateItem `json:"items"`
}

type SendTestEmailRequest struct {
	To string `json:"to"`
}

type TemplateItem struct {
	Id           string `json:"id"`
	Subject      string `json:"subject"`
	BodyText     string `json:"bodyText"`
	BodyHtml     string `json:"bodyHtml"`
	IsCustomized bool   `json:"isCustomized"`
	Description  string `json:"description"`
}

type UpdateTemplateRequest struct {
	Subject  string `json:"subject"`
	BodyText string `json:"bodyText"`
	BodyHtml string `json:"bodyHtml"`
}

// -- Services --------------------------------------------

type MailerService interface {
	List(ctx *rrpc.Context, request *DatatableRequest) (*ListEmailsResponse, error)
	Get(ctx *rrpc.Context) (*EmailItem, error)
	Retry(ctx *rrpc.Context) (*EmailItem, error)
	Cancel(ctx *rrpc.Context) (*EmailItem, error)
	SendTest(ctx *rrpc.Context, request *SendTestEmailRequest) (*EmailItem, error)
	ListTemplates(ctx *rrpc.Context) (*ListTemplatesResponse, error)
	GetTemplate(ctx *rrpc.Context) (*TemplateItem, error)
	UpdateTemplate(ctx *rrpc.Context, request *UpdateTemplateRequest) (*TemplateItem, error)
	ResetTemplate(ctx *rrpc.Context) error
}

// -- Errors ----------------------------------------------

var (
	ErrEmailNotCancellable = rrpc.RRPCError{Code: 3, Name: "EmailNotCancellable", Message: "email is not in a cancellable state", HTTPStatus: 409}

	ErrEmailNotFound = rrpc.RRPCError{Code: 1, Name: "EmailNotFound", Message: "email not found", HTTPStatus: 404}

	ErrEmailNotRetryable = rrpc.RRPCError{Code: 2, Name: "EmailNotRetryable", Message: "email is not in a retryable state", HTTPStatus: 409}

	ErrTemplateInvalid = rrpc.RRPCError{Code: 5, Name: "TemplateInvalid", Message: "template failed to parse", HTTPStatus: 400}

	ErrTemplateNotFound = rrpc.RRPCError{Code: 4, Name: "TemplateNotFound", Message: "template not found", HTTPStatus: 404}

	ErrTestEmailRecipientRequired = rrpc.RRPCError{Code: 6, Name: "TestEmailRecipientRequired", Message: "recipient email address is required", HTTPStatus: 400}
)
