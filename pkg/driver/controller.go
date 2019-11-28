package driver

import (
	"github.com/packethost/packngo"
	"github.com/pkg/errors"

	"net/http"
	"strconv"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/packethost/csi-packet/pkg/packet"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// VolumeMaxRetries maximum number of times to retry until giving up on a volume create request
	VolumeMaxRetries = 10
	// VolumeRetryInterval retry interval in seconds between retries to check if a volume create request is ready
	VolumeRetryInterval = 1 // in seconds
)

var _ csi.ControllerServer = &PacketControllerServer{}

// PacketControllerServer controller server to manage CSI
type PacketControllerServer struct {
	Provider packet.VolumeProvider
}

// NewPacketControllerServer create new PacketControllerServer with the given provider
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
	var sizeRequestGiB int
	if capacityRange == nil {
		sizeRequestGiB = packet.DefaultVolumeSizeGi
	} else {
		maxBytes := capacityRange.GetLimitBytes()
		if maxBytes != 0 {
			sizeRequestGiB = int(maxBytes / packet.Gibi)

		} else {
			minBytes := capacityRange.GetRequiredBytes()
			if minBytes != 0 {
				sizeRequestGiB = int(minBytes / packet.Gibi)

			}
		}
	}
	if sizeRequestGiB > packet.MaxVolumeSizeGi {
		sizeRequestGiB = packet.MaxVolumeSizeGi
	}
	if sizeRequestGiB < packet.MinVolumeSizeGi {
		sizeRequestGiB = packet.MinVolumeSizeGi
	}
	return sizeRequestGiB
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

// CreateVolume create a volume in the given context
// according to https://kubernetes-csi.github.io/docs/external-provisioner.html this should return
// when the volume is successfully provisioned or fails
// csi contains no provision for returning from a volume creation *request* and then checking later
// thus, we must either succeed or fail before returning from this function call
func (controller *PacketControllerServer) CreateVolume(ctx context.Context, in *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {

	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, "controller not configured")
	}
	logger := log.WithFields(log.Fields{"volume_name": in.Name})
	logger.Info("CreateVolume called")

	if in.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "Name unspecified for CreateVolume")
	}
	if in.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "VolumeCapabilities unspecified for CreateVolume")
	}

	sizeRequestGiB := getSizeRequest(in.CapacityRange)
	planID := getPlanID(in.Parameters)

	logger.WithFields(log.Fields{"planID": planID, "sizeRequestGiB": sizeRequestGiB}).Info("Volume requested")

	// check for pre-existing volume
	volumes, httpResponse, err := controller.Provider.ListVolumes(nil)
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

			if volume.Size != sizeRequestGiB {
				return nil, status.Errorf(codes.AlreadyExists, "mismatch with existing volume %s, size %d, requested %d", in.Name, volume.Size, sizeRequestGiB)
			}
			if volume.Plan.ID != planID {
				return nil, status.Errorf(codes.AlreadyExists, "mismatch with existing volume %s, plan %+v, requested %s", in.Name, volume.Plan, planID)
			}

			out := csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					CapacityBytes: int64(volume.Size) * packet.Gibi,
					VolumeId:      volume.ID,
				},
			}
			return &out, nil
		}
	}

	description := packet.NewVolumeDescription(in.Name)

	volumeCreateRequest := packngo.VolumeCreateRequest{
		Size:         sizeRequestGiB,       // int               `json:"size"`
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

	// as described in the description to this CreateVolume method, we must wait for success or failure
	// before returning
	volReady := packet.VolumeReady(volume)
	for counter := 0; !volReady && counter < VolumeMaxRetries; counter++ {
		time.Sleep(VolumeRetryInterval * time.Second)
		volume, httpResponse, err := controller.Provider.Get(volume.ID)
		if err != nil {
			return nil, err
		}
		if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusCreated {
			return nil, errors.Errorf("bad status from create volume, %s", httpResponse.Status)
		}
		volReady = packet.VolumeReady(volume)
	}
	if !volReady {
		return nil, errors.Errorf("volume %s not in ready state after %d seconds", volume.Name, VolumeMaxRetries*VolumeRetryInterval)
	}
	out := csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			CapacityBytes: int64(volume.Size) * packet.Gibi,
			VolumeId:      volume.ID,
		},
	}

	return &out, nil
}

