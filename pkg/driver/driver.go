package driver

import (
	"encoding/json"
	"io/ioutil"

	"github.com/StackPointCloud/csi-packet/pkg/packet"
	log "github.com/sirupsen/logrus"
)

type PacketDriver struct {
	name     string
	nodeID   string
	endpoint string
	config   packet.Config
	Logger   *log.Entry
}

func NewPacketDriver(endpoint, nodeID, configurationPath string) (*PacketDriver, error) {

	var config packet.Config
	if configurationPath != "" {
		configBytes, err := ioutil.ReadFile(configurationPath)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(configBytes, &config)
		if err != nil {
			return nil, err
		}
	}

	return &PacketDriver{
		// name https://github.com/container-storage-interface/spec/blob/master/spec.md#getplugininfo
		name:     "com.packet.csi", // this could be configurable, but must match a plugin directory name for kubelet to use
		nodeID:   nodeID,
		endpoint: endpoint,
		config:   config,
		Logger:   log.WithFields(log.Fields{"node": nodeID, "endpoint": endpoint}),
	}, nil
}

func (d *PacketDriver) Run() {

	s := NewNonBlockingGRPCServer()
	identity := NewPacketIdentityServer(d)
	var controller *PacketControllerServer
	if d.config.AuthToken != "" {
		p, _ := packet.NewPacketProvider(d.config)
		controller = NewPacketControllerServer(p)
	}
	node := NewPacketNodeServer(d)
	d.Logger.Info("Starting server")
	s.Start(d.endpoint,
		identity,
		controller,
		node)
	s.Wait()
}
