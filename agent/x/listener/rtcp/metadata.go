package rtcp

import (
	mdata "github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
)

type metadata struct{}

func (l *rtcpListener) parseMetadata(md mdata.Metadata) (err error) {
	return
}
