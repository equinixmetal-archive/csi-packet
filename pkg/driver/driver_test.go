package driver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/kubernetes-csi/csi-test/pkg/sanity"
	"github.com/packethost/csi-packet/pkg/packet"
	"github.com/packethost/packngo"
	"github.com/packethost/packngo/metadata"

	log "github.com/sirupsen/logrus"
)

const (
	socket     = "/tmp/csi.sock"
	authToken  = "AUTH_TOKEN"
	projectID  = "123456"
	facilityID = "EWR1"
	nodeName   = "node-sanity-test"
	driverName = "sanity-test"
	iqn        = "iqn.2013-05.com.daterainc:tc:01:sn:73d3e29022fddba4"
	ip1        = "10.144.32.8"
	ip2        = "10.144.48.8"
)

func TestPacketDriver(t *testing.T) {
	// where we will connect
	endpoint := "unix://" + socket
	// remove any existing one
	if err := os.Remove(socket); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to remove unix domain socket file %s, error: %s", socket, err)
	}
	defer os.Remove(socket)

	// mock endpoint
	fake := &fakeAPI{
		volumes: map[string]*packngo.Volume{},
		t:       t,
	}
	ts := httptest.NewServer(fake.createHandler())
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

// handler for the httptest server
type fakeAPI struct {
	volumes map[string]*packngo.Volume
	t       *testing.T
}

func (f *fakeAPI) createHandler() http.Handler {
	r := mux.NewRouter()
	// list all facilities
	r.HandleFunc("/facilities", f.allFacilitiesHandler).Methods("GET")
	// get all volumes for a project
	r.HandleFunc("/projects/{projectID}/storage", f.listVolumesHandler).Methods("GET")
	// get information about a specific volume
	r.HandleFunc("/storage/{volumeID}", f.getVolumeHandler).Methods("GET")
	// create a volume for a project
	r.HandleFunc("/projects/{projectID}/storage", f.createVolumeHandler).Methods("POST")
	// delete a volume
	r.HandleFunc("/storage/{volumeID}", f.deleteVolumeHandler).Methods("DELETE")
	// attach a volume to a host
	r.HandleFunc("/storage/{volumeID}/attachments", f.volumeAttachHandler).Methods("POST")
	// detach a volume from a host
	r.HandleFunc("/storage/attachments/{volumeID}", f.volumeDetachHandler).Methods("DELETE")
	// get all devices for a project
	r.HandleFunc("/projects/{projectID}/devices", f.listDevicesHandler).Methods("GET")
	// handle metadata requests
	r.HandleFunc("/metadata", f.metadataHandler).Methods("GET")
	return r
}

// list all facilities
func (f *fakeAPI) allFacilitiesHandler(w http.ResponseWriter, r *http.Request) {
	var resp = struct {
		facilities []*packngo.Facility
	}{
		facilities: []*packngo.Facility{
			{ID: "e1e9c52e-a0bc-4117-b996-0fc94843ea09", Name: "Parsippany, NJ", Code: "ewr1"},
		},
	}
	err := json.NewEncoder(w).Encode(&resp)
	if err != nil {
		f.t.Fatal(err)
	}
	return
}

// list all devices for a project
func (f *fakeAPI) listDevicesHandler(w http.ResponseWriter, r *http.Request) {
	devices := []*packngo.Device{
		{DeviceRaw: packngo.DeviceRaw{
			ID:       uuid.New().String(),
			Hostname: nodeName,
			State:    "active",
		}},
	}
	var resp = struct {
		Devices []*packngo.Device `json:"devices"`
	}{
		Devices: devices,
	}
	err := json.NewEncoder(w).Encode(&resp)
	if err != nil {
		f.t.Fatal(err)
	}
	return
}

