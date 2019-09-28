package driver

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/packethost/csi-packet/pkg/packet"
	"github.com/packethost/csi-packet/pkg/test"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/mock/gomock"
	"github.com/packethost/packngo"
	"github.com/stretchr/testify/assert"
)

const (
	attachmentID     = "60bf5425-e59d-42c3-b9b9-ac0d8cfc86a2"
	providerVolumeID = "9b03a6ea-42fb-40c7-abaa-247445b36890"
	csiNodeIP        = "10.88.52.133"
	csiNodeName      = "spcfoobar-worker-1"
	nodeID           = "262c173c-c24d-4ad6-be1a-13fd9a523cfa"
)

func TestCreateVolume(t *testing.T) {
	csiVolumeName := "kubernetes-volume-request-0987654321"

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	provider := test.NewMockVolumeProvider(mockCtrl)
	volume := packngo.Volume{
		Size:        packet.DefaultVolumeSizeGi,
		ID:          providerVolumeID,
		Description: packet.NewVolumeDescription(csiVolumeName).String(),
		State:       "active",
	}
	resp := packngo.Response{
		Response: &http.Response{
			StatusCode: http.StatusOK,
		},
		Rate: packngo.Rate{},
	}
	provider.EXPECT().ListVolumes().Return([]packngo.Volume{}, &resp, nil)
	provider.EXPECT().Create(gomock.Any()).Return(&volume, &resp, nil)

	controller := NewPacketControllerServer(provider)
	volumeRequest := csi.CreateVolumeRequest{
		VolumeCapabilities: []*csi.VolumeCapability{
			&csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},
	}
	volumeRequest.Name = csiVolumeName
	volumeRequest.CapacityRange = &csi.CapacityRange{
		RequiredBytes: 10 * packet.Gibi,
		LimitBytes:    100 * packet.Gibi,
	}

	csiResp, err := controller.CreateVolume(context.TODO(), &volumeRequest)
	assert.Nil(t, err)
	assert.Equal(t, providerVolumeID, csiResp.GetVolume().VolumeId)
	assert.Equal(t, packet.DefaultVolumeSizeGi*packet.Gibi, csiResp.GetVolume().GetCapacityBytes())
}

type matchRequest struct {
	desc    string
	request packngo.VolumeCreateRequest
}

func MatchRequest(desc string, request packngo.VolumeCreateRequest) gomock.Matcher {
	return &matchRequest{desc, request}
}

func (o *matchRequest) Matches(x interface{}) bool {
	volumeRequest := x.(*packngo.VolumeCreateRequest)
	return volumeRequest.Size == o.request.Size &&
		volumeRequest.PlanID == o.request.PlanID
}

type matchGet struct {
	desc string
	id   string
}

func (o *matchGet) Matches(x interface{}) bool {
	id := x.(string)
	return id == o.id
}

func (o *matchRequest) String() string {
	return fmt.Sprintf("[%s] has request matching <<%v>>", o.desc, o.request)
}

func runTestCreateVolume(t *testing.T, description string, volumeRequest csi.CreateVolumeRequest, providerRequest packngo.VolumeCreateRequest, providerVolume packngo.Volume, success bool, delayToSuccess int) {

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	provider := test.NewMockVolumeProvider(mockCtrl)

	resp := packngo.Response{
		Response: &http.Response{
			StatusCode: http.StatusOK,
		},
		Rate: packngo.Rate{},
	}
	provider.EXPECT().ListVolumes().Return([]packngo.Volume{}, &resp, nil)
	// provider.EXPECT().Create(gomock.Any()).Return(&providerVolume, &resp, nil)
	provider.EXPECT().
		Create(MatchRequest(description, providerRequest)).
		Return(&providerVolume, &resp, nil)

	// did we ask for it not to be ready a few times before it is ready?
	if delayToSuccess > 0 {
		// set the state to queued
		providerVolume.State = "queued"
		// we do it up to the maximum times
		calls := min(delayToSuccess-1, VolumeMaxRetries)
		for i := 0; i < calls; i++ {
			pv := providerVolume
			pv.State = "queued"
			provider.EXPECT().Get(pv.ID).Return(&pv, &resp, nil)
		}
		// for the last one, if not beyond max, set the state to "active"
		if delayToSuccess < VolumeMaxRetries {
			pv := providerVolume
			pv.State = "active"
			provider.EXPECT().Get(pv.ID).Return(&pv, &resp, nil)
		}
	}

	controller := NewPacketControllerServer(provider)

	csiResp, err := controller.CreateVolume(context.TODO(), &volumeRequest)
	if success {
		assert.Nil(t, err, description)
		assert.Equal(t, providerVolume.ID, csiResp.GetVolume().VolumeId, description)
		assert.Equal(t, int64(providerVolume.Size)*packet.Gibi, csiResp.GetVolume().GetCapacityBytes(), description)
	} else {
		assert.NotNil(t, err, description)
	}
}

