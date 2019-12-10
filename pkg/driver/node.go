package driver

import (
	"github.com/packethost/csi-packet/pkg/packet"
	log "github.com/sirupsen/logrus"

	"github.com/container-storage-interface/spec/lib/go/csi"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ csi.NodeServer = &PacketNodeServer{}

// PacketNodeServer represents a packet node
type PacketNodeServer struct {
	Driver         *PacketDriver
	MetadataDriver *packet.MetadataDriver
	Initialized    bool
	initiator      string
}

// NewPacketNodeServer create a new PacketNodeServer
func NewPacketNodeServer(driver *PacketDriver, metadata *packet.MetadataDriver) (*PacketNodeServer, error) {
	// we do NOT initialize here, since NewPacketNodeServer is called in all cases of this program
	//  even on a controller
	//  we wait until our first legitimate call for a Node*() func
	return &PacketNodeServer{
		Driver:         driver,
		MetadataDriver: metadata,
		Initialized:    false,
	}, nil
}

// NodeStageVolume ~ iscisadmin, multipath, format
func (nodeServer *PacketNodeServer) NodeStageVolume(ctx context.Context, in *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	nodeServer.Driver.Logger.Info("NodeStageVolume called")
	// validate arguments
	// this is the abbreviated name...
	volumeName := in.PublishContext["VolumeName"]
	if volumeName == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeName unspecified for NodeStageVolume")
	}

	// do we know our initiator?
	if nodeServer.initiator == "" {
		initiatorName, err := nodeServer.MetadataDriver.GetInitiator()
		if err != nil {
			nodeServer.Driver.Logger.Errorf("NodeGetInfo: metadata error %v", err)
			return nil, status.Errorf(codes.Unknown, "metadata error, %s", err.Error())
		}
		nodeServer.initiator = initiatorName
	}
	volumeMetaData, err := nodeServer.MetadataDriver.GetVolumeMetadata(volumeName)
	if err != nil {
		nodeServer.Driver.Logger.Errorf("NodeStageVolume: %v", err)
		return nil, status.Errorf(codes.Unknown, "metadata error, %s", err.Error())
	}

	if len(volumeMetaData.IPs) == 0 {
		return nil, status.Errorf(codes.Unknown, "volume %s has no portals", volumeName)
	}

	if in.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "VolumeCapability unspecified for NodeStageVolume")
	}
	mnt := in.VolumeCapability.GetMount()
	// options := mnt.MountFlags

	if mnt.FsType != "" {
		if mnt.FsType != "ext4" {
			return nil, status.Errorf(codes.InvalidArgument, "fs type %s not supported", mnt.FsType)
		}
	}

	logger := nodeServer.Driver.Logger.WithFields(log.Fields{
		"volume_id":           in.VolumeId,
		"volume_name":         volumeName,
		"staging_target_path": in.StagingTargetPath,
		"fsType":              mnt.FsType,
		"method":              "NodeStageVolume",
	})

	// discover and log in to iscsiadmin
	for _, ip := range volumeMetaData.IPs {
		err = nodeServer.Driver.Attacher.Discover(ip.String(), nodeServer.initiator) // iscsiadm --mode discovery --type sendtargets --portal 10.144.144.226 --discover
		if err != nil {
			logger.Infof("iscsiadmin discover error, %+v", err)
			return nil, status.Errorf(codes.Unknown, "iscsiadmin discover error, %+v", err)
		}
		err = nodeServer.Driver.Attacher.Login(ip.String(), volumeMetaData.IQN)
		if err != nil {
			logger.Infof("iscsiadmin login error, %+v", err)
			return nil, status.Errorf(codes.Unknown, "iscsiadmin login error, %+v", err)
		}
	}

	// configure multimap
	devicePath, err := nodeServer.Driver.Attacher.GetDevice(volumeMetaData.IPs[0].String(), volumeMetaData.IQN)
	if err != nil {
		logger.Infof("devicePath error, %+v", err)
		return nil, status.Errorf(codes.Unknown, "devicePath error, %+v", err)
	}
	scsiID, err := nodeServer.Driver.Attacher.GetScsiID(devicePath)
	if err != nil {
		logger.Infof("scsiID error, path %s, %+v", devicePath, err)
		return nil, status.Errorf(codes.Unknown, "scsiIDerror, %+v", err)
	}
	bindings, discards, err := nodeServer.Driver.Attacher.MultipathReadBindings()
	if err != nil {
		logger.Infof("readBindings error, %+v", err)
		return nil, status.Errorf(codes.Unknown, "readBindings error, %+v", err)
	}
	bindings[volumeName] = scsiID
	err = nodeServer.Driver.Attacher.MultipathWriteBindings(bindings)
	if err != nil {
		logger.Infof("writeBindings error, %+v", err)
		return nil, status.Errorf(codes.Unknown, "writeBindings error, %+v", err)
	}
	for mappingName := range discards {
		multipath("-f", mappingName)
	}
	// for some reason, you have to do it twice for it to work
	multipath(volumeName)
	multipath(volumeName)

	check, err := multipath("-ll", devicePath)
	logger.Infof("multipath check for %s: %s", devicePath, check)
	if check == "" {
		logger.Infof("empty multipath check for %s", devicePath)
	}

	blockInfo, err := nodeServer.Driver.Mounter.GetMappedDevice(volumeName)
	if err != nil {
		logger.Infof("getMappedDevice error, %+v", err)
		return nil, status.Errorf(codes.Unknown, "getMappedDevice error, %+v", err)
	}
	if blockInfo.FsType == "" {
		err = nodeServer.Driver.Mounter.FormatMappedDevice(volumeName)
		if err != nil {
			logger.Infof("formatMappedDevice error, %+v", err)
			return nil, status.Errorf(codes.Unknown, "formatMappedDevice error, %+v", err)
		}
	}

	logger.Info("mounting mapped device")
	err = nodeServer.Driver.Mounter.MountMappedDevice(volumeName, in.StagingTargetPath)
	if err != nil {
		logger.Infof("mountMappedDevice error, %v", err)
		return nil, status.Errorf(codes.Unknown, "mountMappedDevice error, %+v", err)
	}

	logger.Infof("NodeStageVolume complete")
	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume ~ iscisadmin, multipath
