package driver

import (
	"github.com/StackPointCloud/csi-packet/pkg/version"
	csi "github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ csi.IdentityServer = &PacketIdentityServer{}

type PacketIdentityServer struct {
	Driver *PacketDriver
}

func NewPacketIdentityServer(driver *PacketDriver) *PacketIdentityServer {
	return &PacketIdentityServer{driver}
}

func (packetIdentity *PacketIdentityServer) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	glog.V(5).Infof("Using default GetPluginInfo")

	if packetIdentity.Driver.name == "" {
		return nil, status.Error(codes.Unavailable, "Driver name not configured")
	}

	return &csi.GetPluginInfoResponse{
		Name:          packetIdentity.Driver.name,
		VendorVersion: version.VERSION,
	}, nil
}

func (packetIdentity *PacketIdentityServer) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	glog.V(5).Infof("Using default GetPluginCapabilities")
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			&csi.PluginCapability{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
		},
	}, nil
}

func (packetIdentity *PacketIdentityServer) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	glog.V(5).Infof("Probe called with args: %#v", req)
	return &csi.ProbeResponse{}, nil
}
