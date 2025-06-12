# coordination-apiserver

An in-memory implementation of the Kubernetes Lease API server, designed to replace etcd-based storage for leases.

## Motivation

In Kubernetes, node heartbeats are implemented through Lease objects in the coordination.k8s.io API group. By default, each node updates its Lease every 10 seconds to signal liveness. 

For example, in a 5,000-node cluster, this results in:
- 500 write operations per second (5,000 nodes ÷ 10 seconds)
- 500 read operations per second (as controllers check these leases)

This sustained load on etcd creates significant:
- Write amplification from frequent small updates
- Storage overhead from maintaining revision history
- Network traffic between etcd members

These factors make etcd a scaling bottleneck for large clusters, particularly for this high-frequency, low-value coordination data.

## Solution

This project provides a lightweight, high-performance alternative implementation that:
- Stores Lease objects in-memory for ultra-low latency access
- Scales efficiently to support clusters with 10,000+ nodes

The solution works by deploying our coordination-apiserver and updating the APIService for `v1.coordination.k8s.io` to route requests to our in-memory implementation instead of etcd.

## Usage

### Installation

1. **Backup the current APIService configuration**:
   ```bash
   kubectl get apiservice v1.coordination.k8s.io -o yaml > apiservice-backup.yaml
   ```

2. **Deploy the coordination-apiserver**:
   ```bash
   kubectl apply -f ./manifests/deployment.yaml
   ```

3. **Wait for the deployment to become ready**:
   ```bash
   kubectl wait --for=condition=available deployment/coordination-apiserver -n kube-system --timeout=300s
   ```

4. **Redirect Lease API to coordination-apiserver**:
   ```bash
   kubectl apply -f ./manifests/apiservice.yaml
   ```

At this point:
- All existing leases will be ignore, triggering controller leader re-election (this is expected behavior during the transition)
- Newly created leases will be stored in the coordination-apiserver's in-memory storage

### Rollback to etcd storage

To revert back to etcd-based lease storage:

1. **Restore the original APIService configuration from the backup**:
   ```bash
   kubectl apply -f apiservice-backup.yaml
   ```

2. **Delete the coordination-apiserver**:
   ```bash
   kubectl delete -f ./manifests/deployment.yaml
   ```

## TODO

- TLS verification for APIService
- v1beta1 compatibility and conversion
- Production deployment documentation
- Metrics and monitoring
- Kubernetes API compatibility test
- Lease migration tooling and automation for existing leases
