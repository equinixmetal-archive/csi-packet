# Kubernetes Container Storage Interface (CSI) plugin for Equinix Metal

[![GitHub release](https://img.shields.io/github/release/packethost/csi-packet/all.svg?style=flat-square)](https://github.com/packethost/csi-packet/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/packethost/csi-packet)](https://goreportcard.com/report/github.com/packethost/csi-packet)
![Continuous Integration](https://github.com/packethost/csi-packet/workflows/Continuous%20Integration/badge.svg)
[![Docker Pulls](https://img.shields.io/docker/pulls/packethost/csi-packet.svg)](https://hub.docker.com/r/packethost/csi-packet/)
[![Slack](https://slack.equinixmetal.com/badge.svg)](https://slack.equinixmetal.com)
[![Twitter Follow](https://img.shields.io/twitter/follow/equinixmetal.svg?style=social&label=Follow)](https://twitter.com/intent/follow?screen_name=equinixmetal)
[![End of Life](https://img.shields.io/badge/Stability-EndOfLife-black.svg)](https://github.com/packethost/standards/blob/main/end-of-life-statement.md#end-of-life-statements)

`csi-packet` was the Kubernetes CSI implementation for [Equinix Metal](https://metal.equinix.com/) Block Storage provided by [Datera](https://datera.io/). Read more about the CSI standard [here](https://kubernetes-csi.github.io/docs/).

This repository is [End-Of-Life](https://github.com/packethost/standards/blob/main/end-of-life-statement.md) meaning that this software is no longer supported nor maintained by Equinix Metal or its community.

*_The following information is obsolete. Please see <https://metal.equinix.com/developers/docs/kubernetes/kubernetes-on-equinix-metal/#storage> for alternatives._*

---

**If you have any queries about CSI or would like to raise any bug reports or features requests please [contact support](https://github.com/packethost/csi-packet/blob/master/SUPPORT.md).**

Please Note: "[Elastic Block Storage is only available in Core Legacy Sites: AMS1, DFW2, EWR1, NRT1, SJC1. If you do not have access to these sites, you may reach out to our support team to request it.](https://metal.equinix.com/developers/docs/resilience-recovery/elastic-block-storage/#legacy-only-sites)"

## Requirements

At the current state of Kubernetes, running the CSI requires a few things.
Please read through the requirements carefully as they are critical to running the CSI on a Kubernetes cluster.

### Version

Recommended versions of Equinix Metal CSI based on your Kubernetes version:
* Equinix Metal CSI version v0.0.2 supports Kubernetes version >=v1.10

### Privilege

In order for CSI to work, your kubernetes cluster **must** allow privileged pods. Both the `kube-apiserver` and the kubelet must start with the flag `--allow-privileged=true`.


## Deploying in a kubernetes cluster

### Token

To run `csi-packet`, you need your Equinix Metal api key and project ID that your cluster is running in.
If you are already logged in, you can create one by clicking on your profile in the upper right then "API keys".
To get project ID click into the project that your cluster is under and select "project settings" from the header.
Under General you will see "Project ID". Once you have this information you will be able to fill in the config needed for the CCM.

#### Create config

Copy [deploy/template/secret.yaml](./deploy/template/secret.yaml) to a local file:

```bash
cp deploy/template/secret.yaml packet-cloud-config.yaml
```

Replace the placeholder in the copy with your token. When you're done, the `packet-cloud-config.yaml` should look something like this:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: packet-cloud-config
  namespace: kube-system
stringData:
  cloud-sa.json: |
    {
    "apiKey": "abc123abc123abc123",
    "projectID": "abc123abc123abc123"
    }
```

Then run:

```bash
kubectl apply -f ./packet-cloud-config.yaml`
```

You can confirm that the secret was created in the `kube-system` with the following:

```bash
$ kubectl -n kube-system get secrets packet-cloud-config
NAME                  TYPE                                  DATA      AGE
packet-cloud-config   Opaque                                1         2m
```

**Note:** This is the _exact_ same config as used for [Equinix Metal CCM](https://github.com/packethost/packet-ccm), allowing you to create a single set of credentials in a single secret to support both.

### Set up Driver

```
$ kubectl -n kube-system apply -f deploy/kubernetes/setup.yaml
$ kubectl -n kube-system apply -f deploy/kubernetes/node.yaml
$ kubectl -n kube-system apply -f deploy/kubernetes/controller.yaml
```

### Run demo (optional):

```
$ kubectl apply -f deploy/demo/demo-deployment.yaml
```

## Command-Line Options

You can run the binary with `--help` to get command-line options. Important options are:

* `--endpoint=<path>` : (required) path to the kubelet registration socket. According to the spec, this should be `/var/lib/kubelet/plugins/<unique_provider_name>/csi.sock`. Thus we **strongly** recommend you mount it at `/var/lib/kubelet/plugins/csi.packet.net/csi.sock`. The deployment files in this repository assume that path.
* `--v=<level>` : (optional) verbosity level per [logrus](https://github.com/sirupsen/logrus)
* `--config=<path>` : (optional) path to config file, in json format, that contains the Equinix Metal configuration information as set below.
* `--nodeid=<id>` : (optional) override the unique ID of this node as understood by the Equinix Metal API. If not provided, will retrieve the node ID from the Equinix Metal Metadata service.

### Config File Format

The configuration file passed to `--config` must be a json file, and should contain the following keys:

* `apiKey` : Equinix Metal API key to use
* `projectID` : Equinix Metal project ID
* `facilityID` : Equinix Metal facility ID

### Environment Variables

In addition to passing information via the config file, you can set it in environment variables. Environment variables _always_ override any setting in the config file. The variables are:

* `PACKET_API_KEY`
* `PACKET_PROJECT_ID`
* `PACKET_FACILITY_ID`

## Running the csi-sanity tests

[csi-sanity](https://github.com/kubernetes-csi/csi-test/tree/master/cmd/csi-sanity) is a set of integration tests that can be run on a host where a csi-plugin is running.
In a kubernetes cluster, _csi-sanity_ can be run on a node and communicate with the daemonset node controller running there.

The steps are as follows

1. Install the `csi-packet` plugin as above into a kubernetes cluster, but use `node_controller_sanity_test.yaml` instead of `node.yaml`.
   The crucial difference is to start the driver with the Equinix Metal credentials so that the csi-controller is running.
2. `ssh` to a node, install a golang environment and build the csi-sanity binaries.
3. Run `./csi-sanity --ginkgo.v --csi.endpoint=/var/lib/kubelet/plugins/csi.packet.net/csi.sock`

Please report any failures to this repository.

## Build and Design

To build the Equinix Metal CSI and understand its design, please see [BUILD.md](./BUILD.md).
