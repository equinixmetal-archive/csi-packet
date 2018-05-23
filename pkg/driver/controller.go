package driver

import (
	"fmt"

	"github.com/packethost/packngo"
	"github.com/pkg/errors"

	"net/http"

	"github.com/StackPointCloud/csi-packet/pkg/packet"
	csi "github.com/container-storage-interface/spec/lib/go/csi/v0"
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

func (controller *PacketControllerServer) CreateVolume(ctx context.Context, in *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {

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

	var planID string

	volumePlanRequest := in.GetParameters()["plan"]
	switch volumePlanRequest {
	case packet.VolumePlanPerformance:
		planID = packet.VolumePlanPerformanceID
	case packet.VolumePlanStandard:
		planID = packet.VolumePlanStandardID
	default:
		planID = packet.VolumePlanStandardID

	}

	// size request:
	//   limit if specified
	//   required otherwise
	//   within restrictions of max, min
	//   default otherwise
	var sizeRequestGB int
	capacityRange := in.CapacityRange
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

	httpResponse, err := controller.Provider.Delete(in.GetVolumeId())
	if err != nil {
		return nil, err
	}
	if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusNoContent && httpResponse.StatusCode != http.StatusNotFound {
		return nil, errors.Errorf("bad status from delete volumes, %s", httpResponse.Status)
	}
	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume attaches a volume to a node
func (controller *PacketControllerServer) ControllerPublishVolume(ctx context.Context, in *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	csiNodeID := in.GetNodeId()
	volumeID := in.GetVolumeId()

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
