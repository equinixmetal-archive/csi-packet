package driver

import (
	"fmt"

	"github.com/packethost/packngo"
	"github.com/pkg/errors"

	"net/http"

	csi "github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/packethost/csi-packet/pkg/packet"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ csi.ControllerServer = &PacketControllerServer{}

type PacketControllerServer struct {
	Provider packet.VolumeProvider
}

func NewPacketControllerServer(provider packet.VolumeProvider) *PacketControllerServer {
	return &PacketControllerServer{
		Provider: provider,
	}
}

func getSizeRequest(capacityRange *csi.CapacityRange) int {
	// size request:
	//   limit if specified
	//   required otherwise
	//   within restrictions of max, min
	//   default otherwise
	var sizeRequestGB int
	if capacityRange == nil {
		sizeRequestGB = packet.DefaultVolumeSizeGb
	} else {
		maxBytes := capacityRange.GetLimitBytes()
		if maxBytes != 0 {
			sizeRequestGB = int(maxBytes / packet.GB)

		} else {
			minBytes := capacityRange.GetRequiredBytes()
			if minBytes != 0 {
				sizeRequestGB = int(minBytes / packet.GB)

			}
		}
	}
	if sizeRequestGB > packet.MaxVolumeSizeGb {
		sizeRequestGB = packet.MaxVolumeSizeGb
	}
	if sizeRequestGB < packet.MinVolumeSizeGb {
		sizeRequestGB = packet.MinVolumeSizeGb
	}
	return sizeRequestGB
}

func getPlanID(parameters map[string]string) string {

	var planID string

	volumePlanRequest := parameters["plan"]
	switch volumePlanRequest {
	case packet.VolumePlanPerformance:
		planID = packet.VolumePlanPerformanceID
	case packet.VolumePlanStandard:
		planID = packet.VolumePlanStandardID
	default:
		planID = packet.VolumePlanStandardID

	}
	return planID
}

func (controller *PacketControllerServer) CreateVolume(ctx context.Context, in *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {

	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("controller not configured"))
	}
	logger := log.WithFields(log.Fields{"volume_name": in.Name})
	logger.Info("CreateVolume called")

	if in.Name == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("Name unspecified for CreateVolume"))
	}
	if in.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeCapabilities unspecified for CreateVolume"))
	}

	sizeRequestGB := getSizeRequest(in.CapacityRange)
	planID := getPlanID(in.Parameters)

	logger.WithFields(log.Fields{"planID": planID, "sizeRequestGB": sizeRequestGB}).Infof("Volume requested")

	// check for pre-existing volume
	volumes, httpResponse, err := controller.Provider.ListVolumes()
	if err != nil {
		return nil, errors.Wrap(err, httpResponse.Status)
	}
	if httpResponse.StatusCode != http.StatusOK {
		return nil, errors.Errorf("bad status from list volumes, %s", httpResponse.Status)
	}
	for _, volume := range volumes {

		description, err := packet.ReadDescription(volume.Description)
		if err == nil && description.Name == in.Name {
			logger.Infof("Volume already exists with id %s", volume.ID)

			if volume.Size != sizeRequestGB {
				return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("mismatch with existing volume %s, size %d, requested %d", in.Name, volume.Size, sizeRequestGB))
			}
			if volume.Plan.ID != planID {
				return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("mismatch with existing volume %s, plan %+v, requested %s", in.Name, volume.Plan, planID))
			}

			out := csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					CapacityBytes: int64(volume.Size) * packet.GB,
					Id:            volume.ID,
					Attributes:    nil,
				},
			}
			return &out, nil
		}
	}

	description := packet.NewVolumeDescription(in.Name)

	volumeCreateRequest := packngo.VolumeCreateRequest{
		Size:         sizeRequestGB,        // int               `json:"size"`
		BillingCycle: packet.BillingHourly, // string            `json:"billing_cycle"`
		PlanID:       planID,               // string            `json:"plan_id"`
		Description:  description.String(), // string            `json:"description,omitempty"`
		// SnapshotPolicies // []*SnapshotPolicy `json:"snapshot_policies,omitempty"`
	}
	volume, httpResponse, err := controller.Provider.Create(&volumeCreateRequest)

	if err != nil {
		return nil, err
	}
	if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusCreated {
		return nil, errors.Errorf("bad status from create volume, %s", httpResponse.Status)
	}
	description, err = packet.ReadDescription(volume.Description)
	if err != nil {
		return nil, errors.Wrap(err, "unable to read csi description from provider volume")
	}
	out := csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			CapacityBytes: int64(volume.Size) * packet.GB,
			Id:            volume.ID,
			Attributes:    nil,
		},
	}

	return &out, nil
}

