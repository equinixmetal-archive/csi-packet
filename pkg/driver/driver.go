package driver

import (
	"fmt"

	"github.com/packethost/csi-packet/pkg/packet"
	log "github.com/sirupsen/logrus"
)

const (
	DriverName = "csi.packet.net"
)

var (
	server NonBlockingGRPCServer
)

// PacketDriver driver for packet cloud
type PacketDriver struct {
	name        string
	nodeID      string
	endpoint    string
	config      packet.Config
	Logger      *log.Entry
	Attacher    Attacher
	Mounter     Mounter
	Initializer Initializer
}

// NewPacketDriver create a new PacketDriver
func NewPacketDriver(endpoint, nodeID string, config packet.Config) (*PacketDriver, error) {
	// if the nodeID was not specified, we retrieve it from metadata
	var err error
	nid := nodeID
	if nid == "" {
		md := packet.MetadataDriver{BaseURL: config.MetadataURL}
		nid, err = md.GetNodeID()
		if err != nil {
			return nil, fmt.Errorf("unable to get node ID from metadata: %v", err)
		}
	}
	return &PacketDriver{
		// name https://github.com/container-storage-interface/spec/blob/master/spec.md#getplugininfo
		name:     DriverName, // this could be configurable, but must match a plugin directory name for kubelet to use
		nodeID:   nid,
		endpoint: endpoint,
		config:   config,
		Logger:   log.WithFields(log.Fields{"node": nid, "endpoint": endpoint}),
		// default attacher and mounter
		Attacher:    &AttacherImpl{},
		Mounter:     &MounterImpl{},
		Initializer: &InitializerImpl{},
	}, nil
}

// Run execute
func (d *PacketDriver) Run() {
	server = NewNonBlockingGRPCServer()
	identity := NewPacketIdentityServer(d)
	metadataDriver := packet.MetadataDriver{BaseURL: d.config.MetadataURL}
	var controller *PacketControllerServer
	if d.config.AuthToken != "" {
		p, err := packet.NewPacketProvider(d.config, metadataDriver)
		if err != nil {
			d.Logger.Fatalf("Unable to create controller %+v", err)
		}
		controller = NewPacketControllerServer(p)
	}
	node, err := NewPacketNodeServer(d, &metadataDriver)
	if err != nil {
		d.Logger.Fatalf("Unable to create node server %+v", err)
	}

	d.Logger.Info("Starting server")
	server.Start(d.endpoint,
		identity,
		controller,
		node)
	server.Wait()
}

// Stop
func (d *PacketDriver) Stop() {
	server.Stop()
}
