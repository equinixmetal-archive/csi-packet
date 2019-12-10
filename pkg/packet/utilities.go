package packet

import (
	"net"
	"strconv"

	"github.com/packethost/packngo/metadata"
	"github.com/pkg/errors"
)

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

// CapacityMetaData exists for parsing json metadata
type CapacityMetaData struct {
	Size string `json:"size"`
	Unit string `json:"unit"`
}

// VolumeMetadata exists for parsing json metadata
type VolumeMetadata struct {
	Name     string           `json:"name"`
	IPs      []net.IP         `json:"ips"`
	Capacity CapacityMetaData `json:"capacity"`
	IQN      string           `json:"iqn"`
}

type MetadataDriver struct {
	BaseURL *string
}

// GetVolumeMetadata get all the metadata, extract only the parsed volume information, select the desired volume
func (m *MetadataDriver) GetVolumeMetadata(volumeName string) (VolumeMetadata, error) {
	empty := VolumeMetadata{}
	volumeInfo, err := m.packngoGetPacketVolumeMetadata(volumeName)
	if err != nil {
		return empty, err
	}

	volumeMetaData := VolumeMetadata{
		Name: volumeInfo.Name,
		IPs:  volumeInfo.IPs,
		IQN:  volumeInfo.IQN,
		Capacity: CapacityMetaData{
			Size: strconv.Itoa(volumeInfo.Capacity.Size),
			Unit: volumeInfo.Capacity.Unit,
		},
	}

	return volumeMetaData, nil
}

// GetFacilityCodeMetadata get all the metadata, return the facility code
func (m *MetadataDriver) GetFacilityCodeMetadata() (string, error) {
	device, err := m.getMetadata()
	if err != nil {
		return "", err
	}

	return device.Facility, nil
}

// GetInitiator get the initiator name for iscsi
func (m *MetadataDriver) GetInitiator() (string, error) {
	device, err := m.getMetadata()
	if err != nil {
		return "", err
	}

	return device.IQN, nil
}

// GetNodeID get the official packet node ID
func (m *MetadataDriver) GetNodeID() (string, error) {
	device, err := m.getMetadata()
	if err != nil {
		return "", err
	}

	return device.ID, nil
}

// use this when packngo serialization is fixed
// GetVolumeMetadata gets the volume metadata for a named volume
func (m *MetadataDriver) packngoGetPacketVolumeMetadata(volumeName string) (metadata.VolumeInfo, error) {
	device, err := m.getMetadata()
	if err != nil {
		return metadata.VolumeInfo{}, err
	}
	// logrus.Infof("device metadata: %+v", device)

	var volumeMetaData = metadata.VolumeInfo{}

	for _, vdata := range device.Volumes {
		if vdata.Name == volumeName {
			volumeMetaData = vdata
			break
		}
	}

	if volumeMetaData.Name == "" {
		return metadata.VolumeInfo{}, errors.Errorf("volume %s not found in metadata", volumeName)
	}

	return volumeMetaData, nil
}

// use this when packngo serialization is fixed
// GetFacilityCodeMetadata returns the facility code
func (m *MetadataDriver) packngoGetPacketFacilityCodeMetadata() (string, error) {

	device, err := m.getMetadata()
	if err != nil {
		return "", err
	}

	return device.Facility, nil
}

func (m *MetadataDriver) getMetadata() (*metadata.CurrentDevice, error) {
	if m.BaseURL == nil {
		return metadata.GetMetadata()
	}
	return metadata.GetMetadataFromURL(*m.BaseURL)
}
