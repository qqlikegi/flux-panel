package registry

import (
	"github.com/qqlikegi/forwardz/agent/x/lib/core/listener"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/logger"
)

type NewListener func(opts ...listener.Option) listener.Listener

type listenerRegistry struct {
	registry[NewListener]
}

func (r *listenerRegistry) Register(name string, v NewListener) error {
	if err := r.registry.Register(name, v); err != nil {
		logger.Default().Fatal(err)
	}
	return nil
}
