# Kubernetes Container Storage Interface (CSI) plugin for Packet

`csi-packet` is the Kubernetes CSI implementation for Packet. Read more about the CSI [here](https://kubernetes-csi.github.io/docs/).

## Requirements

At the current state of Kubernetes, running the CSI requires a few things.
Please read through the requirements carefully as they are critical to running the CSI on a Kubernetes cluster.

### Version

Recommended versions of Packet CSI based on your Kubernetes version:
* Packet CSI version v0.0.2 supports Kubernetes version >=v1.10

### Privilege

In order for CSI to work, your kubernetes cluster **must** allow privileged pods. Both the `kube-apiserver` and the kubelet must start with the flag `--allow-privileged=true`.


## Deploying in a kubernetes cluster

### Token

To run `csi-packet`, you need your Packet api key and project ID that your cluster is running in.
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

**Note:** This is the _exact_ same config as used for [Packet CCM](https://github.com/packethost/packet-ccm), allowing you to create a single set of credentials in a single secret to support both.

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

* `--endpoint=<path>` : (required) path to the kubelet registration socket. According to the spec, this should be `/var/lib/kubelet/plugins/<unique_provider_name>/csi.sock`. Thus we **strongly** recommend you mount it at `/var/lib/kubelet/plugins/net.packet.csi/csi.sock`. The deployment files in this repository assume that path.
* `--nodeid=<name>` : (required) name of the node as understood by the kubernetes cluster. Normally retrieved via the Kubernetes [downward API](https://kubernetes.io/docs/tasks/inject-data-application/downward-api-volume-expose-pod-information/) as `spec.nodeName`
* `--v=<level>` : (optional) verbosity level per [logrus](https://github.com/sirupsen/logrus)
* `--config=<path>` : (optional) path to config file, in json format, that contains the Packet configuration information as set below.

### Config File Format

The configuration file passed to `--config` must be a json file, and should contain the following keys:

* `apiKey` : packet API key to use
* `projectID` : packet project ID
* `facilityID` : packet facility ID

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
   The crucial difference is to start the driver with the packet credentials so that the csi-controller is running.
2. `ssh` to a node, install a golang environment and build the csi-sanity binaries.
3. Run `./csi-sanity --ginkgo.v --csi.endpoint=/var/lib/kubelet/plugins/net.packet.csi/csi.sock`

Please report any failures to this repository.

## Build and Design

To build the Packet CSI and understand its design, please see [BUILD.md](./BUILD.md).
