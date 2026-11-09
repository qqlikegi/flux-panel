package handler

import (
	"context"
	"net"

	"github.com/qqlikegi/forwardz/agent/x/lib/core/hop"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
)

type Handler interface {
	Init(metadata.Metadata) error
	Handle(context.Context, net.Conn, ...HandleOption) error
}

type Forwarder interface {
	Forward(hop.Hop)
}
