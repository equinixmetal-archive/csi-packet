package packet

import (
	"encoding/json"
	"time"

	"github.com/packethost/packngo"
)

const (
	GB                      int64 = 1024 * 1024 * 1024
	MaxVolumeSizeGb               = 10000
	DefaultVolumeSizeGb           = 100
	MinVolumeSizeGb               = 10
	VolumePlanStandard            = "standard"
	VolumePlanStandardID          = "87728148-3155-4992-a730-8d1e6aca8a32"
	VolumePlanPerformance         = "performance"
	VolumePlanPerformanceID       = "d6570cfb-38fa-4467-92b3-e45d059bb249"
)

type VolumeProvider interface {
	ListVolumes() ([]packngo.Volume, *packngo.Response, error)
	Get(volumeID string) (*packngo.Volume, *packngo.Response, error)
	Delete(volumeID string) (*packngo.Response, error)
	Create(*packngo.VolumeCreateRequest) (*packngo.Volume, *packngo.Response, error)
	Attach(volumeID, deviceID string) (*packngo.VolumeAttachment, *packngo.Response, error)
	Detach(attachmentID string) (*packngo.Response, error)
	GetNodes() ([]packngo.Device, *packngo.Response, error)
}

type VolumeDescription struct {
	Name    string
	Created time.Time
}

func (desc VolumeDescription) String() string {
	serialized, err := json.Marshal(desc)
	if err != nil {
		return ""
	}
	return string(serialized)
}

func NewVolumeDescription(name string) VolumeDescription {
	return VolumeDescription{
		Name:    name,
		Created: time.Now(),
	}
}

func ReadDescription(serialized string) (VolumeDescription, error) {
	desc := VolumeDescription{}
	err := json.Unmarshal([]byte(serialized), &desc)
	return desc, err
}

type NodeVolumeManager interface{}
