package driver

import (
	"fmt"

	"github.com/packethost/csi-packet/pkg/packet"
	"github.com/sirupsen/logrus"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ csi.NodeServer = &PacketNodeServer{}

type PacketNodeServer struct {
	Driver *PacketDriver
}

func NewPacketNodeServer(driver *PacketDriver) *PacketNodeServer {
	return &PacketNodeServer{
		Driver: driver,
	}
}

// NodeStageVolume ~ iscisadmin, multipath, format
func (nodeServer *PacketNodeServer) NodeStageVolume(ctx context.Context, in *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {

	nodeServer.Driver.Logger.Info("NodeStageVolume called")
	// validate arguments
	// this is the full packet UUID, not the abbreviated name...
	// volumeID := in.VolumeId
	volumeName := in.PublishInfo["VolumeName"]
	if volumeName == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeName unspecified for NodeStageVolume"))
	}

	volumeMetaData, err := packet.GetPacketVolumeMetadata(volumeName)
	if err != nil {
		nodeServer.Driver.Logger.Infof("NodeStageVolume: %v", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("metadata error, %s", err.Error()))
	}

	if len(volumeMetaData.Ips) == 0 {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("volume %s has no portals", volumeName))
	}

	if in.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeCapability unspecified for NodeStageVolume"))
	}
	mnt := in.VolumeCapability.GetMount()
	// options := mnt.MountFlags

	if mnt.FsType != "" {
		if mnt.FsType != "ext4" {
			return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("fs type %s not supported", mnt.FsType))
		}
	}

	logger := nodeServer.Driver.Logger.WithFields(logrus.Fields{
		"volume_id":           in.VolumeId,
		"volume_name":         volumeName,
		"staging_target_path": in.StagingTargetPath,
		"fsType":              mnt.FsType,
		"method":              "NodeStageVolume",
	})

	// discover and log in to iscsiadmin
	for _, ip := range volumeMetaData.Ips {
		err = iscsiadminDiscover(ip) // iscsiadm --mode discovery --type sendtargets --portal 10.144.144.226 --discover
		if err != nil {
			logger.Infof("isciadmin discover error, %+v", err)
			return nil, status.Error(codes.Unknown, fmt.Sprintf("isciadmin discover error, %+v", err))
		}
		err = iscsiadminLogin(ip, volumeMetaData.Iqn)
		if err != nil {
			logger.Infof("isciadmin login error, %+v", err)
			return nil, status.Error(codes.Unknown, fmt.Sprintf("isciadmin login error, %+v", err))
		}
	}

	// configure multimap
	devicePath, err := getDevice(volumeMetaData.Ips[0], volumeMetaData.Iqn)
	if err != nil {
		logger.Infof("devicePath error, %+v", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("devicePath error, %+v", err))
	}
	scsiID, err := getScsiID(devicePath)
	if err != nil {
		logger.Infof("scsiID error, path %s, %+v", devicePath, err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("scsiIDerror, %+v", err))
	}
	bindings, discards, err := readBindings()
	if err != nil {
		logger.Infof("readBindings error, %+v", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("readBindings error, %+v", err))
	}
	bindings[volumeName] = scsiID
	err = writeBindings(bindings)
	if err != nil {
		logger.Infof("writeBindings error, %+v", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("writeBindings error, %+v", err))
	}
	for mappingName, _ := range discards {
		multipath("-f", mappingName)
	}
	multipath(volumeName)

	check, err := multipath("-ll", devicePath)
	logger.Infof("multipath check for %s: %s", devicePath, check)
	if check == "" {
		logger.Infof("empty multipath check for %s", devicePath)
	}

	blockInfo, err := getMappedDevice(volumeName)
	if err != nil {
		logger.Infof("getMappedDevice error, %+v", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("getMappedDevice error, %+v", err))
	}
	if blockInfo.FsType == "" {
		err = formatMappedDevice(volumeName)
		if err != nil {
			logger.Infof("formatMappedDevice error, %+v", err)
			return nil, status.Error(codes.Unknown, fmt.Sprintf("formatMappedDevice error, %+v", err))
		}
	}

	logger.Info("mounting mapped device")
	err = mountMappedDevice(volumeName, in.StagingTargetPath)
	if err != nil {
		logger.Infof("mountMappedDevice error, %v", err)
		return nil, status.Error(codes.Unknown, fmt.Sprintf("mountMappedDevice error, %+v", err))
	}

	logger.Infof("NodeStageVolume complete")
	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume ~ iscisadmin, multipath
