package driver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	log "github.com/sirupsen/logrus"
)

// represents the lsblk info
type BlockInfo struct {
	Name       string `json:"name"`
	FsType     string `json:"fstype"`
	Label      string `json:"label"`
	UUID       string `json:"uuid"`
	Mountpoint string `json:"mountpoint"`
}

// represents the lsblk info
type Deviceset struct {
	BlockDevices []BlockInfo `json:"blockdevices"`
}

type Mounter interface {
	Bindmount(string, string) error
	Unmount(string) error
	MountMappedDevice(string, string) error
	FormatMappedDevice(string) error
	GetMappedDevice(string) (BlockInfo, error)
}

type MounterImpl struct {
}

// Methods to format and mount

func (m *MounterImpl) Bindmount(src, target string) error {

	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(target, 0755)
		} else {
			log.Errorf("stat %s, %v", target, err)
			return err
		}
	}
	_, err := os.Stat(target)
	if err != nil {
		log.Errorf("stat %s, %v", target, err)
		return err
	}
	args := []string{"--bind", src, target}
	_, err = execCommand("mount", args...)
	return err
}

func (m *MounterImpl) Unmount(path string) error {
	err := unix.Unmount(path, 0)
	// we are willing to pass on a directory that is not mounted any more
	if err != nil && err != unix.EINVAL {
		return err
	}
	return nil
}

func (m *MounterImpl) MountMappedDevice(device, target string) error {
	devicePath := filepath.Join("/dev/mapper/", device)
	os.MkdirAll(target, os.ModeDir)
	args := []string{"-t", "ext4", "--source", devicePath, "--target", target}
	_, err := execCommand("mount", args...)
	return err
}

// ext4 format
func (m *MounterImpl) FormatMappedDevice(device string) error {
	devicePath := filepath.Join("/dev/mapper/", device)
	args := []string{"-F", devicePath}
	fstype := "ext4"
	command := "mkfs." + fstype
	_, err := execCommand(command, args...)
	return err
}

// get info
func (m *MounterImpl) GetMappedDevice(device string) (BlockInfo, error) {
	devicePath := filepath.Join("/dev/mapper/", device)

	// testing issue: must mock out call to Stat as well as to exec.Command
	if _, err := os.Stat(devicePath); os.IsNotExist(err) {
		return BlockInfo{}, err
	}

	// use -J json output so we can parse it into a BlockInfo struct
	out, err := execCommand("lsblk", "-J", "-i", "--output", "NAME,FSTYPE,LABEL,UUID,MOUNTPOINT", devicePath)
	if err != nil {
		return BlockInfo{}, err
	}
	devices := Deviceset{}
	err = json.Unmarshal(out, &devices)
	if err != nil {
		return BlockInfo{}, err
	}
	for _, info := range devices.BlockDevices {
		if info.Name == device {
			return info, nil
		}
	}
	return BlockInfo{}, fmt.Errorf("device %s not found", device)
}
