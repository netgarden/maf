package rpc

import (
	"github.com/netgarden/rrpc"

	"io"
)

// -- Services --------------------------------------------

type MetricsService interface {
	Scrape(ctx *rrpc.Context, w io.Writer) error
}
