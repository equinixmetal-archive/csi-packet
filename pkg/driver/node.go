package driver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/StackPointCloud/csi-packet/pkg/packet"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/golang/glog"
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

// {
// 	"ips": [
// 	  "10.144.144.144",
// 	  "10.144.145.66"
// 	],
// 	"name": "volume-4b6ed3d8",
// 	"capacity": {
// 	  "size": "100",
// 	  "unit": "gb"
// 	},
// 	"iqn": "iqn.2013-05.com.daterainc:tc:01:sn:b06f15a423fec58b"
// }

// PacketCapacityMetaData exists for parsing json metadata
type PacketCapacityMetaData struct {
	Size string `json:"size"`
	Unit string `json:"unit"`
}

// PacketVolumeMetadata exists for parsing json metadata
type PacketVolumeMetadata struct {
	Name     string                 `json:"name"`
	Ips      []string               `json:"ips"`
	Capacity PacketCapacityMetaData `json:"capacity"`
	Iqn      string                 `json:"iqn"`
}

// get all the metadata, extract only the parsed volume information, select the desired volume
func getPacketVolumeMetadata(volumeName string) (PacketVolumeMetadata, error) {

	empty := PacketVolumeMetadata{}

	res, err := http.Get("https://metadata.packet.net/metadata")
	if err != nil {
		return empty, err
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return empty, err
	}

	allData := map[string]interface{}{}
	err = json.Unmarshal([]byte(body), &allData)
	if err != nil {
		return empty, err
	}

	volumesUnparsed := allData["volumes"]
	volumesAsJSON, err := json.Marshal(volumesUnparsed)
	if err != nil {
		return empty, err
	}

	volumes := []PacketVolumeMetadata{}
	err = json.Unmarshal(volumesAsJSON, &volumes)
	if err != nil {
		return empty, err
	}

	if err != nil {
		return empty, err
	}

	var volumeMetaData = PacketVolumeMetadata{}
	for _, vdata := range volumes {
		if vdata.Name == volumeName {
			volumeMetaData = vdata
			break
		}
	}
	if volumeMetaData.Name == "" {
		return empty, fmt.Errorf("volume %s not found in metadata", volumeName)
	}

	return volumeMetaData, nil
}

