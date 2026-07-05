package mailer

// Hand-written implementation of the generated rpc.MailerService interface
// (see rpc/mailer.gen.go — never hand-edit that file, regenerate it with
// `make generate` after changing rpc/def/mailer.rrpc). This lives in the
// package alongside Service rather than in a separate rpc/impl/ package —
// unlike e.g. citadel's or maf/auth's split between a "services" package
// and a "module.go" wiring package, mailer's business logic (Service)
// already lives directly in this package, so putting the impl here too
// avoids an import cycle (rpc/impl would need *mailer.Service, and
// module.go already needs to import rpc/impl to wire things up) with no
// upside from a separate package.

import (
	"errors"
	"time"

	"github.com/netgarden/maf/mailer/rpc"
	"github.com/netgarden/rrpc"
)

func newMailerRPCService(svc *Service) rpc.MailerService {
	return &mailerRPCService{svc: svc}
}

type mailerRPCService struct {
	svc *Service
}

func (s *mailerRPCService) List(ctx *rrpc.Context, req *rpc.ListEmailsRequest) (*rpc.ListEmailsResponse, error) {
	result, err := s.svc.ListEmails(ListEmailsFilter{
		Status:   req.Status,
		Search:   req.Search,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}

	items := make([]rpc.EmailItem, 0, len(result.Items))
	for _, email := range result.Items {
		items = append(items, toEmailItem(email))
	}

	return &rpc.ListEmailsResponse{
		Items:    items,
		Total:    int(result.Total),
		Page:     result.Page,
		PageSize: result.PageSize,
	}, nil
}

func (s *mailerRPCService) Get(ctx *rrpc.Context) (*rpc.EmailItem, error) {
	id := ctx.Params().GetString("id")
	email, err := s.svc.GetEmail(id)
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if email == nil {
		return nil, rpc.ErrEmailNotFound
	}
	item := toEmailItem(*email)
	return &item, nil
}

func (s *mailerRPCService) Retry(ctx *rrpc.Context) (*rpc.EmailItem, error) {
	id := ctx.Params().GetString("id")
	email, err := s.svc.RetryEmail(id)
	if err != nil {
		return nil, mapEmailStateError(err)
	}
	item := toEmailItem(*email)
	return &item, nil
}

func (s *mailerRPCService) Cancel(ctx *rrpc.Context) (*rpc.EmailItem, error) {
	id := ctx.Params().GetString("id")
	email, err := s.svc.CancelEmail(id)
	if err != nil {
		return nil, mapEmailStateError(err)
	}
	item := toEmailItem(*email)
	return &item, nil
}

func (s *mailerRPCService) ListTemplates(ctx *rrpc.Context) (*rpc.ListTemplatesResponse, error) {
	templates, err := s.svc.ListTemplates()
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}

	items := make([]rpc.TemplateItem, 0, len(templates))
	for _, t := range templates {
		items = append(items, toTemplateItem(t))
	}

	return &rpc.ListTemplatesResponse{Items: items}, nil
}

func (s *mailerRPCService) GetTemplate(ctx *rrpc.Context) (*rpc.TemplateItem, error) {
	id := ctx.Params().GetString("id")
	info, err := s.svc.GetTemplateInfo(id)
	if err != nil {
		return nil, rrpc.ErrRrpcInternalError.WithCause(err)
	}
	if info == nil {
		return nil, rpc.ErrTemplateNotFound
	}
	item := toTemplateItem(*info)
	return &item, nil
}

func (s *mailerRPCService) UpdateTemplate(ctx *rrpc.Context, req *rpc.UpdateTemplateRequest) (*rpc.TemplateItem, error) {
	id := ctx.Params().GetString("id")

	var bodyHTML *string
	if req.BodyHtml != "" {
		bodyHTML = &req.BodyHtml
	}

	info, err := s.svc.UpdateTemplate(id, req.Subject, req.BodyText, bodyHTML)
	if err != nil {
		switch {
		case errors.Is(err, ErrTemplateNotFound):
			return nil, rpc.ErrTemplateNotFound
		case errors.Is(err, ErrTemplateInvalid):
			return nil, rpc.ErrTemplateInvalid
		default:
			return nil, rrpc.ErrRrpcInternalError.WithCause(err)
		}
	}

	item := toTemplateItem(*info)
	return &item, nil
}

func (s *mailerRPCService) ResetTemplate(ctx *rrpc.Context) error {
	id := ctx.Params().GetString("id")
	err := s.svc.ResetTemplate(id)
	if err != nil {
		if errors.Is(err, ErrTemplateNotFound) {
			return rpc.ErrTemplateNotFound
		}
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
	return nil
}

func mapEmailStateError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return rpc.ErrEmailNotFound
	case errors.Is(err, ErrNotRetryable):
		return rpc.ErrEmailNotRetryable
	case errors.Is(err, ErrNotCancellable):
		return rpc.ErrEmailNotCancellable
	default:
		return rrpc.ErrRrpcInternalError.WithCause(err)
	}
}

func toEmailItem(email Email) rpc.EmailItem {
	status := email.EffectiveStatus()

	// NextAttemptAt only means something while the email is still active in
	// the queue — for a terminal status it would just show a stale value
	// from before that status was reached.
	var nextAttemptAt string
	if status == EmailStatusQueued || status == EmailStatusSending {
		nextAttemptAt = email.NextAttemptAt.Format(time.RFC3339)
	}

	return rpc.EmailItem{
		Id:            email.ID.String(),
		To:            email.To,
		Cc:            email.Cc,
		Bcc:           email.Bcc,
		Subject:       email.Subject,
		Status:        string(status),
		Attempts:      email.Attempts,
		LastError:     derefString(email.LastError),
		CreatedAt:     email.CreatedAt.Format(time.RFC3339),
		NextAttemptAt: nextAttemptAt,
		SentAt:        formatTimePtr(email.SentAt),
		CancelledAt:   formatTimePtr(email.CancelledAt),
		GaveUpAt:      formatTimePtr(email.GaveUpAt),
	}
}

func toTemplateItem(info TemplateInfo) rpc.TemplateItem {
	return rpc.TemplateItem{
		Id:           info.ID,
		Subject:      info.Subject,
		BodyText:     info.BodyText,
		BodyHtml:     derefString(info.BodyHTML),
		IsCustomized: info.IsCustomized,
		Description:  info.Description,
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
