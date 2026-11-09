package chain

import (
	"context"
	"net"
	"time"

	"github.com/qqlikegi/forwardz/agent/x/lib/core/hosts"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/logger"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/recorder"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/resolver"
)

type SockOpts struct {
	Mark int
}

type RouterOptions struct {
	Retries    int
	Timeout    time.Duration
	IfceName   string
	Netns      string
	SockOpts   *SockOpts
	Chain      Chainer
	Resolver   resolver.Resolver
	HostMapper hosts.HostMapper
	Recorders  []recorder.RecorderObject
	Logger     logger.Logger
}

type RouterOption func(*RouterOptions)

func InterfaceRouterOption(ifceName string) RouterOption {
	return func(o *RouterOptions) {
		o.IfceName = ifceName
	}
}

func NetnsRouterOption(netns string) RouterOption {
	return func(o *RouterOptions) {
		o.Netns = netns
	}
}

func SockOptsRouterOption(so *SockOpts) RouterOption {
	return func(o *RouterOptions) {
		o.SockOpts = so
	}
}

func TimeoutRouterOption(timeout time.Duration) RouterOption {
	return func(o *RouterOptions) {
		o.Timeout = timeout
	}
}

func RetriesRouterOption(retries int) RouterOption {
	return func(o *RouterOptions) {
		o.Retries = retries
	}
}

func ChainRouterOption(chain Chainer) RouterOption {
	return func(o *RouterOptions) {
		o.Chain = chain
	}
}

func ResolverRouterOption(resolver resolver.Resolver) RouterOption {
	return func(o *RouterOptions) {
		o.Resolver = resolver
	}
}

func HostMapperRouterOption(m hosts.HostMapper) RouterOption {
	return func(o *RouterOptions) {
		o.HostMapper = m
	}
}

func RecordersRouterOption(recorders ...recorder.RecorderObject) RouterOption {
	return func(o *RouterOptions) {
		o.Recorders = recorders
	}
}

func LoggerRouterOption(logger logger.Logger) RouterOption {
	return func(o *RouterOptions) {
		o.Logger = logger
	}
}

type Router interface {
	Options() *RouterOptions
	Dial(ctx context.Context, network, address string) (net.Conn, error)
	Bind(ctx context.Context, network, address string, opts ...BindOption) (net.Listener, error)
}
