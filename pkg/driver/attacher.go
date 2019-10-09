package driver

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	multipathTimeout  = 10 * time.Second
	multipathExec     = "/sbin/multipath"
	multipathBindings = "/etc/multipath/bindings"
	iscsiIface        = "kubernetescsi0"
)

// IscsiAdm interface provides methods of executing iscsi admin commands
type Attacher interface {
	// these interact with iscsiadm and the iscsi target
	Discover(string) error
	HasSession(string, string) (bool, error)
	Login(string, string) error
	Logout(string, string) error
	// these check locally on the local host
	GetScsiID(string) (string, error)
	GetDevice(string, string) (string, error)
	// these do multipath
	MultipathReadBindings() (map[string]string, map[string]string, error)
	MultipathWriteBindings(map[string]string) error
}

type AttacherImpl struct {
}

func (i *AttacherImpl) GetScsiID(devicePath string) (string, error) {
	args := []string{"-g", "-u", "-d", devicePath}
	out, err := execCommand("/lib/udev/scsi_id", args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// look for file that matches portal, iqn, look up what it links to
func (i *AttacherImpl) GetDevice(portal, iqn string) (string, error) {

	pattern := fmt.Sprintf("%s*%s*%s*", "/dev/disk/by-path/", portal, iqn)

	files, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("file not found for pattern %s", pattern)
	}

	file := files[0]
	finfo, err := os.Lstat(file)
	if err != nil {
		return "", err
	}
	if finfo.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("file %s is not a link", file)
	}
	source, err := filepath.EvalSymlinks(file)
	if err != nil {
		log.Errorf("cannot get symlink for %s", file)
		return "", err
	}
	return source, nil
}

func (i *AttacherImpl) Discover(ip string) error {
	// does the desired iface exist?
	args := []string{"--mode", "iface", "-o", "show"}
	out, err := execCommand("iscsiadm", args...)
	// if an error was returned, we cannot do much
	if err != nil {
		return fmt.Errorf("unable to list all ifaces: %v", err)
	}
	// parse through them to find the one we want
	pat, err := regexp.Compile(`(?m)^` + iscsiIface + ` .*`)
	if err != nil {
		return fmt.Errorf("error compiling pattern: %v", err)
	}
	found := pat.FindString(string(out))
	// if the iface does not exist, we must create it
	if found == "" {
		args := []string{"-I", iscsiIface, "--mode", "iface", "-o", "new"}
		_, err := execCommand("iscsiadm", args...)
		if err != nil {
			return fmt.Errorf("unable to create new iscsi iface %s: %v", iscsiIface, err)
		}
		// get the configs for the default, and then clone them, while overriding the initiator name
		args = []string{"-I", "default", "--mode", "iface", "-o", "show"}
		out, err := execCommand("iscsiadm", args...)
		if err != nil {
			return fmt.Errorf("unable to get parameters for default iface: %v", err)
		}
		params, err := parseIscsiIfaceShow(string(out))
		if err != nil {
			return fmt.Errorf("unable to parse parameters for default iface: %v", err)
		}
		// THIS IS WRONG! this should be set to the correct initiator from metadata
		params["iface.initiatorname"] = iscsiIface
		// update new iface records
		for key, val := range params {
			args := []string{"-I", iscsiIface, "--mode", "iface", "-o", "update", "-n", key, "-v", val}
			_, err = execCommand("iscsiadm", args...)
			if err != nil {
				return fmt.Errorf("unable to set parameter %s for iscsi iface %s: %v", key, iscsiIface, err)
			}
		}
		// now we can use it
	}

	args = []string{"-I", iscsiIface, "--mode", "discovery", "--portal", ip, "--type", "sendtargets", "--discover"}
	_, err = execCommand("iscsiadm", args...)
	return err
}

