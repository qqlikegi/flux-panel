package unix

import (
	"net"

	"github.com/qqlikegi/forwardz/agent/x/lib/core/limiter"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/listener"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/logger"
	md "github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
	admission "github.com/qqlikegi/forwardz/agent/x/admission/wrapper"
	climiter "github.com/qqlikegi/forwardz/agent/x/limiter/conn/wrapper"
	limiter_wrapper "github.com/qqlikegi/forwardz/agent/x/limiter/traffic/wrapper"
	metrics "github.com/qqlikegi/forwardz/agent/x/metrics/wrapper"
	stats "github.com/qqlikegi/forwardz/agent/x/observer/stats/wrapper"
	"github.com/qqlikegi/forwardz/agent/x/registry"
)

func init() {
	registry.ListenerRegistry().Register("unix", NewListener)
}

type unixListener struct {
	ln      net.Listener
	logger  logger.Logger
	md      metadata
	options listener.Options
}

func NewListener(opts ...listener.Option) listener.Listener {
	options := listener.Options{}
	for _, opt := range opts {
		opt(&options)
	}
	return &unixListener{
		logger:  options.Logger,
		options: options,
	}
}

func (l *unixListener) Init(md md.Metadata) (err error) {
	if err = l.parseMetadata(md); err != nil {
		return
	}

	ln, err := net.Listen("unix", l.options.Addr)
	if err != nil {
		return
	}

	ln = metrics.WrapListener(l.options.Service, ln)
	ln = stats.WrapListener(ln, l.options.Stats)
	ln = admission.WrapListener(l.options.Admission, ln)
	ln = limiter_wrapper.WrapListener(l.options.Service, ln, l.options.TrafficLimiter)
	ln = climiter.WrapListener(l.options.ConnLimiter, ln)
	l.ln = ln

	return
}

func (l *unixListener) Accept() (conn net.Conn, err error) {
	conn, err = l.ln.Accept()
	if err != nil {
		return
	}

	conn = limiter_wrapper.WrapConn(
		conn,
		l.options.TrafficLimiter,
		conn.RemoteAddr().String(),
		limiter.ScopeOption(limiter.ScopeConn),
		limiter.ServiceOption(l.options.Service),
		limiter.NetworkOption(conn.LocalAddr().Network()),
		limiter.SrcOption(conn.RemoteAddr().String()),
	)

	return
}

func (l *unixListener) Addr() net.Addr {
	return l.ln.Addr()
}

func (l *unixListener) Close() error {
	return l.ln.Close()
}
