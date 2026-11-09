package quic

import (
	"time"

	mdata "github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
	mdutil "github.com/qqlikegi/forwardz/agent/x/metadata/util"
)

type metadata struct {
	keepAlivePeriod  time.Duration
	maxIdleTimeout   time.Duration
	handshakeTimeout time.Duration
}

func (d *icmpDialer) parseMetadata(md mdata.Metadata) (err error) {
	if mdutil.GetBool(md, "keepalive") {
		d.md.keepAlivePeriod = mdutil.GetDuration(md, "ttl")
		if d.md.keepAlivePeriod <= 0 {
			d.md.keepAlivePeriod = 10 * time.Second
		}
	}
	d.md.handshakeTimeout = mdutil.GetDuration(md, "handshakeTimeout")
	d.md.maxIdleTimeout = mdutil.GetDuration(md, "maxIdleTimeout")

	return
}