// DeleteVolume delete the specific volume
func (controller *PacketControllerServer) DeleteVolume(ctx context.Context, in *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, "controller not configured")
	}
	logger := log.WithFields(log.Fields{"volume_id": in.VolumeId})
	logger.Info("DeleteVolume called")

	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId unspecified for DeleteVolume")
	}

	httpResponse, err := controller.Provider.Delete(in.GetVolumeId())
	if err != nil {
		if httpResponse.StatusCode == http.StatusUnprocessableEntity {
			return nil, status.Errorf(codes.FailedPrecondition, "delete should retry, %v", err)
		}
		return nil, status.Errorf(codes.Unknown, "bad status from delete volumes, %s", httpResponse.Status)
	}
	switch httpResponse.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return &csi.DeleteVolumeResponse{}, nil
	case http.StatusUnprocessableEntity:
		return nil, status.Errorf(codes.FailedPrecondition, "code %d indicates retry condition", httpResponse.StatusCode)
	}
	return nil, status.Errorf(codes.Unknown, "bad status from delete volumes, %s", httpResponse.Status)
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

	returnError := processGetError(volumeID, httpResponse, err)
	if returnError != nil {
		return nil, returnError
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
		return nil, status.Errorf(codes.NotFound, "node not found for host/ip %s", csiNodeID)
	}
	attachment, httpResponse, err := controller.Provider.Attach(volumeID, nodeID)
	if err != nil {
		return nil, status.Errorf(codes.Unknown, "error attempting to attach %s to %s, %v", volumeID, nodeID, err)
	}
	if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusCreated {
		return nil, status.Errorf(codes.Unknown, "bad status from attach volumes, %s", httpResponse.Status)
	}

	metadata := make(map[string]string)
	metadata["AttachmentId"] = attachment.ID
	metadata["VolumeId"] = volumeID
	metadata["VolumeName"] = volume.Name
	response := &csi.ControllerPublishVolumeResponse{
		PublishContext: metadata,
	}
	return response, nil
}

// ControllerUnpublishVolume detaches a volume from a node
func (controller *PacketControllerServer) ControllerUnpublishVolume(ctx context.Context, in *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	logger := log.WithFields(log.Fields{"node_id": in.NodeId, "volume_id": in.VolumeId})
	logger.Info("UnpublishVolume called")

	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, "controller not configured")
	}
	if in.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeId unspecified for ControllerUnpublishVolume")
	}
	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId unspecified for ControllerUnpublishVolume")
	}

	nodeID := in.GetNodeId()
	volumeID := in.GetVolumeId()

	volume, httpResponse, err := controller.Provider.Get(volumeID)
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			logger.Infof("volumeId not found, Get() returned %d", httpResponse.StatusCode)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}
	if httpResponse.StatusCode != http.StatusOK {
		return nil, status.Errorf(codes.Unknown, "bad status from get volume %s, %s", volumeID, httpResponse.Status)
	}
	attachments := volume.Attachments
	if attachments == nil {
		return nil, status.Errorf(codes.Unknown, "cannot detach unattached volume %s", volumeID)
	}
	attachmentID := ""
	for _, attachment := range attachments {
		if attachment.Volume.ID == volumeID && attachment.Device.ID == nodeID {
			attachmentID = attachment.ID
		}
	}
	logger = logger.WithFields(log.Fields{"attachmentID": attachmentID})
	logger.Info("attachmentID found")

	httpResponse, err = controller.Provider.Detach(attachmentID)
	if err != nil {
		if httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound {
			logger.Infof("attachmentID not found, Detach() returned %d", httpResponse.StatusCode)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}
	if httpResponse.StatusCode != http.StatusOK && httpResponse.StatusCode != http.StatusNotFound {
		return nil, errors.Errorf("bad status from detach volume, %s", httpResponse.Status)
	}

	logger.Info("successful Detach()")
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

// ValidateVolumeCapabilities validate that a given volume has the require capabilities
func (controller *PacketControllerServer) ValidateVolumeCapabilities(ctx context.Context, in *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {

	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, "controller not configured")
	}

	if in.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "VolumeCapability unspecified for ValidateVolumeCapabilities")
	}
	if in.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "VolumeId unspecified for ValidateVolumeCapabilities")
	}
	// we always have to retrieve the volume to check that it exists; it is a CSI spec requirement
	volumeID := in.VolumeId
	_, httpResponse, err := controller.Provider.Get(volumeID)
	returnError := processGetError(volumeID, httpResponse, err)
	if returnError != nil {
		return nil, returnError
	}

	// supported capabilities all defined here instead
	supported := map[csi.VolumeCapability_AccessMode_Mode]bool{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:      true,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY: true,
	}
	for _, cap := range in.VolumeCapabilities {
		mode := cap.AccessMode.Mode
		if !supported[mode] {
			return &csi.ValidateVolumeCapabilitiesResponse{}, nil
		}
	}
	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: in.VolumeCapabilities,
		},
	}, nil
}

