package packet

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/packethost/packngo"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

const (
	// ConsumerToken token for packet consumer
	ConsumerToken = "csi-packet"
	// BillingHourly string to indicate hourly billing
	BillingHourly = "hourly"
)

// Config configuration for a volume provider, includes authentication token, project ID and facility ID
type Config struct {
	AuthToken  string `json:"apiKey"`
	ProjectID  string `json:"projectId"`
	FacilityID string `json:"facility-id"`
}

// VolumeProviderPacketImpl the volume provider for Packet
type VolumeProviderPacketImpl struct {
	config Config
}

var _ VolumeProvider = &VolumeProviderPacketImpl{}

// VolumeIDToName convert a volume UUID to its representative name, e.g. "3ee59355-a51a-42a8-b848-86626cc532f0" -> "volume-3ee59355"
func VolumeIDToName(id string) string {
	// "3ee59355-a51a-42a8-b848-86626cc532f0" -> "volume-3ee59355"
	uuidElements := strings.Split(id, "-")
	return fmt.Sprintf("volume-%s", uuidElements[0])
}

// NewPacketProvider create a new VolumeProviderPacketImpl from a given Config
func NewPacketProvider(config Config) (*VolumeProviderPacketImpl, error) {
	if config.AuthToken == "" {
		return nil, errors.New("AuthToken not specified")
	}
	if config.ProjectID == "" {
		return nil, errors.New("ProjectID not specified")
	}
	logger := log.WithFields(log.Fields{"project_id": config.ProjectID})
	logger.Info("Creating provider")

	if config.FacilityID == "" {
		facilityCode, err := GetFacilityCodeMetadata()
		if err != nil {
			logger.Errorf("Cannot get facility code %v", err)
			return nil, errors.Wrap(err, "cannot construct VolumeProviderPacketImpl")
		}
		c := constructClient(config.AuthToken)
		facilities, resp, err := c.Facilities.List()
		if err != nil {
			if resp.StatusCode == http.StatusForbidden {
				return nil, fmt.Errorf("cannot construct VolumeProviderPacketImpl, access denied to search facilities")
			}
			return nil, errors.Wrap(err, "cannot construct VolumeProviderPacketImpl")
		}
		for _, facility := range facilities {
			if facility.Code != facilityCode {
				continue
			}

			if !contains(facility.Features, "storage") {
				return nil, errors.New("this device's facility does not support storage volumes")
			}

			config.FacilityID = facility.ID
			logger.WithField("facility_id", facility.ID).Infof("facility found")
			break
		}
	}

	if config.FacilityID == "" {
		logger.Errorf("FacilityID not specified and cannot be found")
		return nil, fmt.Errorf("FacilityID not specified and cannot be found")
	}

	provider := VolumeProviderPacketImpl{config}
	return &provider, nil
}

func contains(arr []string, str string) bool {
	for _, a := range arr {
		if a == str {
			return true
		}
	}

	return false
}

func constructClient(authToken string) *packngo.Client {
	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}
	client := &http.Client{Transport: tr}

	// client.Transport = logging.NewTransport("Packet", client.Transport)
	return packngo.NewClientWithAuth(ConsumerToken, authToken, client)
}

// Client() returns a new client for accessing Packet's API.
func (p *VolumeProviderPacketImpl) client() *packngo.Client {
	return constructClient(p.config.AuthToken)
}

// ListVolumes wrap the packet api as an interface method
func (p *VolumeProviderPacketImpl) ListVolumes() ([]packngo.Volume, *packngo.Response, error) {
	return p.client().Volumes.List(p.config.ProjectID, &packngo.ListOptions{})
}

// Get wraps the packet api as an interface method
func (p *VolumeProviderPacketImpl) Get(volumeUUID string) (*packngo.Volume, *packngo.Response, error) {
	return p.client().Volumes.Get(volumeUUID)
}

// Delete wraps the packet api as an interface method
func (p *VolumeProviderPacketImpl) Delete(volumeUUID string) (*packngo.Response, error) {
	resp, err := p.client().Volumes.Delete(volumeUUID)
	if resp.StatusCode == http.StatusNotFound {
		return resp, nil
	}
	return resp, err
}

// Create wraps the packet api as an interface method
func (p *VolumeProviderPacketImpl) Create(createRequest *packngo.VolumeCreateRequest) (*packngo.Volume, *packngo.Response, error) {

	createRequest.FacilityID = p.config.FacilityID

	return p.client().Volumes.Create(createRequest, p.config.ProjectID)
}

// Attach wraps the packet api as an interface method
func (p *VolumeProviderPacketImpl) Attach(volumeID, deviceID string) (*packngo.VolumeAttachment, *packngo.Response, error) {
	volume, httpResponse, err := p.client().Volumes.Get(volumeID)
	if err != nil || httpResponse.StatusCode != http.StatusOK {
		return nil, httpResponse, errors.Wrap(err, "prechecking existence of volume attachment")
	}
	for _, attachment := range volume.Attachments {
		if attachment.Device.ID == deviceID {
			return p.client().VolumeAttachments.Get(attachment.ID)
		}
	}
	return p.client().VolumeAttachments.Create(volumeID, deviceID)
}

// Detach wraps the packet api as an interface method
func (p *VolumeProviderPacketImpl) Detach(attachmentID string) (*packngo.Response, error) {
	return p.client().VolumeAttachments.Delete(attachmentID)
}

// GetNodes list nodes
func (p *VolumeProviderPacketImpl) GetNodes() ([]packngo.Device, *packngo.Response, error) {
	return p.client().Devices.List(p.config.ProjectID, &packngo.ListOptions{})
}