// list all volumes for a project
func (f *fakeAPI) listVolumesHandler(w http.ResponseWriter, r *http.Request) {
	var err error
	// by default, send all of the volumes
	count := len(f.volumes)
	// were we asked to limit it?
	perPage, ok := r.URL.Query()["per_page"]
	if ok && len(perPage) > 0 && perPage[0] != "" {
		count, err = strconv.Atoi(perPage[0])
		// any error converting should be returned
		if err != nil {
			w.WriteHeader(http.StatusOK)
			resp := struct {
				Error string `json:"error"`
			}{
				Error: fmt.Sprintf("invalid per_page query parameter: %s", perPage[0]),
			}
			err = json.NewEncoder(w).Encode(&resp)
			if err != nil {
				f.t.Fatal(err)
			}
			return
		}
	}
	vols := make([]*packngo.Volume, 0, count)
	for _, v := range f.volumes {
		if len(vols) >= count {
			break
		}
		vols = append(vols, v)
	}
	var resp = struct {
		Volumes []*packngo.Volume `json:"volumes"`
	}{
		Volumes: vols,
	}
	err = json.NewEncoder(w).Encode(&resp)
	if err != nil {
		f.t.Fatal(err)
	}
	return
}

// get information about a specific volume
func (f *fakeAPI) getVolumeHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	volID := vars["volumeID"]
	if vol, ok := f.volumes[volID]; ok {
		err := json.NewEncoder(w).Encode(&vol)
		if err != nil {
			f.t.Fatal(err)
		}
		return
	}
	http.NotFound(w, r)
	return
}

// delete a volume
func (f *fakeAPI) deleteVolumeHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	volID := vars["volumeID"]
	if _, ok := f.volumes[volID]; ok {
		delete(f.volumes, volID)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.NotFound(w, r)
	return
}

// create a volume
func (f *fakeAPI) createVolumeHandler(w http.ResponseWriter, r *http.Request) {
	// get the info from the body
	decoder := json.NewDecoder(r.Body)
	var cvr packngo.VolumeCreateRequest
	err := decoder.Decode(&cvr)
	if err != nil {
		f.t.Fatal(err)
	}

	// just create it
	uuid := uuid.New().String()
	vol := packngo.Volume{
		ID:          uuid,
		Name:        packet.VolumeIDToName(uuid),
		Description: cvr.Description,
		Size:        cvr.Size,
		State:       "active",
		Plan:        &packngo.Plan{ID: cvr.PlanID},
	}
	f.volumes[uuid] = &vol
	w.WriteHeader(http.StatusCreated)
	err = json.NewEncoder(w).Encode(&vol)
	if err != nil {
		f.t.Fatal(err)
	}
	return
}

// attach volume
func (f *fakeAPI) volumeAttachHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	volID := vars["volumeID"]
	// get the device from the body
	decoder := json.NewDecoder(r.Body)
	var attachRequest struct {
		Device string `json:"device_id"`
	}
	err := decoder.Decode(&attachRequest)
	if err != nil {
		f.t.Fatal(err)
	}

	// record the attachment
	for _, vol := range f.volumes {
		if vol.ID == volID {
			uuid := uuid.New().String()
			attachment := packngo.VolumeAttachment{
				ID:     uuid,
				Device: packngo.Device{DeviceRaw: packngo.DeviceRaw{ID: attachRequest.Device}},
				Volume: *vol,
			}
			vol.Attachments = []*packngo.VolumeAttachment{&attachment}
			w.WriteHeader(http.StatusOK)
			err = json.NewEncoder(w).Encode(&attachment)
			if err != nil {
				f.t.Fatal(err)
			}
			return
		}
	}
	http.NotFound(w, r)
	return
}

// detach volume
func (f *fakeAPI) volumeDetachHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// metadata handler
func (f *fakeAPI) metadataHandler(w http.ResponseWriter, r *http.Request) {
	vols := make([]*metadata.VolumeInfo, 0, len(f.volumes))
	for _, v := range f.volumes {
		vols = append(vols, &metadata.VolumeInfo{
			Name: fmt.Sprintf("volume-%s", v.ID[:8]),
			IQN:  iqn,
			IPs:  []net.IP{net.ParseIP(ip1), net.ParseIP(ip2)},
			Capacity: struct {
				Size int    `json:"size,string"`
				Unit string `json:"unit"`
			}{
				Size: v.Size,
				Unit: "gb",
			},
		})
	}
	var resp = struct {
		Volumes []*metadata.VolumeInfo `json:"volumes"`
	}{
		Volumes: vols,
	}
	err := json.NewEncoder(w).Encode(&resp)
	if err != nil {
		f.t.Fatal(err)
	}
	return
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
func (a *AttacherMock) Discover(ip string) error {
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
