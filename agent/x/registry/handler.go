package registry

import (
	"github.com/qqlikegi/forwardz/agent/x/lib/core/handler"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/logger"
)

type NewHandler func(opts ...handler.Option) handler.Handler

type handlerRegistry struct {
	registry[NewHandler]
}

func (r *handlerRegistry) Register(name string, v NewHandler) error {
	if err := r.registry.Register(name, v); err != nil {
		logger.Default().Fatal(err)
	}
	return nil
}
