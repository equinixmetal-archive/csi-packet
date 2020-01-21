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
	// volumeInUseMessage message that is returned if volume is in use
	volumeInUseMessage = "Cannot detach since volume is actively being used on your server"
)

// Config configuration for a volume provider, includes authentication token, project ID and facility ID, and optional override URL to talk to a different packet API endpoint
type Config struct {
	AuthToken   string  `json:"apiKey"`
	ProjectID   string  `json:"projectId"`
	FacilityID  string  `json:"facility-id"`
	BaseURL     *string `json:"base-url,omitempty"`
	MetadataURL *string `json:"metadata-url,omitempty"`
}

// VolumeProviderPacketImpl the volume provider for Packet
type VolumeProviderPacketImpl struct {
	config   Config
	metadata MetadataDriver
}

var _ VolumeProvider = &VolumeProviderPacketImpl{}

// VolumeIDToName convert a volume UUID to its representative name, e.g. "3ee59355-a51a-42a8-b848-86626cc532f0" -> "volume-3ee59355"
func VolumeIDToName(id string) string {
	// "3ee59355-a51a-42a8-b848-86626cc532f0" -> "volume-3ee59355"
	uuidElements := strings.Split(id, "-")
	return fmt.Sprintf("volume-%s", uuidElements[0])
}

// NewPacketProvider create a new VolumeProviderPacketImpl from a given Config
func NewPacketProvider(config Config, metadata MetadataDriver) (*VolumeProviderPacketImpl, error) {
	if config.AuthToken == "" {
		return nil, errors.New("AuthToken not specified")
	}
	if config.ProjectID == "" {
		return nil, errors.New("ProjectID not specified")
	}
	logger := log.WithFields(log.Fields{"project_id": config.ProjectID})
	logger.Info("Creating provider")

	if config.FacilityID == "" {
		facilityCode, err := metadata.GetFacilityCodeMetadata()
		if err != nil {
			logger.Errorf("Cannot get facility code %v", err)
			return nil, errors.Wrap(err, "cannot construct VolumeProviderPacketImpl")
		}
		c := constructClient(config.AuthToken, config.BaseURL)
		facilities, resp, err := c.Facilities.List(&packngo.ListOptions{})
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

	provider := VolumeProviderPacketImpl{config, metadata}
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

func constructClient(authToken string, baseURL *string) *packngo.Client {
	tr := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}
	client := &http.Client{Transport: tr}

	// client.Transport = logging.NewTransport("Packet", client.Transport)
	if baseURL != nil {
		// really should handle error, but packngo does not distinguish now or handle errors, so ignoring for now
		client, _ := packngo.NewClientWithBaseURL(ConsumerToken, authToken, client, *baseURL)
		return client
	}
	return packngo.NewClientWithAuth(ConsumerToken, authToken, client)
}

// Client() returns a new client for accessing Packet's API.
func (p *VolumeProviderPacketImpl) client() *packngo.Client {
	return constructClient(p.config.AuthToken, p.config.BaseURL)
}

// ListVolumes wrap the packet api as an interface method
func (p *VolumeProviderPacketImpl) ListVolumes(options *packngo.ListOptions) ([]packngo.Volume, *packngo.Response, error) {
	listOptions := options
	if listOptions == nil {
		listOptions = &packngo.ListOptions{}
	}
	return p.client().Volumes.List(p.config.ProjectID, listOptions)
}

// Get wraps the packet api as an interface method
func (p *VolumeProviderPacketImpl) Get(volumeUUID string) (*packngo.Volume, *packngo.Response, error) {
	return p.client().Volumes.Get(volumeUUID, &packngo.GetOptions{Includes: []string{"attachments.volume", "attachments.device"}})
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
	// if the volume already is attached to a different node, reject it
	volume, httpResponse, err := p.client().Volumes.Get(volumeID, &packngo.GetOptions{})
	if err != nil || httpResponse.StatusCode != http.StatusOK {
		return nil, httpResponse, errors.Wrap(err, "prechecking existence of volume attachment")
	}
	// we only allow attaching to one node at a time
	switch len(volume.Attachments) {
	case 0:
		// not attached anywhere, so attach it
		return p.client().VolumeAttachments.Create(volumeID, deviceID)
	case 1:
		// attached to just one node, so it better be is
		attachment := volume.Attachments[0]
		if attachment.Device.ID == deviceID {
			return p.client().VolumeAttachments.Get(attachment.ID, &packngo.GetOptions{})
		}
		return nil, nil, WrongDeviceAttachmentError{deviceID: attachment.Device.ID}
	default:
		// attached to more than one node, that is an error
		devices := make([]string, 0)
		for _, a := range volume.Attachments {
			devices = append(devices, a.Device.ID)
		}
		return nil, nil, TooManyDevicesAttachedError{deviceIDs: devices}
	}
}

// Detach wraps the packet api as an interface method
func (p *VolumeProviderPacketImpl) Detach(attachmentID string) (*packngo.Response, error) {
	response, err := p.client().VolumeAttachments.Delete(attachmentID)
	// is this a "volume still attached" error? if so, indicate
	if err == nil {
		return response, err
	}
	errResponse, ok := err.(*packngo.ErrorResponse)
	if ok && response != nil && response.StatusCode == http.StatusUnprocessableEntity && len(errResponse.Errors) > 0 && strings.HasPrefix(errResponse.Errors[0], volumeInUseMessage) {
		return response, &DeviceStillAttachedError{}
	}
	return response, err
}

// GetNodes list nodes
func (p *VolumeProviderPacketImpl) GetNodes() ([]packngo.Device, *packngo.Response, error) {
	return p.client().Devices.List(p.config.ProjectID, &packngo.ListOptions{})
}
