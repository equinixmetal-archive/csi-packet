package driver

import (
	"github.com/packethost/csi-packet/pkg/packet"
	log "github.com/sirupsen/logrus"
)

var (
	server NonBlockingGRPCServer
)

// PacketDriver driver for packet cloud
type PacketDriver struct {
	name     string
	nodeID   string
	endpoint string
	config   packet.Config
	Logger   *log.Entry
	Attacher Attacher
	Mounter  Mounter
}

// NewPacketDriver create a new PacketDriver
func NewPacketDriver(endpoint, nodeID string, config packet.Config) (*PacketDriver, error) {
	return &PacketDriver{
		// name https://github.com/container-storage-interface/spec/blob/master/spec.md#getplugininfo
		name:     "net.packet.csi", // this could be configurable, but must match a plugin directory name for kubelet to use
		nodeID:   nodeID,
		endpoint: endpoint,
		config:   config,
		Logger:   log.WithFields(log.Fields{"node": nodeID, "endpoint": endpoint}),
		// default attacher and mounter
		Attacher: &AttacherImpl{},
		Mounter:  &MounterImpl{},
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
	node := NewPacketNodeServer(d, &metadataDriver)
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
