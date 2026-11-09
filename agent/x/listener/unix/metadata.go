package unix

import (
	md "github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
)

type metadata struct{}

func (l *unixListener) parseMetadata(md md.Metadata) (err error) {
	return
}
