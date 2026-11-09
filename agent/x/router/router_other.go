//go:build !linux

package router

import (
	"github.com/qqlikegi/forwardz/agent/x/lib/core/router"
)

func (*localRouter) setSysRoutes(routes ...*router.Route) error {
	return nil
}
