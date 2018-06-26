### Step 1 (Create Credentials):

Obtain the packet auth token, project id and facility id for the nodes of the cluster
```
$ cat <<EOF > cloud-sa.json
{
   "auth-token": "${PACKET_TOKEN}",
   "project-id": "${PROJECT_ID}",
   "facility-id": "${FACILITY_ID}"
}
EOF
```

Create Kubernetes secret:
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