package tls

import (
	mdata "github.com/go-gost/core/metadata"
	mdutil "github.com/qqlikegi/forwardz/agent/x/metadata/util"
)

type metadata struct {
	mptcp bool
}

func (l *tlsListener) parseMetadata(md mdata.Metadata) (err error) {
	l.md.mptcp = mdutil.GetBool(md, "mptcp")

	return
}