func (nodeServer *PacketNodeServer) NodeUnstageVolume(ctx context.Context, in *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {

	nodeServer.Driver.Logger.Info("NodeUnstageVolume called")

	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeId unspecified for NodeUnpublishVolume"))
	}
	if in.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("StagingTargetPath unspecified for NodeUnpublishVolume"))
	}

	volumeID := in.VolumeId
	volumeName := packet.VolumeIDToName(volumeID)

	logger := nodeServer.Driver.Logger.WithFields(logrus.Fields{
		"volume_id":           in.VolumeId,
		"volume_name":         volumeName,
		"staging_target_path": in.StagingTargetPath,
		"method":              "NodeUnstageVolume",
	})
	// failureResponse := &csi.NodeUnstageVolumeResponse{} but this is empty ...

	err := unmountFs(in.StagingTargetPath)
	if err != nil {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("unmounting error, %s", err.Error()))
	}
	logger.Infof("Unmounted staging target")

	volumeMetaData, err := packet.GetPacketVolumeMetadata(volumeName)
	if err != nil {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("metadata access error, %s ", err.Error()))
	}

	if len(volumeMetaData.Ips) == 0 {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("volume %s not has no portals", volumeName))
	}

	// remove multipath
	bindings, discards, err := readBindings()
	if err != nil {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("multipath error, %s", err.Error()))
	}
	delete(bindings, volumeName)
	err = writeBindings(bindings)
	if err != nil {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("multipath error, %s", err.Error()))
	}
	logger.Info("multipath flush")
	for mappingName, _ := range discards {
		multipath("-f", mappingName)
	}
	multipath("-f", volumeName)

	for _, ip := range volumeMetaData.Ips {
		logger.WithFields(logrus.Fields{"ip": ip, "iqn": volumeMetaData.Iqn}).Info("iscsiadmin logout")
		err = iscsiadminLogout(ip, volumeMetaData.Iqn)
		if err != nil {
			return nil, status.Error(codes.Unknown, fmt.Sprintf("iscsiadminLogout error, %s", err.Error()))
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
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeId unspecified for NodeStageVolume"))
	}
	if in.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("TargetPath unspecified for NodeStageVolume"))
	}
	if in.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("StagingTargetPath unspecified for NodeStageVolume"))
	}

	logger := nodeServer.Driver.Logger.WithFields(logrus.Fields{
		"volume_id":           in.VolumeId,
		"target_path":         in.TargetPath,
		"staging_target_path": in.StagingTargetPath,
		"method":              "NodePublishVolume",
	})

	err := bindmountFs(in.GetStagingTargetPath(), in.GetTargetPath())
	if err != nil {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("bind mount error, %+v", err))
	}
	logger.Info("bind mount complete")
	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume ~ unmount
func (nodeServer *PacketNodeServer) NodeUnpublishVolume(ctx context.Context, in *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {

	nodeServer.Driver.Logger.Info("NodeUnpublishVolume called")

	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeId unspecified for NodeUnpublishVolume"))
	}
	if in.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("TargetPath unspecified for NodeUnpublishVolume"))
	}

	logger := nodeServer.Driver.Logger.WithFields(logrus.Fields{
		"volume_id":   in.VolumeId,
		"target_path": in.TargetPath,
		"method":      "NodePublishVolume",
	})

	err := unmountFs(in.GetTargetPath())
	if err != nil {
		return nil, status.Error(codes.Unknown, fmt.Sprintf("unmount error, %+v", err))
	}
	logger.Info("unmount complete")

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetId
func (nodeServer *PacketNodeServer) NodeGetId(ctx context.Context, in *csi.NodeGetIdRequest) (*csi.NodeGetIdResponse, error) {
	nodeServer.Driver.Logger.Info("NodeGetId called")
	return &csi.NodeGetIdResponse{
		NodeId: nodeServer.Driver.nodeID,
	}, nil
}

// NodeGetInfo
func (nodeServer *PacketNodeServer) NodeGetInfo(context.Context, *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	nodeServer.Driver.Logger.Info("NodeGetInfo called")
	return &csi.NodeGetInfoResponse{
		NodeId: nodeServer.Driver.nodeID,
		// MaxVolumesPerNode: 0,
		// AccessibleTopology: nil,
	}, nil
}

// NodeGetCapabilities
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
