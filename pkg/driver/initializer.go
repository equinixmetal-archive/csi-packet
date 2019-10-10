package driver

import (
	"fmt"
	"io/ioutil"
)

const (
	initiatorNameFile = "/etc/iscsi/initiatorname.iscsi"
	mpathConfigFile   = "/etc/multipath.conf"
	mpathConfig       = `
defaults {
       polling_interval       3
       fast_io_fail_tmo 5
       path_selector              "round-robin 0"
       rr_min_io                    100
       rr_weight                    priorities
       failback                    immediate
       no_path_retry              queue
       user_friendly_names     yes
}
blacklist {
         devnode "^(ram|raw|loop|fd|md|dm-|sr|scd|st)[0-9]*"
         devnode "^hd[a-z][[0-9]*]"
         devnode "^vd[a-z]"
         devnode "^cciss!c[0-9]d[0-9]*[p[0-9]*]"
         device {
               vendor  "Micron"
               product ".*"
         }
         device {
               vendor  "Intel"
               product ".*"
         }
         device {
               vendor  "DELL"
               product ".*"
         }
}
devices {
        device {
                vendor "DATERA"
                product "IBLOCK"
                path_grouping_policy group_by_prio
                path_checker tur
                #checker_timer 5
                #prio_callout "/sbin/mpath_prio_alua /dev/%n"
                hardware_handler "1 alua"
        }
}
`
)

type Initializer interface {
	NodeInit(string) error
}

type InitializerImpl struct {
}

// NodeInit does all node initialization necessary for iscsi to be configured correctly
func (n *InitializerImpl) NodeInit(initiatorName string) error {
	if err := n.SetIscsiInitiator(initiatorName); err != nil {
		return err
	}
	if err := n.ConfigureMultipath(); err != nil {
		return err
	}
	if err := n.RestartServices(); err != nil {
		return err
	}
	return nil
}

// SetIscsiInitiator sets the name of the iscsi initiator
func (n *InitializerImpl) SetIscsiInitiator(initiatorName string) error {
	// get the name of our initiator
	// update the file
	contents := []byte(fmt.Sprintf("InitiatorName=%s\n", initiatorName))
	err := ioutil.WriteFile(initiatorNameFile, contents, 0644)
	if err != nil {
		return err
	}
	return nil
}

func (n *InitializerImpl) ConfigureMultipath() error {
	contents := []byte(mpathConfig)
	err := ioutil.WriteFile(mpathConfigFile, contents, 0644)
	if err != nil {
		return err
	}
	return nil
}

// RestartServices should restart services, but we have no way to do that yet
func (n *InitializerImpl) RestartServices() error {
	return nil
}
