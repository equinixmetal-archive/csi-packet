# Container Storage Interface (CSI) plugin for Packet

The CSI Packet plugin allows the creation and mounting of packet storage volumes as
persistent volume claims in a kubernetes cluster.

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

The files found in _deploy/kubernetes/_ define the deployment process, which follows the approach diagrammed in the [design proposal](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/storage/container-storage-interface.md#recommended-mechanism-for-deploying-csi-drivers-on-kubernetes)

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

The makefile uses an alpine-go image to build the driver, and packages it into an image, closely following https://github.com/thockin/go-build-template.  The version number defaults to the git hash but may be set explicitly.

```
dep ensure
# export VERSION=5.4.3
make container
```