func (controller *PacketControllerServer) DeleteVolume(ctx context.Context, in *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("controller not configured"))
	}
	logger := log.WithFields(log.Fields{"volume_id": in.VolumeId})
	logger.Info("DeleteVolume called")

	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeId unspecified for DeleteVolume"))
	}

	httpResponse, err := controller.Provider.Delete(in.GetVolumeId())
	if err != nil {
		if httpResponse.StatusCode == http.StatusUnprocessableEntity {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("delete should retry, %v", err))
		}
		return nil, status.Error(codes.Unknown, fmt.Sprintf("bad status from delete volumes, %s", httpResponse.Status))
	}
	switch httpResponse.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return &csi.DeleteVolumeResponse{}, nil
	case http.StatusUnprocessableEntity:
		return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("code %d indicates retry condition", httpResponse.StatusCode))
	}
	return nil, status.Error(codes.Unknown, fmt.Sprintf("bad status from delete volumes, %s", httpResponse.Status))
}

// ControllerPublishVolume attaches a volume to a node
func (controller *PacketControllerServer) ControllerPublishVolume(ctx context.Context, in *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, "controller not configured")
	}
	if in.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeId unspecified for ControllerPublishVolume")
	}
	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId unspecified for ControllerPublishVolume")
	}
	if in.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "VolumeCapability unspecified for ControllerPublishVolume")
	}

	csiNodeID := in.NodeId
	volumeID := in.VolumeId

	volume, httpResponse, err := controller.Provider.Get(volumeID)
	if err != nil {
		return nil, err
	}
	if httpResponse.StatusCode != http.StatusOK {
		return nil, errors.Errorf("bad status from get volume %s, %s", volumeID, httpResponse.Status)
	}

	nodes, httpResponse, err := controller.Provider.GetNodes()
	if err != nil {
		return nil, err
	}
	if httpResponse.StatusCode != http.StatusOK {
		return nil, errors.Errorf("bad status from get nodes, %s", httpResponse.Status)
	}
	// for packet this should be an ip address but try hostnam as well first anyway
	var nodeID string
	for _, node := range nodes {
		if node.Hostname == csiNodeID {
			nodeID = node.ID
			break
		}
		for _, ipAssignment := range node.Network {
			if ipAssignment.Address == csiNodeID {
				nodeID = node.ID
				break
			}
		}
	}
	if nodeID == "" {
		return nil, fmt.Errorf("node not found for host/ip %s", csiNodeID)
	}
	attachment, httpResponse, err := controller.Provider.Attach(volumeID, nodeID)
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("attempting to attach %s to %s", volumeID, nodeID))
	}
	if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusCreated {
		return nil, errors.Errorf("bad status from attach volumes, %s", httpResponse.Status)
	}

	metadata := make(map[string]string)
	metadata["AttachmentId"] = attachment.ID
	metadata["VolumeId"] = volumeID
	metadata["VolumeName"] = volume.Name
	response := &csi.ControllerPublishVolumeResponse{
		PublishInfo: metadata,
	}
	return response, nil
}

