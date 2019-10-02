package packet

import (
	"encoding/json"
	"time"

	"github.com/packethost/packngo"
)

const (
	// Gibi represents a Gibibyte
	Gibi int64 = 1024 * 1024 * 1024
	// MaxVolumeSizeGi maximum size in Gi
	MaxVolumeSizeGi = 10000
	// DefaultVolumeSizeGi default size in Gi
	DefaultVolumeSizeGi = 100
	// MinVolumeSizeGi minimum size in Gi
	MinVolumeSizeGi = 10
	// VolumePlanStandard standard plan name
	VolumePlanStandard = "standard"
	// VolumePlanStandardID standard plan ID
	VolumePlanStandardID = "87728148-3155-4992-a730-8d1e6aca8a32"
	// VolumePlanPerformance performance plan name
	VolumePlanPerformance = "performance"
	// VolumePlanPerformanceID performance plan ID
	VolumePlanPerformanceID = "d6570cfb-38fa-4467-92b3-e45d059bb249"
)

// VolumeProvider interface for a volume provider
type VolumeProvider interface {
	ListVolumes(*packngo.ListOptions) ([]packngo.Volume, *packngo.Response, error)
	Get(volumeID string) (*packngo.Volume, *packngo.Response, error)
	Delete(volumeID string) (*packngo.Response, error)
	Create(*packngo.VolumeCreateRequest) (*packngo.Volume, *packngo.Response, error)
	Attach(volumeID, deviceID string) (*packngo.VolumeAttachment, *packngo.Response, error)
	Detach(attachmentID string) (*packngo.Response, error)
	GetNodes() ([]packngo.Device, *packngo.Response, error)
}

// VolumeDescription description of characteristics of a volume
type VolumeDescription struct {
	Name    string
	Created time.Time
}

// String serialize a VolumeDescription to a string
func (desc VolumeDescription) String() string {
	serialized, err := json.Marshal(desc)
	if err != nil {
		return ""
	}
	return string(serialized)
}

// NewVolumeDescription create a new VolumeDescription from a given name
func NewVolumeDescription(name string) VolumeDescription {
	return VolumeDescription{
		Name:    name,
		Created: time.Now(),
	}
}

// ReadDescription read a serialized form of a VolumeDescription into a VolumeDescription struct
func ReadDescription(serialized string) (VolumeDescription, error) {
	desc := VolumeDescription{}
	err := json.Unmarshal([]byte(serialized), &desc)
	return desc, err
}

// VolumeReady determine if a volume is in the ready state after being created
func VolumeReady(volume *packngo.Volume) bool {
	return volume != nil && volume.State == "active"
}