type VolumeTestCase struct {
	description     string
	volumeRequest   csi.CreateVolumeRequest
	providerRequest packngo.VolumeCreateRequest
	providerVolume  packngo.Volume
	success         bool
	delayToSuccess  int
}

func TestCreateVolumes(t *testing.T) {
	testCases := []VolumeTestCase{
		VolumeTestCase{
			description: "verify capacity specification",
			volumeRequest: csi.CreateVolumeRequest{
				Name: "pv-qT2QXcwbqPB3BAurt1ccs7g6SDVT0qLv",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 10 * packet.Gibi,
					LimitBytes:    173 * packet.Gibi,
				},
				VolumeCapabilities: []*csi.VolumeCapability{
					&csi.VolumeCapability{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			providerRequest: packngo.VolumeCreateRequest{
				BillingCycle: packet.BillingHourly,
				Description:  packet.NewVolumeDescription("pv-qT2QXcwbqPB3BAurt1ccs7g6SDVT0qLv").String(),
				Locked:       false,
				Size:         173,
				PlanID:       packet.VolumePlanStandardID,
			},
			providerVolume: packngo.Volume{
				Size:        173,
				ID:          "5a3c678a-64a4-41ba-a03c-e7d74a96f06a",
				Description: packet.NewVolumeDescription("pv-qT2QXcwbqPB3BAurt1ccs7g6SDVT0qLv").String(),
				State:       "active",
			},
			success:        true,
			delayToSuccess: 0,
		},
		VolumeTestCase{
			description: "verify capacity maximum",
			volumeRequest: csi.CreateVolumeRequest{
				Name: "pv-61C4yMq09WV1ZpNIOBKHRQDKoZzyK7ZF",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 1 * 1024 * 1024,
					LimitBytes:    15000 * packet.Gibi,
				},
				VolumeCapabilities: []*csi.VolumeCapability{
					&csi.VolumeCapability{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			providerRequest: packngo.VolumeCreateRequest{
				BillingCycle: packet.BillingHourly,
				Description:  packet.NewVolumeDescription("pv-61C4yMq09WV1ZpNIOBKHRQDKoZzyK7ZF").String(),
				Locked:       false,
				Size:         packet.MaxVolumeSizeGi,
				PlanID:       packet.VolumePlanStandardID,
			},
			providerVolume: packngo.Volume{
				Size:        packet.DefaultVolumeSizeGi,
				ID:          "06e45c5c-8bd9-44fd-a9e4-1518105de113",
				Description: packet.NewVolumeDescription("pv-61C4yMq09WV1ZpNIOBKHRQDKoZzyK7ZF").String(),
				State:       "active",
			},
			success:        true,
			delayToSuccess: 0,
		},
		VolumeTestCase{
			description: "verify capacity minimum",
			volumeRequest: csi.CreateVolumeRequest{
				Name: "pv-pUk6DzHQF3cGMfLCRnXSpDJ2HpzhefKI",
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 1 * 1024 * 1024,
					LimitBytes:    1 * 1024 * 1024,
				},
				VolumeCapabilities: []*csi.VolumeCapability{
					&csi.VolumeCapability{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			providerRequest: packngo.VolumeCreateRequest{
				BillingCycle: packet.BillingHourly,
				Description:  packet.NewVolumeDescription("pv-61C4yMq09WV1ZpNIOBKHRQDKoZzyK7ZF").String(),
				Locked:       false,
				Size:         packet.MinVolumeSizeGi,
				PlanID:       packet.VolumePlanStandardID,
			},
			providerVolume: packngo.Volume{
				Size:        packet.DefaultVolumeSizeGi,
				ID:          "8c3b6f51-7045-44b8-ab6d-d6df7371471e",
				Description: packet.NewVolumeDescription("pv-61C4yMq09WV1ZpNIOBKHRQDKoZzyK7ZF").String(),
				State:       "active",
			},
			success:        true,
			delayToSuccess: 0,
		},
		VolumeTestCase{
			description: "verify capacity default, performance plan type",
			volumeRequest: csi.CreateVolumeRequest{
				Name:       "pv-pUk6DzHQF3cGMfLCRnXSpDJ2HpzhefKI",
				Parameters: map[string]string{"plan": "performance"},
				VolumeCapabilities: []*csi.VolumeCapability{
					&csi.VolumeCapability{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			providerRequest: packngo.VolumeCreateRequest{
				BillingCycle: packet.BillingHourly,
				Description:  packet.NewVolumeDescription("pv-61C4yMq09WV1ZpNIOBKHRQDKoZzyK7ZF").String(),
				Locked:       false,
				Size:         packet.DefaultVolumeSizeGi,
				PlanID:       packet.VolumePlanPerformanceID,
			},
			providerVolume: packngo.Volume{
				Size:        packet.DefaultVolumeSizeGi,
				ID:          "a94ecff0-b221-4d2d-8dc4-432bed506941",
				Description: packet.NewVolumeDescription("pv-61C4yMq09WV1ZpNIOBKHRQDKoZzyK7ZF").String(),
				State:       "active",
			},
			success:        true,
			delayToSuccess: 0,
		},
		VolumeTestCase{
			description: "becomes ready after 5 seconds",
			volumeRequest: csi.CreateVolumeRequest{
				Name:       "pv-succeedin5",
				Parameters: map[string]string{"plan": "performance"},
				VolumeCapabilities: []*csi.VolumeCapability{
					&csi.VolumeCapability{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			providerRequest: packngo.VolumeCreateRequest{
				BillingCycle: packet.BillingHourly,
				Description:  packet.NewVolumeDescription("pv-succeedin5").String(),
				Locked:       false,
				Size:         packet.DefaultVolumeSizeGi,
				PlanID:       packet.VolumePlanPerformanceID,
			},
			providerVolume: packngo.Volume{
				Size:        packet.DefaultVolumeSizeGi,
				ID:          "abcdef-22bb-d4d4-cc5f-cw1123456abcdef",
				Description: packet.NewVolumeDescription("pv-succeedin5").String(),
				State:       "active",
			},
			success:        true,
			delayToSuccess: 7,
		},
		VolumeTestCase{
			description: "never becomes ready",
			volumeRequest: csi.CreateVolumeRequest{
				Name:       "pv-failfail123",
				Parameters: map[string]string{"plan": "performance"},
				VolumeCapabilities: []*csi.VolumeCapability{
					&csi.VolumeCapability{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
			},
			providerRequest: packngo.VolumeCreateRequest{
				BillingCycle: packet.BillingHourly,
				Description:  packet.NewVolumeDescription("pv-failfail123").String(),
				Locked:       false,
				Size:         packet.DefaultVolumeSizeGi,
				PlanID:       packet.VolumePlanPerformanceID,
			},
			providerVolume: packngo.Volume{
				Size:        packet.DefaultVolumeSizeGi,
				ID:          "ff665c4-22bb-d4d4-cc5f-cw1123456abcdef",
				Description: packet.NewVolumeDescription("pv-failfail123").String(),
				State:       "active",
			},
			success:        false,
			delayToSuccess: 1000,
		},
	}

	for _, testCase := range testCases {
		runTestCreateVolume(t, testCase.description, testCase.volumeRequest, testCase.providerRequest, testCase.providerVolume, testCase.success, testCase.delayToSuccess)
	}
}

func TestIdempotentCreateVolume(t *testing.T) {

	csiVolumeName := "kubernetes-volume-request-0987654321"

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	provider := test.NewMockVolumeProvider(mockCtrl)
	volumeAlreadyExisting := packngo.Volume{
		Size:        packet.DefaultVolumeSizeGi,
		ID:          providerVolumeID,
		Description: packet.NewVolumeDescription(csiVolumeName).String(),
		Plan: &packngo.Plan{
			Name: packet.VolumePlanStandard,
			ID:   packet.VolumePlanStandardID,
		},
	}
	resp := packngo.Response{
		Response: &http.Response{
			StatusCode: http.StatusOK,
		},
		Rate: packngo.Rate{},
	}
	provider.EXPECT().ListVolumes().Return([]packngo.Volume{volumeAlreadyExisting}, &resp, nil)

	controller := NewPacketControllerServer(provider)
	volumeRequest := csi.CreateVolumeRequest{
		Name: csiVolumeName,
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: packet.DefaultVolumeSizeGi * packet.Gibi,
			LimitBytes:    packet.DefaultVolumeSizeGi * packet.Gibi,
		},
		VolumeCapabilities: []*csi.VolumeCapability{
			&csi.VolumeCapability{
				AccessMode: &csi.VolumeCapability_AccessMode{
					Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
				},
			},
		},
		Parameters: map[string]string{
			"plan": packet.VolumePlanStandard,
		},
	}

	csiResp, err := controller.CreateVolume(context.TODO(), &volumeRequest)
	assert.Nil(t, err)
	assert.Equal(t, volumeAlreadyExisting.ID, csiResp.GetVolume().VolumeId)
}

func TestListVolumes(t *testing.T) {

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	provider := test.NewMockVolumeProvider(mockCtrl)

	resp := packngo.Response{
		Response: &http.Response{
			StatusCode: http.StatusOK,
		},
		Rate: packngo.Rate{},
	}
	provider.EXPECT().ListVolumes().Return([]packngo.Volume{}, &resp, nil)

	controller := NewPacketControllerServer(provider)
	volumeRequest := csi.ListVolumesRequest{}

	csiResp, err := controller.ListVolumes(context.TODO(), &volumeRequest)
	assert.Nil(t, err)
	assert.NotNil(t, csiResp)
	assert.Equal(t, 0, len(csiResp.Entries))

}

func TestDeleteVolume(t *testing.T) {

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	provider := test.NewMockVolumeProvider(mockCtrl)

	resp := packngo.Response{
		Response: &http.Response{
			StatusCode: http.StatusOK,
		},
		Rate: packngo.Rate{},
	}
	provider.EXPECT().Delete(providerVolumeID).Return(&resp, nil)

	controller := NewPacketControllerServer(provider)
	volumeRequest := csi.DeleteVolumeRequest{
		VolumeId: providerVolumeID,
	}

	csiResp, err := controller.DeleteVolume(context.TODO(), &volumeRequest)
	assert.Nil(t, err)
	assert.NotNil(t, csiResp)

}

func TestPublishVolume(t *testing.T) {

	providerVolumeName := "name-assigned-by-provider"

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	provider := test.NewMockVolumeProvider(mockCtrl)

	resp := packngo.Response{
		Response: &http.Response{
			StatusCode: http.StatusOK,
		},
		Rate: packngo.Rate{},
	}
	nodeIPAddress := packngo.IPAddressAssignment{}
	nodeIPAddress.Address = csiNodeIP
	nodeResp := []packngo.Device{
		packngo.Device{
			Hostname: csiNodeName,
			ID:       nodeID,
			Network: []*packngo.IPAddressAssignment{
				&nodeIPAddress,
			},
		},
	}
	volumeResp := packngo.Volume{
		ID:   providerVolumeID,
		Name: providerVolumeName,
		Attachments: []*packngo.VolumeAttachment{
			&packngo.VolumeAttachment{
				ID: attachmentID,
				Volume: packngo.Volume{
					ID: providerVolumeID,
				},
				Device: packngo.Device{
					ID: nodeID,
				},
			},
		},
	}
	attachResp := packngo.VolumeAttachment{
		ID:     attachmentID,
		Volume: volumeResp,
		Device: packngo.Device{
			ID: nodeID,
		},
	}
	provider.EXPECT().GetNodes().Return(nodeResp, &resp, nil)

	provider.EXPECT().Get(providerVolumeID).Return(&volumeResp, &resp, nil)

	provider.EXPECT().Attach(providerVolumeID, nodeID).Return(&attachResp, &resp, nil)

	controller := NewPacketControllerServer(provider)
	volumeRequest := csi.ControllerPublishVolumeRequest{
		VolumeId:         providerVolumeID,
		NodeId:           csiNodeIP,
		VolumeCapability: &csi.VolumeCapability{},
	}

	csiResp, err := controller.ControllerPublishVolume(context.TODO(), &volumeRequest)
	assert.Nil(t, err)
	assert.NotNil(t, csiResp)
	assert.NotNil(t, csiResp.GetPublishContext())
	assert.Equal(t, attachmentID, csiResp.PublishContext["AttachmentId"])
	assert.Equal(t, providerVolumeID, csiResp.PublishContext["VolumeId"])
	assert.Equal(t, providerVolumeName, csiResp.PublishContext["VolumeName"])

}

func TestUnpublishVolume(t *testing.T) {

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	provider := test.NewMockVolumeProvider(mockCtrl)

	resp := packngo.Response{
		Response: &http.Response{
			StatusCode: http.StatusOK,
		},
		Rate: packngo.Rate{},
	}
	attachedVolume := packngo.Volume{
		ID: providerVolumeID,
		Attachments: []*packngo.VolumeAttachment{
			&packngo.VolumeAttachment{
				ID: attachmentID,
				Volume: packngo.Volume{
					ID: providerVolumeID,
				},
				Device: packngo.Device{
					ID: nodeID,
				},
			},
		},
	}

	provider.EXPECT().Get(providerVolumeID).Return(&attachedVolume, &resp, nil)
	provider.EXPECT().Detach(attachmentID).Return(&resp, nil)

	controller := NewPacketControllerServer(provider)
	volumeRequest := csi.ControllerUnpublishVolumeRequest{
		VolumeId: providerVolumeID,
		NodeId:   nodeID,
	}

	csiResp, err := controller.ControllerUnpublishVolume(context.TODO(), &volumeRequest)
	assert.Nil(t, err)
	assert.NotNil(t, csiResp)

}

func TestGetCapacity(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	provider := test.NewMockVolumeProvider(mockCtrl)

	capacityRequest := csi.GetCapacityRequest{}
	controller := NewPacketControllerServer(provider)
	csiResp, err := controller.GetCapacity(context.TODO(), &capacityRequest)
	assert.NotNil(t, err, "this method is not implemented")
	assert.Nil(t, csiResp, "this method is not implemented")
}

type volumeCapabilityTestCase struct {
	capabilitySet   []*csi.VolumeCapability
	packetSupported *csi.ValidateVolumeCapabilitiesResponse_Confirmed
	description     string
}

func getVolumeCapabilityTestCases() []volumeCapabilityTestCase {

	snwCap := csi.VolumeCapability{
		AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
	}
	snroCap := csi.VolumeCapability{
		AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY},
	}
	mnmwCap := csi.VolumeCapability{
		AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
	}
	mnroCap := csi.VolumeCapability{
		AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY},
	}
	mnswCap := csi.VolumeCapability{
		AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER},
	}

	return []volumeCapabilityTestCase{

		{
			capabilitySet: []*csi.VolumeCapability{&snwCap},
			packetSupported: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
				VolumeCapabilities: []*csi.VolumeCapability{
					&snwCap,
				},
			},
			description: "single node writer",
		},
		{
			capabilitySet: []*csi.VolumeCapability{&snroCap},
			packetSupported: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
				VolumeCapabilities: []*csi.VolumeCapability{
					&snroCap,
				},
			},
			description: "single node read only",
		},
		{
			capabilitySet: []*csi.VolumeCapability{&mnroCap},
			description:   "multi node read only",
		},
		{
			capabilitySet: []*csi.VolumeCapability{&mnswCap},
			description:   "multinode single writer",
		},
		{
			capabilitySet: []*csi.VolumeCapability{&mnmwCap},
			description:   "multi node multi writer",
		},
		{
			capabilitySet: []*csi.VolumeCapability{&mnmwCap, &mnroCap, &mnswCap, &snroCap, &snwCap},
			description:   "all capabilities",
		},
		{
			capabilitySet: []*csi.VolumeCapability{&snroCap, &snwCap},
			packetSupported: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
				VolumeCapabilities: []*csi.VolumeCapability{
					&snroCap, &snwCap,
				},
			},
			description: "single node capabilities",
		},
	}
}

func TestValidateVolumeCapabilities(t *testing.T) {

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	provider := test.NewMockVolumeProvider(mockCtrl)

	controller := NewPacketControllerServer(provider)

	for _, testCase := range getVolumeCapabilityTestCases() {

		request := &csi.ValidateVolumeCapabilitiesRequest{
			VolumeCapabilities: testCase.capabilitySet,
			VolumeId:           providerVolumeID,
		}

		resp, err := controller.ValidateVolumeCapabilities(context.TODO(), request)
		assert.Nil(t, err)
		assert.Equal(t, testCase.packetSupported, resp.Confirmed, testCase.description)

	}

}

// min returns the smaller of x or y.
func min(x, y int) int {
	if x > y {
		return y
	}
	return x
}
