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
)

// IscsiAdm interface provides methods of executing iscsi admin commands
type Attacher interface {
	GetScsiID(string) (string, error)
	GetDevice(string, string) (string, error)
	Discover(string) error
	HasSession(string, string) (bool, error)
	Login(string, string) error
	Logout(string, string) error
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
	args := []string{"--mode", "discovery", "--portal", ip, "--type", "sendtargets", "--discover"}
	_, err := execCommand("iscsiadm", args...)
	return err
}

// HasSession checks to see if the session exists, may log an extraneous error if the seesion does not exist
func (i *AttacherImpl) HasSession(ip, iqn string) (bool, error) {
	args := []string{"--mode", "session"}
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
	args := []string{"--mode", "node", "--portal", ip, "--targetname", iqn, "--login"}
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
	args := []string{"--mode", "node", "--portal", ip, "--targetname", iqn, "--logout"}
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
