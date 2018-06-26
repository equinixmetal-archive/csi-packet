## Background reading


### CSI vs FlexVolumes

FlexVolumes are an older spec, since k8s 1.2, and will be supported in the future.  Requires root access to install on each node and assumes OS-based tools are installed. "The Storage SIG suggests implementing a CSI driver if possible"

### CSI design summary

Kubernetes will introduce a new in-tree volume plugin called CSI.

This plugin, in kubelet, will make volume mount and unmount rpcs to a unix domain socket on the host machine. The driver component responds to these requests in a specialized way. (https://github.com/kubernetes/community/blob/master/contributors/design-proposals/storage/container-storage-interface.md#kubelet-to-csi-driver-communication)


Lifecycle management of volume is done by the controller-manager (https://github.com/kubernetes/community/blob/master/contributors/design-proposals/storage/container-storage-interface.md#master-to-csi-driver-communication) and communication is mediated through the api-server, which requires that the external component watch the k8s api for changes. The design document suggests a sidecar “Kubernetes to CSI” proxy.

The concern here is that
  - communication to the diver is done through a local unix domain socket
  - the driver is untrusted and cannot be allowed to run on the master node
  - the controller manager runs on the master node
  - the driver doesn't have any kubernetes-awareness, doesn't have k8s client code or how to watch the api serer

This section: https://github.com/kubernetes/community/blob/master/contributors/design-proposals/storage/container-storage-interface.md#recommended-mechanism-for-deploying-csi-drivers-on-kubernetes
shows the recommended deployment which puts the driver in a container inside a pod, sharing it with a k8s-aware container, with communication between those two via a unix domain socket "in the pod"



### References

Packet API:
  *  https://www.packet.net/developers/api/volumes/
  *  https://github.com/packethost/packngo
  *  https://github.com/ebsarr/packet
  *  https://github.com/packethost/packet-block-storage/


packet-flex-volume:
  *  https://github.com/karlbunch/packet-k8s-flexvolume/blob/master/flexvolume/packet/plugin.py#L463

  *  create: https://github.com/karlbunch/packet-k8s-flexvolume/blob/master/flexvolume/packet/plugin.py#L350
  *  attach:
      *  https://github.com/karlbunch/packet-k8s-flexvolume/blob/master/flexvolume/packet/plugin.py#L497
      *  https://github.com/packethost/packet-python/blob/master/packet/Volume.py#L38
  *  iscsi, multipath: https://github.com/karlbunch/packet-k8s-flexvolume/blob/master/flexvolume/packet/plugin.py#L515
  *  mount: https://github.com/karlbunch/packet-k8s-flexvolume/blob/master/flexvolume/packet/plugin.py#L544

iscsi:
 *   https://coreos.com/os/docs/latest/iscsi.html
 *   https://eucalyptus.atlassian.net/wiki/spaces/STOR/pages/84312154/iscsiadm+basics
 *   https://linux.die.net/man/8/multipath
 *   https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/6/html/dm_multipath/

mount:
  *   https://coreos.com/os/docs/latest/mounting-storage.html
  *   https://oguya.ch/posts/2015-09-01-systemd-mount-partition/
  *  ? https://github.com/coreos/bugs/issues/2254
  *  ? https://github.com/kubernetes/kubernetes/issues/59946#issuecomment-380401916
  *  ? https://github.com/kubernetes/kubernetes/pull/63176

CSI design
  *  https://github.com/container-storage-interface/spec/blob/master/spec.md#rpc-interface

CSI examples
  *  https://github.com/kubernetes-csi/drivers
  *  https://github.com/libopenstorage/openstorage/tree/master/csi
  *  https://github.com/thecodeteam/csi-vsphere
  *  https://github.com/openebs/csi-openebs/
  *  https://github.com/digitalocean/csi-digitalocean
  *  https://github.com/GoogleCloudPlatform/compute-persistent-disk-csi-driver/
  *  https://github.com/GoogleCloudPlatform/compute-persistent-disk-csi-driver/blob/master/deploy/kubernetes/README.md

grpc server

  *    https://github.com/GoogleCloudPlatform/compute-persistent-disk-csi-driver/blob/6702720a9de93b57d73fa8912ef04ce6327a00e3/pkg/gce-csi-driver/server.go
  *  https://github.com/digitalocean/csi-digitalocean/blob/783dcec9b26da4ee9c36b6472e180ebb904c465d/driver/driver.go
  *  https://dev.to/chilladx/how-we-use-grpc-to-build-a-clientserver-system-in-go-1mi


Documentation
  *  https://kubernetes.io/blog/2018/04/10/container-storage-interface-beta/
  *  https://github.com/kubernetes/community/blob/master/contributors/design-proposals/resource-management/device-plugin.md#unix-socket
  *  https://github.com/kubernetes/community/blob/master/contributors/design-proposals/storage/container-storage-interface.md
  *  https://github.com/kubernetes/community/blob/master/sig-storage/volume-plugin-faq.md
  *  https://github.com/kubernetes/community/blob/master/sig-storage/volume-plugin-faq.md#working-with-out-of-tree-volume-plugin-options
  *  https://kubernetes-csi.github.io/docs/Drivers.html
  *  https://kubernetes.io/blog/2018/01/introducing-container-storage-interface/
  *  https://kubernetes.io/docs/concepts/storage/volumes/
  *  https://github.com/container-storage-interface/spec/blob/master/spec.md#rpc-interface

grpc
   * https://grpc.io/docs/quickstart/go.html
   * https://github.com/golang/protobuf
   * https://grpc.io/docs/tutorials/basic/go.html
   * https://developers.google.com/protocol-buffers/docs/proto3


protobuf spec
  *  https://github.com/container-storage-interface/spec


### Credentials

https://github.com/kubernetes/community/blob/master/contributors/design-proposals/storage/container-storage-interface.md#csi-credentials


### Deployment Helpers

  * external-attacher https://github.com/kubernetes-csi/external-attacher
  * external-provisioner https://github.com/kubernetes-csi/external-provisioner
  * driver-registrar https://github.com/kubernetes-csi/driver-registrar
  * liveness-probe https://github.com/kubernetes-csi/livenessprobe

### Mounting

Mounting a filesystem is an os task, not cloud provider.

DO, GCE create a mounter type
 *   https://github.com/digitalocean/csi-digitalocean/blob/master/driver/mounter.go
 *   https://github.com/GoogleCloudPlatform/compute-persistent-disk-csi-driver/blob/master/pkg/mount-manager/mounter.go
VSphere calls out to a separate library
 *   https://github.com/akutz/gofsutil
why not use [sys](https://godoc.org/golang.org/x/sys/unix#Mount)? Well, it seems we need to exec out in order to call mkfs anyway

  * https://github.com/thecodeteam/csi-vsphere/blob/master/service/node.go


on our coreos installs,
 *   iscsid
 *   multipathd
are present but not running