func (nodeServer *PacketNodeServer) NodeUnstageVolume(ctx context.Context, in *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {

	nodeServer.Driver.Logger.Info("NodeUnstageVolume called")

	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId unspecified for NodeUnstageVolume")
	}
	if in.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "StagingTargetPath unspecified for NodeUnstageVolume")
	}

	volumeID := in.VolumeId
	volumeName := packet.VolumeIDToName(volumeID)

	logger := nodeServer.Driver.Logger.WithFields(log.Fields{
		"volume_id":           in.VolumeId,
		"volume_name":         volumeName,
		"staging_target_path": in.StagingTargetPath,
		"method":              "NodeUnstageVolume",
	})

	err := nodeServer.Driver.Mounter.Unmount(in.StagingTargetPath)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unmounting error, %v", err)
	}
	logger.Infof("Unmounted staging target")

	volumeMetaData, err := nodeServer.MetadataDriver.GetVolumeMetadata(volumeName)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "metadata access error, %v ", err)
	}

	if len(volumeMetaData.IPs) == 0 {
		return nil, status.Errorf(codes.Unknown, "volume %s has no portals", volumeName)
	}

	// remove multipath
	bindings, discards, err := nodeServer.Driver.Attacher.MultipathReadBindings()
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "multipath error, %v", err)
	}
	delete(bindings, volumeName)
	err = nodeServer.Driver.Attacher.MultipathWriteBindings(bindings)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "multipath error, %v", err)
	}
	logger.Info("multipath flush")
	for mappingName := range discards {
		multipath("-f", mappingName)
	}
	multipath("-f", volumeName)

	for _, ip := range volumeMetaData.IPs {
		logger.WithFields(log.Fields{"ip": ip, "iqn": volumeMetaData.IQN}).Info("iscsiadmin logout")
		err = nodeServer.Driver.Attacher.Logout(ip.String(), volumeMetaData.IQN)
		if err != nil {
			return nil, status.Errorf(codes.Unknown, "iscsiadminLogout error, %v", err)
		}
	}

	logger.Info("NodeUnstageVolume complete")
	response := &csi.NodeUnstageVolumeResponse{}
	return response, nil
}

