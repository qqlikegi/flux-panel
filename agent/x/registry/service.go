package registry

import (
	"github.com/qqlikegi/forwardz/agent/x/lib/core/service"
)

type serviceRegistry struct {
	registry[service.Service]
}
