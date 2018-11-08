# Container Storage Interface (CSI) plugin for Packet

The CSI Packet plugin allows the creation and mounting of packet storage volumes as
persistent volume claims in a kubernetes cluster.

## Deploy
Read how to deploy the Kubernetes CSI plugin for Packet [here](deploy/kubernetes/)!

## Design
The basic refernce for Kubernetes CSI is found at https://kubernetes-csi.github.io/docs/

A typical sequence of the tasks performed by the Controller and Node components is

 - **Create**          *Controller.CreateVolume*
    - **Attach**       *Controller.ControllerPublish*
        - **Mount, Format**   *Node.NodeStageVolume* (called once only per volume)
            - **Bind Mount**   *Node.NodePublishVolume*
            - **Bind Unmount** *Node.NodeUnpublishVolume*
        - **Unmount**          *Node.NodeUnstageVolume*
    - **Detach**       *Controller.ControllerUnpublish*
 - **Destroy**         *Controller.DeleteVolum*e


## System configuration

The plugin node component require particular configuration of the packet host with regard to the services that are running.
It relies on iscsid being configured correctly with the initiator name, and up multipathd running with a configuration that includes `user_friendly_names     yes`  This setup is not perfomed by the plugin.

## Deployment

The files found in `deploy/kubernetes/` define the deployment process, which follows the approach diagrammed in the [design proposal](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/storage/container-storage-interface.md#recommended-mechanism-for-deploying-csi-drivers-on-kubernetes)

### Packet credentials

Packet credentials are used by the controller to manage volumes.  They are configured with a json-formatted secret which contains

* an authetication token
* a project id
* a facility id

The cluster is assumed to reside wholly in one facility, so the controller includes that facility id in all of the volume-related api calls.

### RBAC

The file deploy/kubernetes/setup.yaml contains the serviceaccount, role and rolebinding definitions used by the various components.

### Deployment

The controller is deployed as a StatefulSet to ensure that there is a single instance of the pod.  The node is deployed as a daemonset, which will install an instance of the pod on every un-tainted node.  In most cluster deployments, the master node will have a `node-role.kubernetes.io/master:NoSchedule` taint and so the csi-packet plugin will not operate there.

### Helper sidecar containers

The CSI plugin framework is designed to be agnostic with respect to the Container Orchestrator system, and so there is no direct communication between the CSI plugin and the kubernetes api.  Instead, the CSI project provides a number of sidecar containers which mediate beween the plugin and kubernetes.

The controller deployment uses

  * external-attacher https://github.com/kubernetes-csi/external-attacher
  * external-provisioner https://github.com/kubernetes-csi/external-provisioner

which communicate with the kubernetes api within the cluster, and communicate with the csi-packet plugin through a unix domain socket shared in the pod.

The node deployment uses

  * driver-registrar https://github.com/kubernetes-csi/driver-registrar

which advertises the csi-packet driver to the cluster.  There is also by unix domain socket communciation channel, but in this case it is a host-mounted directory in order to permit the kubelet process to interact with the plugin.

TODO: incorporate the liveness prob

* liveness-probe https://github.com/kubernetes-csi/livenessprobe

### Mounted volumes and privilege

The node processes must interact with services running on the host in order to connect, mount and format the packet volumes. These interactions require a particular pod configuration.  The driver invokes the *iscsiadm* and *multipath* client processes and they must communicate with the *iscisd* and *multipathd* systemd services.  In consequence, the pod
 - uses hostNetwork: true
 - is privileged
 - mounts /etc/
 - mounts /dev
 - mounts /var/lib/iscsi
 - mounts /sys/devices
 - mounts /run/udev/
 - mounts /var/lib/kubelet
 - mounts /csi


## Further documentation

See additional documents in the docs/ directory

## Caveat

The plugin at present is far from production assurance, and requires testing and hardening. Even listing the known issues is premature.

## Development
The Makefile builds locally using your own installed `go`, or in a container via `docker run`. 

The following are standard commands:

* `make build ARCH=$(ARCH)` - ensure vendor dependencies and build the single CSI binary as `dist/bin/packet-cloud-storage-interface-$(ARCH)`
* `make build-all` - ensure vendor dependencies and build the single CSI binary for all supported architectures. We build a binary for each arch for which a `Dockerfile.<arch>` exists in this directory.
* `make build ARCH=$(ARCH) DOCKERBUILD=true` - ensure vendor dependencies and build the CSI binary as above, while performing the build inside a docker container
* `make build-all` - ensure vendor dependencies and build the CSI binary for all supported architectures.
* `make image ARCH=$(ARCH)` - make an OCI image for the provided ARCH
* `make image-all` - make an OCI image for all supported architectures
* `make ci` - build, test and create an OCI image for all supported architectures. This is what the CI system runs.
* `make cd` - deploy images for all supported architectures, as well as a multi-arch manifest..
* `make release` - deploy tagged release images for all supported architectures, as well as a multi-arch manifest..

All images are tagged `$(TAG)-$(ARCH)`, where:

* `TAG` = the image tag, which always includes the current branch when merged into `master`, and the short git hash. For git tags that are applied in master via `make release`, it also is the git tag. Thus a normal merge releases two image tags - git hash and `master` - while adding a git tag to release formally creates a third one.
* `ARCH` = the architecture of the image

In addition, we use multi-arch manifests for "archless" releases, e.g. `:3456abcd` or `:v1.2.3` or `:master`.
