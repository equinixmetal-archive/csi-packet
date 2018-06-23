## Deploying in a kubernetes cluster

### Step 1 (Create Credentials):

Obtain the packet auth token, project id and facility id.

Note that the auth token must be at user or organization scope, since a project-scoped token does not provide access to all of the currently-used api endpoints.
```
$ cat <<EOF > cloud-sa.json
{
   "auth-token": "${PACKET_TOKEN}",
   "project-id": "${PROJECT_ID}",
   "facility-id": "${FACILITY_ID}"
}
EOF
```

Create Kubernetes secret in the kube-system namespace:
```
    kubectl create -n kube-system secret generic cloud-sa --from-file=cloud-sa.json
```

### Step 2 (Set up Driver):
```
$ kubectl -n kube-system create -f setup.yaml
$ kubectl -n kube-system create -f node.yaml
$ kubectl -n kube-system create -f controller.yaml
```

### Step 3 (Run demo [optional]):
```
$ kubectl create -f demo-deployment.yaml
```

## Running the csi-sanity tests

[csi-sanity](https://github.com/kubernetes-csi/csi-test/tree/master/cmd/csi-sanity) is a set of integration tests that can be run on a host where a csi-plugin is running.  In a kubernetes cluster, _csi-sanity_ can be run on a node and communicate with
the daemonset node controller running there.

The steps are as follows

1.  Install the csi-packet plugin as above into a kubernetes cluster, but use _node_controller_sanity_test.yaml_ instead of _node.yaml_.  The crucial difference is to start the driver with the packet credentials so that the csi-controller is running.
2.  Ssh to a node, install a golang environment and build the csi-sanity binaries.
3.  Run `./csi-sanity --ginkgo.v --csi.endpoint=/var/lib/kubelet/plugins/com.packet.csi/csi.sock`

Please report any failures to this repository.