// ListVolumes list known volumes
func (controller *PacketControllerServer) ListVolumes(ctx context.Context, in *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	if controller == nil || controller.Provider == nil {
		return nil, status.Error(codes.Internal, "controller not configured")
	}

	// was there any pagination?
	var listOptions *packngo.ListOptions
	if in != nil {
		listOptions = &packngo.ListOptions{
			PerPage: int(in.MaxEntries),
		}
		if in.StartingToken != "" {
			page, err := strconv.Atoi(in.StartingToken)
			if err != nil {
				return nil, status.Errorf(codes.Aborted, "starting token must be an integer to indicate which page, %s", in.StartingToken)
			}
			listOptions.Page = page
		}
	}
	volumes, httpResponse, err := controller.Provider.ListVolumes(listOptions)
	if err != nil {
		return nil, errors.Wrap(err, httpResponse.Status)
	}
	if httpResponse.StatusCode != http.StatusOK {
		return nil, status.Errorf(codes.Unknown, "bad status from list volumes, %s", httpResponse.Status)
	}
	entries := []*csi.ListVolumesResponse_Entry{}
	for _, volume := range volumes {
		entry := &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				CapacityBytes: int64(volume.Size * 1024 * 1024 * 1024),
				VolumeId:      volume.ID,
			},
		}
		entries = append(entries, entry)
	}
	response := &csi.ListVolumesResponse{}
	response.Entries = entries
	return response, nil

}

// GetCapacity get the available capacity
func (controller *PacketControllerServer) GetCapacity(ctx context.Context, in *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities get capabilities of the controller
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

// CreateSnapshot snapshot a single volume
func (controller *PacketControllerServer) CreateSnapshot(context.Context, *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// DeleteSnapshot delete an existing snapshot
func (controller *PacketControllerServer) DeleteSnapshot(context.Context, *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ListSnapshots list known snapshots
func (controller *PacketControllerServer) ListSnapshots(context.Context, *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerExpandVolume expand a volume
func (controller *PacketControllerServer) ControllerExpandVolume(context.Context, *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// take the packet error return code from Provider.Get and determine what we should do with it
func processGetError(volumeID string, httpResponse *packngo.Response, err error) error {
	// if we have no valid response and an error, return the error immediately
	// if we have a valid response but not found, return the not found special code
	// if we have a valid response but not OK, return a general error
	// if any other error, return it
	// otherwise, everything is fine, continue
	switch {
	case httpResponse == nil && err != nil:
		return err
	case httpResponse != nil && httpResponse.StatusCode == http.StatusNotFound:
		return status.Errorf(codes.NotFound, "volume not found %s", volumeID)
	case httpResponse != nil && httpResponse.StatusCode != http.StatusOK:
		return errors.Errorf("bad status from get volume %s, %s", volumeID, httpResponse.Status)
	case err != nil:
		return err
	default:
		return nil
	}
}
