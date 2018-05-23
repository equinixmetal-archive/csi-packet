package driver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang/glog"
)

// Methods to format and mount

func bindmountFs(src, target string) error {

	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(target, 0755)
		} else {
			glog.V(5).Infof("stat %s, %v", target, err)
			return err
		}
	}
	_, err := os.Stat(target)
	if err != nil {
		glog.V(5).Infof("stat %s, %v", target, err)
		return err
	}
	args := []string{"--bind", src, target}
	_, err = execCommand("mount", args...)
	return err
}

func unmountFs(path string) error {
	args := []string{path}
	_, err := execCommand("umount", args...)
	return err
}

func mountMappedDevice(device, target string) error {
	devicePath := filepath.Join("/dev/mapper/", device)
	args := []string{"-t", "ext4", "--source", devicePath, "--target", target}
	_, err := execCommand("mount", args...)
	return err
}

// etx4 format
func formatMappedDevice(device string) error {
	devicePath := filepath.Join("/dev/mapper/", device)
	args := []string{"-F", devicePath}
	fstype := "ext4"
	command := "mkfs." + fstype
	_, err := execCommand(command, args...)
	return err
}

// represents the lsblk info
type blockInfo struct {
	Name       string `json:"name"`
	FsType     string `json:"fstype"`
	Label      string `json:"label"`
	UUID       string `json:"uuid"`
	Mountpoint string `json:"mountpoint"`
}

// represents the lsblk info
type deviceset struct {
	BlockDevices []blockInfo `json:"blockdevices"`
}

// get info

func getMappedDevice(device string) (blockInfo, error) {
	devicePath := filepath.Join("/dev/mapper/", device)

	// testing issue: must mock out call to Stat as well as to exec.Command
	if _, err := os.Stat(devicePath); os.IsNotExist(err) {
		return blockInfo{}, err
	}

	// use -J json output so we can parse it into a blockInfo struct
	out, err := execCommand("lsblk", "-J", "-i", "--output", "NAME,FSTYPE,LABEL,UUID,MOUNTPOINT", devicePath)
	if err != nil {
		return blockInfo{}, err
	}
	devices := deviceset{}
	err = json.Unmarshal(out, &devices)
	if err != nil {
		return blockInfo{}, err
	}
	for _, info := range devices.BlockDevices {
		if info.Name == device {
			return info, nil
		}
	}
	return blockInfo{}, fmt.Errorf("device %s not found", device)
}
