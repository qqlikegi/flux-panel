package tls

import (
	md "github.com/go-gost/core/metadata"
	mdutil "github.com/qqlikegi/forwardz/agent/x/metadata/util"
)

type metadata struct {
	mptcp bool
}

func (l *obfsListener) parseMetadata(md md.Metadata) (err error) {
	l.md.mptcp = mdutil.GetBool(md, "mptcp")

	return
}
