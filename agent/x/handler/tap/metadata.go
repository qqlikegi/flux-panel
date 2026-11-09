package tap

import (
	"math"

	mdata "github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
	mdutil "github.com/qqlikegi/forwardz/agent/x/metadata/util"
)

const (
	MaxMessageSize = math.MaxUint16
)

type metadata struct {
	key string
}

func (h *tapHandler) parseMetadata(md mdata.Metadata) (err error) {
	const (
		key = "key"
	)

	h.md.key = mdutil.GetString(md, key)
	return
}
