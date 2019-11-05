package driver

import (
	"fmt"
	"io/ioutil"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/kubernetes-csi/csi-test/pkg/sanity"
	"github.com/packethost/csi-packet/pkg/packet"
	packetServer "github.com/packethost/packet-api-server/pkg/server"
	"github.com/packethost/packet-api-server/pkg/store"

	log "github.com/sirupsen/logrus"
)

const (
	socket     = "/tmp/csi.sock"
	authToken  = "AUTH_TOKEN"
	projectID  = "123456"
	facilityID = "EWR1"
	nodeName   = "node-sanity-test"
	driverName = "sanity-test"
)

type apiServerError struct {
	t *testing.T
}

func (a *apiServerError) Error(err error) {
	a.t.Fatal(err)
}
func TestPacketDriver(t *testing.T) {
	// where we will connect
	endpoint := "unix://" + socket
	// remove any existing one
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove unix domain socket file %s, error: %s", socket, err)
	}
	defer os.Remove(socket)

	backend := store.NewMemory()
	dev, err := backend.CreateDevice(projectID, nodeName)
	if err != nil {
		t.Fatalf("error creating device: %v", err)
	}
	// mock endpoint
	fake := packetServer.PacketServer{
		Store: backend,
		ErrorHandler: &apiServerError{
			t: t,
		},
		MetadataDevice: dev.ID,
	}
	ts := httptest.NewServer(fake.CreateHandler())
	defer ts.Close()

	url, _ := url.Parse(ts.URL)
	urlString := url.String()

	// Setup the full driver and its environment
	// normally we care about all of these settings, but since this all is stubbed out, it does not matter
	config := packet.Config{
		AuthToken:   authToken,
		ProjectID:   projectID,
		FacilityID:  facilityID,
		BaseURL:     &urlString,
		MetadataURL: &urlString,
	}
	driver := &PacketDriver{
		endpoint: endpoint,
		name:     driverName,
		nodeID:   nodeName,
		config:   config,
		Logger:   log.WithFields(log.Fields{"node": nodeName, "endpoint": endpoint}),
		Attacher: &AttacherMock{
			sessions:  map[string]iscsiSession{},
			bindings:  map[string]string{},
			maxDevice: 0,
		},
		Mounter: &MounterMock{
			bindmounts:  map[string]string{},
			blockmounts: map[string]string{},
		},
		Initializer: &InitializerMock{},
	}
	defer driver.Stop()

	// run the driver
	go driver.Run()

	mntDir, err := ioutil.TempDir("", "mnt")
	if err != nil {
		t.Fatal(err)
	}
	// we remove them now, because sanity expects to be given the parent where to create a directory, and fails when it already exists
	os.RemoveAll(mntDir)
	defer os.RemoveAll(mntDir)

	mntStageDir, err := ioutil.TempDir("", "mnt-stage")
	if err != nil {
		t.Fatal(err)
	}
	// we remove them now, because sanity expects to be given the parent where to create a directory, and fails when it already exists
	os.RemoveAll(mntStageDir)
	defer os.RemoveAll(mntStageDir)

	sanityConfig := &sanity.Config{
		TargetPath:  mntDir,
		StagingPath: mntStageDir,
		Address:     endpoint,
	}

	// call the test suite
	sanity.Test(t, sanityConfig)
}

/*****
mock for attacher (iscsiadm) and mounter commands
*****/
type iscsiSession struct {
	ip  string
	iqn string
	dev string
	id  string
}
type AttacherMock struct {
	sessions  map[string]iscsiSession
	maxDevice int
	bindings  map[string]string
}

func (a *AttacherMock) sessionName(ip, iqn string) string {
	return fmt.Sprintf("%s %s", ip, iqn)
}
func (a *AttacherMock) GetScsiID(devicePath string) (string, error) {
	for _, v := range a.sessions {
		if v.dev == devicePath {
			return v.id, nil
		}
	}
	return "", fmt.Errorf("device %s not found", devicePath)
}
func (a *AttacherMock) GetDevice(portal, iqn string) (string, error) {
	if v, ok := a.sessions[a.sessionName(portal, iqn)]; ok {
		return v.dev, nil
	}
	return "", fmt.Errorf("device %s %s not found", portal, iqn)
}
func (a *AttacherMock) Discover(ip, initiator string) error {
	return nil
}
func (a *AttacherMock) HasSession(ip, iqn string) (bool, error) {
	if _, ok := a.sessions[a.sessionName(ip, iqn)]; ok {
		return true, nil
	}
	return false, nil
}
func (a *AttacherMock) Login(ip, iqn string) error {
	a.maxDevice++
	a.sessions[a.sessionName(ip, iqn)] = iscsiSession{
		ip:  ip,
		iqn: iqn,
		dev: fmt.Sprintf("/dev/%d", a.maxDevice), // create a device for it
		id:  uuid.New().String(),
	}
	return nil
}
func (a *AttacherMock) Logout(ip, iqn string) error {
	delete(a.sessions, a.sessionName(ip, iqn))
	return nil
}
func (a *AttacherMock) MultipathReadBindings() (map[string]string, map[string]string, error) {
	return a.bindings, map[string]string{}, nil
}
func (a *AttacherMock) MultipathWriteBindings(bindings map[string]string) error {
	a.bindings = bindings
	return nil
}

type MounterMock struct {
	bindmounts  map[string]string // maps target to src
	blockmounts map[string]string // maps target to device
}

func (m *MounterMock) Bindmount(src, target string) error {
	m.bindmounts[target] = src
	return nil
}
func (m *MounterMock) Unmount(path string) error {
	delete(m.bindmounts, path)
	delete(m.blockmounts, path)
	return nil
}
func (m *MounterMock) MountMappedDevice(device, target string) error {
	m.blockmounts[target] = device
	return nil
}
func (m *MounterMock) FormatMappedDevice(device string) error {
	// we do not do anything here
	return nil
}
func (m *MounterMock) GetMappedDevice(device string) (BlockInfo, error) {
	return BlockInfo{
		Name:       "name",
		FsType:     "ext4",
		Label:      "label",
		UUID:       uuid.New().String(),
		Mountpoint: "/mnt/foo",
	}, nil
}

type InitializerMock struct {
}

func (i *InitializerMock) NodeInit(initiatorName string) error {
	return nil
}
