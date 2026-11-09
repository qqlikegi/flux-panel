package proto

import (
	context "context"
	"strings"

	grpc "google.golang.org/grpc"
)

type ForwardZTunnelClientX interface {
	TunnelX(ctx context.Context, method string, opts ...grpc.CallOption) (ForwardZTunnel_TunnelClient, error)
}
type forwardZTunnelClientX struct {
	cc grpc.ClientConnInterface
}

func NewForwardZTunnelClientX(cc grpc.ClientConnInterface) ForwardZTunnelClientX {
	return &forwardZTunnelClientX{
		cc: cc,
	}
}

func (c *forwardZTunnelClientX) TunnelX(ctx context.Context, method string, opts ...grpc.CallOption) (ForwardZTunnel_TunnelClient, error) {
	sd := ServerDesc(method)
	method = "/" + sd.ServiceName + "/" + sd.Streams[0].StreamName
	stream, err := c.cc.NewStream(ctx, &sd.Streams[0], method, opts...)
	if err != nil {
		return nil, err
	}
	x := &forwardZTunnelTunnelClient{stream}
	return x, nil
}

func RegisterForwardZTunnelServerX(s grpc.ServiceRegistrar, srv ForwardZTunnelServer, method string) {
	sd := ServerDesc(method)
	s.RegisterService(&sd, srv)
}

func ServerDesc(method string) grpc.ServiceDesc {
	serviceName, streamName := parsingMethod(method)

	return grpc.ServiceDesc{
		ServiceName: serviceName,
		HandlerType: ForwardZTunnel_ServiceDesc.HandlerType,
		Methods:     ForwardZTunnel_ServiceDesc.Methods,
		Streams: []grpc.StreamDesc{
			{
				StreamName:    streamName,
				Handler:       ForwardZTunnel_ServiceDesc.Streams[0].Handler,
				ServerStreams: ForwardZTunnel_ServiceDesc.Streams[0].ServerStreams,
				ClientStreams: ForwardZTunnel_ServiceDesc.Streams[0].ClientStreams,
			},
		},
		Metadata: ForwardZTunnel_ServiceDesc.Metadata,
	}

}

func parsingMethod(method string) (string, string) {
	serviceName := ForwardZTunnel_ServiceDesc.ServiceName
	streamName := ForwardZTunnel_ServiceDesc.Streams[0].StreamName
	v := strings.SplitN(strings.Trim(method, "/"), "/", 2)
	if len(v) == 1 && v[0] != "" {
		serviceName = v[0]
	}
	if len(v) == 2 {
		serviceName = v[0]
		streamName = strings.Replace(v[1], "/", "-", -1)
	}

	return serviceName, streamName
}
