package direct

import (
	"strings"

	mdata "github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
	mdutil "github.com/qqlikegi/forwardz/agent/x/metadata/util"
)

type metadata struct {
	action string
}

func (c *directConnector) parseMetadata(md mdata.Metadata) (err error) {
	c.md.action = strings.ToLower(mdutil.GetString(md, "action"))
	return
}
