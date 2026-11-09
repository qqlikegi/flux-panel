package chain

import (
	"github.com/qqlikegi/forwardz/agent/x/lib/core/chain"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/hop"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/logger"
	"github.com/qqlikegi/forwardz/agent/x/lib/core/metadata"
	xchain "github.com/qqlikegi/forwardz/agent/x/chain"
	"github.com/qqlikegi/forwardz/agent/x/config"
	hop_parser "github.com/qqlikegi/forwardz/agent/x/config/parsing/hop"
	mdx "github.com/qqlikegi/forwardz/agent/x/metadata"
	"github.com/qqlikegi/forwardz/agent/x/registry"
)

func ParseChain(cfg *config.ChainConfig, log logger.Logger) (chain.Chainer, error) {
	if cfg == nil {
		return nil, nil
	}

	chainLogger := log.WithFields(map[string]any{
		"kind":  "chain",
		"chain": cfg.Name,
	})

	var md metadata.Metadata
	if cfg.Metadata != nil {
		md = mdx.NewMetadata(cfg.Metadata)
	}

	c := xchain.NewChain(cfg.Name,
		xchain.MetadataChainOption(md),
		xchain.LoggerChainOption(chainLogger),
	)

	for _, ch := range cfg.Hops {
		var hop hop.Hop
		var err error

		if ch.Nodes != nil || ch.Plugin != nil {
			if hop, err = hop_parser.ParseHop(ch, log); err != nil {
				return nil, err
			}
		} else {
			hop = registry.HopRegistry().Get(ch.Name)
		}
		if hop != nil {
			c.AddHop(hop)
		}
	}

	return c, nil
}