// NodePublishVolume ~ mount
func (nodeServer *PacketNodeServer) NodePublishVolume(ctx context.Context, in *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {

	nodeServer.Driver.Logger.Info("NodePublishVolume called")

	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId unspecified for NodeStageVolume")
	}
	if in.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "TargetPath unspecified for NodeStageVolume")
	}
	if in.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "StagingTargetPath unspecified for NodeStageVolume")
	}

	logger := nodeServer.Driver.Logger.WithFields(log.Fields{
		"volume_id":           in.VolumeId,
		"target_path":         in.TargetPath,
		"staging_target_path": in.StagingTargetPath,
		"method":              "NodePublishVolume",
	})

	err := nodeServer.Driver.Mounter.Bindmount(in.GetStagingTargetPath(), in.GetTargetPath())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "bind mount error, %+v", err)
	}
	logger.Info("bind mount complete")
	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume ~ unmount
func (nodeServer *PacketNodeServer) NodeUnpublishVolume(ctx context.Context, in *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {

	nodeServer.Driver.Logger.Info("NodeUnpublishVolume called")

	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId unspecified for NodeUnpublishVolume")
	}
	if in.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "TargetPath unspecified for NodeUnpublishVolume")
	}

	logger := nodeServer.Driver.Logger.WithFields(log.Fields{
		"volume_id":   in.VolumeId,
		"target_path": in.TargetPath,
		"method":      "NodePublishVolume",
	})

	err := nodeServer.Driver.Mounter.Unmount(in.GetTargetPath())
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "unmount error, %+v", err)
	}
	logger.Info("unmount complete")

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetVolumeStats gets the usage stats of the volume
func (nodeServer *PacketNodeServer) NodeGetVolumeStats(ctx context.Context, in *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	nodeServer.Driver.Logger.Info("NodeGetVolumeStats called")
	// TODO: get info from packet about the usage of this volume
	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{},
	}, nil
}

// NodeGetInfo get info for a given node
func (nodeServer *PacketNodeServer) NodeGetInfo(context.Context, *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	nodeServer.Driver.Logger.Info("NodeGetInfo called")
	// initialize
	if !nodeServer.Initialized {
		initiatorName, err := nodeServer.MetadataDriver.GetInitiator()
		if err != nil {
			nodeServer.Driver.Logger.Errorf("NodeGetInfo: metadata error %v", err)
			return nil, status.Errorf(codes.Unknown, "metadata error, %s", err.Error())
		}
		err = nodeServer.Driver.Initializer.NodeInit(initiatorName)
		if err != nil {
			nodeServer.Driver.Logger.Errorf("NodeGetInfo: NodeInit error %v", err)
			return nil, status.Errorf(codes.Unknown, "NodeInit error, %s", err.Error())
		}
		nodeServer.Initialized = true
	}
	return &csi.NodeGetInfoResponse{
		NodeId: nodeServer.Driver.nodeID,
		// MaxVolumesPerNode: 0,
		// AccessibleTopology: nil,
	}, nil
}

// NodeGetCapabilities get capabilities of a given node
func (nodeServer *PacketNodeServer) NodeGetCapabilities(ctx context.Context, in *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {

	nodeServer.Driver.Logger.Info("NodeGetCapabilities called")
	// define
	nsCapabilitySet := []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
	}
	// transform
	var nsc []*csi.NodeServiceCapability
	for _, nscap := range nsCapabilitySet {
		nsc = append(nsc, &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: nscap,
				},
			},
		})
	}

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nsc,
	}, nil
}

// NodeExpandVolume expand a volume
func (nodeServer *PacketNodeServer) NodeExpandVolume(context.Context, *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