// ControllerPublishVolume detaches a volume from a node
func (controller *PacketControllerServer) ControllerUnpublishVolume(ctx context.Context, in *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("controller not configured"))
	}
	if in.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("NodeId unspecified for ControllerUnpublishVolume"))
	}
	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeId unspecified for ControllerUnpublishVolume"))
	}

	nodeID := in.GetNodeId()
	volumeID := in.GetVolumeId()

	volume, httpResponse, err := controller.Provider.Get(volumeID)
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}
	if httpResponse.StatusCode != http.StatusOK {
		return nil, errors.Errorf("bad status from get volume %s, %s", volumeID, httpResponse.Status)
	}
	attachments := volume.Attachments
	if attachments == nil {
		return nil, errors.Errorf("cannot detach unattached volume %s", volumeID)
	}
	attachmentID := ""
	for _, attachment := range attachments {
		if attachment.Volume.ID == volumeID && attachment.Device.ID == nodeID {
			attachmentID = attachment.ID
		}
	}

	httpResponse, err = controller.Provider.Detach(attachmentID)
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}
	if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusNotFound {
		return nil, errors.Errorf("bad status from detach volume, %s", httpResponse.Status)
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (controller *PacketControllerServer) ValidateVolumeCapabilities(ctx context.Context, in *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {

	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("controller not configured"))
	}

	if in.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeCapability unspecified for ValidateVolumeCapabilities"))
	}
	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("VolumeId unspecified for ValidateVolumeCapabilities"))
	}
	// if capabilities depended on the volume, we would retrieve it here
	// testVolumeID := in.volumeID
	// testVolume := controller.Provider.Get(testVolumeID)

	// supported capabilities all defined here instead
	supported := []*csi.VolumeCapability_AccessMode{}
	supported = append(supported, &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER})
	supported = append(supported, &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY})

	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Supported: false,
	}

	for _, cap := range in.VolumeCapabilities {

		mode := cap.AccessMode
		hasSupport := false
		for _, supportedCap := range supported {
			if mode.Mode == supportedCap.Mode {
				hasSupport = true
			}
		}

		if !hasSupport {
			return resp, nil
		}
	}
	resp.Supported = true
	return resp, nil
}

func (controller *PacketControllerServer) ListVolumes(ctx context.Context, in *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("controller not configured"))
	}

	volumes, httpResponse, err := controller.Provider.ListVolumes()
	if err != nil {
		return nil, errors.Wrap(err, httpResponse.Status)
	}
	if httpResponse.StatusCode != http.StatusOK {
		return nil, errors.Errorf("bad status from list volumes, %s", httpResponse.Status)
	}
	entries := []*csi.ListVolumesResponse_Entry{}
	for _, volume := range volumes {
		entry := &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				CapacityBytes: int64(volume.Size * 1024 * 1024 * 1024),
				Id:            volume.ID,
			},
		}
		entries = append(entries, entry)
	}
	response := &csi.ListVolumesResponse{}
	response.Entries = entries
	return response, nil

}

func (controller *PacketControllerServer) GetCapacity(ctx context.Context, in *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (controller *PacketControllerServer) ControllerGetCapabilities(ctx context.Context, in *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {

	// mapping function from defined RPC constant to capability type
	rpcCapMapper := func(cap csi.ControllerServiceCapability_RPC_Type) *csi.ControllerServiceCapability {
		return &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		}
	}

	// XXX review, move definitions elsewhere
	var caps []*csi.ControllerServiceCapability
	for _, rpcCap := range []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
	} {
		caps = append(caps, rpcCapMapper(rpcCap))
	}

	resp := &csi.ControllerGetCapabilitiesResponse{
		Capabilities: caps,
	}

	return resp, nil
}

func (controller *PacketControllerServer) CreateSnapshot(context.Context, *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (controller *PacketControllerServer) DeleteSnapshot(context.Context, *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (controller *PacketControllerServer) ListSnapshots(context.Context, *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
