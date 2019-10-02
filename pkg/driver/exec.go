package driver

import (
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
)

// generic execCommand function which logs on error
func execCommand(command string, args ...string) ([]byte, error) {
	out, err := exec.Command(command, args...).CombinedOutput()
	if err != nil {
		log.WithFields(log.Fields{"command": command, "args": strings.Join(args, " "), "out": string(out[:]), "error": err.Error()}).Error("Error")
		return nil, err
	}
	return out, nil
}
