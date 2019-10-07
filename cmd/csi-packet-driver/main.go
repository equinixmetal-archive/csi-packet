/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/packethost/csi-packet/pkg/driver"
	"github.com/packethost/csi-packet/pkg/packet"
	"github.com/packethost/csi-packet/pkg/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	endpoint       string
	nodeID         string
	providerConfig string
)

const (
	apiKeyName     = "PACKET_API_KEY"
	projectIDName  = "PACKET_PROJECT_ID"
	facilityIDName = "PACKET_FACILITY_ID"
)

func init() {
	flag.Set("logtostderr", "true")

	// Log as JSON instead of the default text.
	log.SetFormatter(&log.JSONFormatter{})

	// Output to stdout instead of the default stderr
	log.SetOutput(os.Stdout)

	log.SetLevel(log.DebugLevel)
}

func main() {
	// log our starting point
	log.WithFields(log.Fields{"version": version.VERSION}).Info("started")

	flag.CommandLine.Parse([]string{})

	cmd := &cobra.Command{
		Use:   "Packet",
		Short: "CSI Packet driver",
		Run: func(cmd *cobra.Command, args []string) {
			handle()
		},
	}

	cmd.Flags().AddGoFlagSet(flag.CommandLine)

	cmd.PersistentFlags().StringVar(&nodeID, "nodeid", "", "node id")
	cmd.MarkPersistentFlagRequired("nodeid")

	cmd.PersistentFlags().StringVar(&endpoint, "endpoint", "", "CSI endpoint")
	cmd.MarkPersistentFlagRequired("endpoint")

	cmd.PersistentFlags().StringVar(&providerConfig, "config", "", "path to provider config file")

	cmd.ParseFlags(os.Args[1:])
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s", err.Error())
		os.Exit(1)
	}

	os.Exit(0)
}

func handle() {
	// create our config, as needed
	var config, rawConfig packet.Config
	if providerConfig != "" {
		configBytes, err := ioutil.ReadFile(providerConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get read configuration file at path %s: %v\n", providerConfig, err)
			os.Exit(1)
		}
		err = json.Unmarshal(configBytes, &rawConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to process json of configuration file at path %s: %v\n", providerConfig, err)
			os.Exit(1)
		}
	}

	// read env vars; if not set, use rawConfig
	apiToken := os.Getenv(apiKeyName)
	if apiToken == "" {
		apiToken = rawConfig.AuthToken
	}
	config.AuthToken = apiToken

	projectID := os.Getenv(projectIDName)
	if projectID == "" {
		projectID = rawConfig.ProjectID
	}
	config.ProjectID = projectID

	facilityID := os.Getenv(facilityIDName)
	if facilityID == "" {
		facilityID = rawConfig.FacilityID
	}
	config.FacilityID = facilityID

	d, err := driver.NewPacketDriver(endpoint, nodeID, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get packet driver: %v\n", err)
		os.Exit(1)
	}
	d.Run()
}