// HasSession checks to see if the session exists, may log an extraneous error if the seesion does not exist
func (i *AttacherImpl) HasSession(ip, iqn string) (bool, error) {
	args := []string{"-I", iscsiIface, "--mode", "session"}
	out, err := execCommand("iscsiadm", args...)
	if err != nil {
		return false, nil // this is almost certainly "No active sessions"
	}
	pat, err := regexp.Compile(ip + ".*" + iqn)
	if err != nil {
		return false, err
	}
	lines := strings.Split(string(out[:]), "\n")
	for _, line := range lines {
		found := pat.FindString(line)
		if found != "" {
			return true, nil
		}
	}
	return false, nil
}

func (i *AttacherImpl) Login(ip, iqn string) error {
	hasSession, err := i.HasSession(ip, iqn)
	if err != nil {
		return err
	}
	if hasSession {
		return nil
	}
	args := []string{"-I", iscsiIface, "--mode", "node", "--portal", ip, "--targetname", iqn, "--login"}
	_, err = execCommand("iscsiadm", args...)
	return err
}

func (i *AttacherImpl) Logout(ip, iqn string) error {
	hasSession, err := i.HasSession(ip, iqn)
	if err != nil {
		return err
	}
	if !hasSession {
		return nil
	}
	args := []string{"-I", iscsiIface, "--mode", "node", "--portal", ip, "--targetname", iqn, "--logout"}
	_, err = execCommand("iscsiadm", args...)
	return err
}

// read the bindings from /etc/multipath/bindings
// separating into keep/discard sets
// return elements map from volume name to scsi id
func (i *AttacherImpl) MultipathReadBindings() (map[string]string, map[string]string, error) {

	var bindings = map[string]string{}
	var discard = map[string]string{}

	if _, err := os.Stat(multipathBindings); err != nil {
		if os.IsNotExist(err) {
			// file does not exist
			return bindings, discard, nil
		}
		return nil, nil, err
	}

	f, err := os.Open(multipathBindings)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) > 0 && line[0] != '#' {
			elements := strings.Fields(line)
			if len(elements) == 2 {

				if strings.HasPrefix(elements[0], "mpath") {
					discard[elements[0]] = elements[1]
				} else {
					bindings[elements[0]] = elements[1]
				}
			}
		}
	}

	return bindings, discard, nil
}

// write the bindings to /etc/multipath/bindings
func (i *AttacherImpl) MultipathWriteBindings(bindings map[string]string) error {

	f, err := os.Create(multipathBindings)
	if err != nil {
		return err
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	for name, id := range bindings {
		writer.WriteString(fmt.Sprintf("%s %s\n", name, id))
	}
	writer.Flush()
	return nil
}

// multipath hangs when run inside a container, but is safe to terminate
func multipath(args ...string) (string, error) {

	ctx, cancel := context.WithTimeout(context.Background(), multipathTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, multipathExec, args...)

	output, err := cmd.Output()

	if ctx.Err() == context.DeadlineExceeded {
		log.WithFields(log.Fields{"timeout": multipathTimeout, "args": strings.Join(args, " ")}).Info("multipath timed out")
		return string(output), nil
	}

	return string(output), err
}

// taken unabashedly from kubernetes kubelet iscsi volume driver, which is licensed Apache 2.0
// see https://github.com/kubernetes/kubernetes/blob/ce42bc382e38dd7cd233b8a350723287f6e79f82/pkg/volume/iscsi/iscsi_util.go
func parseIscsiIfaceShow(data string) (map[string]string, error) {
	params := make(map[string]string)
	slice := strings.Split(data, "\n")
	for _, line := range slice {
		if !strings.HasPrefix(line, "iface.") || strings.Contains(line, "<empty>") {
			continue
		}
		iface := strings.Fields(line)
		if len(iface) != 3 || iface[1] != "=" {
			return nil, fmt.Errorf("Error: invalid iface setting: %v", iface)
		}
		// iscsi_ifacename is immutable once the iface is created
		if iface[0] == "iface.iscsi_ifacename" {
			continue
		}
		params[iface[0]] = iface[2]
	}
	return params, nil
}