// NodeStageVolume ~ iscisadmin, multipath, format
func (nodeServer *PacketNodeServer) NodeStageVolume(ctx context.Context, in *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {

	// validate arguments
	// this is the full packet UUID, not the abbreviated name...
	// volumeID := in.VolumeId
	volumeName := in.PublishInfo["VolumeName"]

	volumeMetaData, err := getPacketVolumeMetadata(volumeName)
	if err != nil {
		glog.V(5).Infof("NodeStageVolume: %v", err)
		return nil, err
	}

	if len(volumeMetaData.Ips) == 0 {
		return nil, fmt.Errorf("volume %s has no portals", volumeName)
	}

	if in.GetVolumeCapability() == nil {
		return nil, fmt.Errorf("VolumeCapability unspecified for NodeStageVolume")
	}
	mnt := in.VolumeCapability.GetMount()
	// options := mnt.MountFlags

	if mnt.FsType != "" {
		if mnt.FsType != "ext4" {
			return nil, status.Error(codes.Unimplemented, fmt.Sprintf("fs type %s not supported", mnt.FsType))
		}
	}

	// fmt.Printf("%s %s %s\n", volumeID, targetPath, fsType)

	// discover and log in to iscsiadmin
	for _, ip := range volumeMetaData.Ips {
		err = iscsiadminDiscover(ip) // iscsiadm --mode discovery --type sendtargets --portal 10.144.144.226 --discover
		if err != nil {
			glog.V(5).Infof("NodeStageVolume: %v", err)
			return nil, err
		}
		err = iscsiadminLogin(ip, volumeMetaData.Iqn)
		if err != nil {
			glog.V(5).Infof("NodeStageVolume: %v", err)
			return nil, err
		}
	}

	// configure multimap
	devicePath, err := getDevice(volumeMetaData.Ips[0], volumeMetaData.Iqn)
	if err != nil {
		glog.V(5).Infof("NodeStageVolume: %v", err)
		return nil, err
	}
	scsiID, err := getScsiID(devicePath)
	if err != nil {
		glog.V(5).Infof("NodeStageVolume: %v", err)
		return nil, err
	}
	bindings, discards, err := readBindings()
	if err != nil {
		glog.V(5).Infof("NodeStageVolume: %v", err)
		return nil, err
	}
	bindings[volumeName] = scsiID
	err = writeBindings(bindings)
	if err != nil {
		glog.V(5).Infof("NodeStageVolume: %v", err)
		return nil, err
	}
	for mappingName, _ := range discards {
		multipath("-f", mappingName)
	}
	multipath(volumeName)

	check, err := multipath("-ll", devicePath)
	glog.V(5).Infof("multipath check for %s: %s", devicePath, check)
	if check == "" {
		glog.V(5).Infof("NodeStageVolume: check is empty")
	}

	blockInfo, err := getMappedDevice(volumeName)
	if err != nil {
		glog.V(5).Infof("NodeStageVolume: %v", err)
		return nil, err
	}
	if blockInfo.FsType == "" {
		err = formatMappedDevice(volumeName)
		if err != nil {
			glog.V(5).Infof("NodeStageVolume: %v", err)
			return nil, err
		}
	}

	err = mountMappedDevice(volumeName, in.StagingTargetPath)
	if err != nil {
		glog.V(5).Infof("NodeStageVolume: %v", err)
		return nil, err
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume ~ iscisadmin, multipath
func (nodeServer *PacketNodeServer) NodeUnstageVolume(ctx context.Context, in *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	volumeID := in.VolumeId
	volumeName := packet.VolumeIDToName(volumeID)

	err := unmountFs(in.StagingTargetPath)
	if err != nil {
		return nil, err
	}

	volumeMetaData, err := getPacketVolumeMetadata(volumeName)
	if err != nil {
		return nil, err
	}

	if len(volumeMetaData.Ips) == 0 {
		return nil, fmt.Errorf("volume %s not has no portals", volumeName)
	}

	// remove multipath
	bindings, discards, err := readBindings()
	if err != nil {
		return nil, err
	}
	delete(bindings, volumeName)
	err = writeBindings(bindings)
	if err != nil {
		return nil, err
	}
	for mappingName, _ := range discards {
		multipath("-f", mappingName)
	}
	multipath("-f", volumeName)

	for _, ip := range volumeMetaData.Ips {
		err = iscsiadminLogout(ip, volumeMetaData.Iqn)
		if err != nil {
			return nil, err
		}
	}

	response := &csi.NodeUnstageVolumeResponse{}
	return response, nil
}

// NodePublishVolume ~ mount
func (nodeServer *PacketNodeServer) NodePublishVolume(ctx context.Context, in *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {

	// volumeID := in.VolumeId
	// volumeName := in.PublishInfo["VolumeName"]

	err := bindmountFs(in.GetStagingTargetPath(), in.GetTargetPath())
	if err != nil {
		return nil, err
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume ~ unmount
func (nodeServer *PacketNodeServer) NodeUnpublishVolume(ctx context.Context, in *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	err := unmountFs(in.GetTargetPath())
	if err != nil {
		return nil, err
	}
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetId
func (nodeServer *PacketNodeServer) NodeGetId(ctx context.Context, in *csi.NodeGetIdRequest) (*csi.NodeGetIdResponse, error) {
	glog.V(2).Infof("queried for node id, %s", nodeServer.Driver.nodeID)
	return &csi.NodeGetIdResponse{
		NodeId: nodeServer.Driver.nodeID,
	}, nil
}

// NodeGetCapabilities
func (nodeServer *PacketNodeServer) NodeGetCapabilities(ctx context.Context, in *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {

	// define
	ns := []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
	}
	// transform
	var nsc []*csi.NodeServiceCapability
	for _, n := range ns {
		nsc = append(nsc, &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: n,
				},
			},
		})
	}

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nsc,
	}, nil
}
