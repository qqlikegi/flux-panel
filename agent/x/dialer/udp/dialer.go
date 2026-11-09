package udp

import (
	"context"
	"net"

	"github.com/qqlikegi/forwardz/agent/x/lib/core/dialer"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/logger"
	md "github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
	"github.com/qqlikegi/forwardz/agent/x/registry"
)

func init() {
	registry.DialerRegistry().Register("udp", NewDialer)
}

type udpDialer struct {
	md     metadata
	logger logger.Logger
}

func NewDialer(opts ...dialer.Option) dialer.Dialer {
	options := &dialer.Options{}
	for _, opt := range opts {
		opt(options)
	}

	return &udpDialer{
		logger: options.Logger,
	}
}

func (d *udpDialer) Init(md md.Metadata) (err error) {
	return d.parseMetadata(md)
}

func (d *udpDialer) Dial(ctx context.Context, addr string, opts ...dialer.DialOption) (net.Conn, error) {
	var options dialer.DialOptions
	for _, opt := range opts {
		opt(&options)
	}

	c, err := options.Dialer.Dial(ctx, "udp", addr)
	if err != nil {
		return nil, err
	}
	return &conn{
		UDPConn: c.(*net.UDPConn),
	}, nil
}
