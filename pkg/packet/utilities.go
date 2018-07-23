package packet

import (
	"net"

	"github.com/packethost/packngo/metadata"
	"github.com/pkg/errors"

	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
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

// PacketCapacityMetaData exists for parsing json metadata
type PacketCapacityMetaData struct {
	Size string `json:"size"`
	Unit string `json:"unit"`
}

// PacketVolumeMetadata exists for parsing json metadata
type PacketVolumeMetadata struct {
	Name     string                 `json:"name"`
	IPs      []net.IP               `json:"ips"`
	Capacity PacketCapacityMetaData `json:"capacity"`
	IQN      string                 `json:"iqn"`
}

// get all the metadata, extract only the parsed volume information, select the desired volume
func GetPacketVolumeMetadata(volumeName string) (PacketVolumeMetadata, error) {

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

// get all the metadata, return the facility code
func GetPacketFacilityCodeMetadata() (string, error) {

	res, err := http.Get("https://metadata.packet.net/metadata")
	if err != nil {
		return "", err
	}

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	allData := map[string]interface{}{}
	err = json.Unmarshal([]byte(body), &allData)
	if err != nil {
		return "", err
	}

	facilityCode, ok := allData["facility"].(string)
	if ok {
		return facilityCode, nil
	}
	return "", fmt.Errorf("Unable to read facility code")
}

// use this when packngo serialization is fixed
// GetPacketVolumeMetadata gets the volume metadata for a named volume
func packngoGetPacketVolumeMetadata(volumeName string) (metadata.VolumeInfo, error) {
	device, err := metadata.GetMetadata()
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
// GetPacketFacilityCodeMetadata returns the facility code
func packngoGetPacketFacilityCodeMetadata() (string, error) {

	device, err := metadata.GetMetadata()
	if err != nil {
		return "", err
	}

	return device.Facility, nil
}
