package unix

import (
	"context"
	"net"

	"github.com/qqlikegi/forwardz/agent/x/lib/core/dialer"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/logger"
	md "github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
	"github.com/qqlikegi/forwardz/agent/x/registry"
)

func init() {
	registry.DialerRegistry().Register("unix", NewDialer)
}

type unixDialer struct {
	md     metadata
	logger logger.Logger
}

func NewDialer(opts ...dialer.Option) dialer.Dialer {
	options := &dialer.Options{}
	for _, opt := range opts {
		opt(options)
	}

	return &unixDialer{
		logger: options.Logger,
	}
}

func (d *unixDialer) Init(md md.Metadata) (err error) {
	return d.parseMetadata(md)
}

func (d *unixDialer) Dial(ctx context.Context, addr string, opts ...dialer.DialOption) (net.Conn, error) {
	var options dialer.DialOptions
	for _, opt := range opts {
		opt(&options)
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", addr)
	if err != nil {
		d.logger.Error(err)
	}
	return conn, err
}
