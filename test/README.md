# Timoni end-to-end testing

This directory contains the local dev and e2e testing setup for Timoni:
Kind clusters, a local container registry, and a multi-cluster bundle
deployed with the locally built `timoni` binary.

## Requirements

- docker
- kind
- kubectl

## Layout

- `Makefile`: entry point for all local dev and e2e targets
- `manifests/apps.cue`: the `apps` bundle with the `frontend`, `backend` and `cache` instances
- `manifests/runtime.cue`: the `fleet` runtime defining the `staging` and `production` clusters
- `scripts/kind-up.sh`: creates a Kind cluster and the local registry on `localhost:5555`
- `scripts/kind-down.sh`: deletes the Kind cluster and the registry
- `scripts/mod-push.sh`: pushes the `blueprints/starter` module to the local registry as `oci://localhost:5555/modules/blueprint`

## Single cluster workflow

Build the Timoni binary, then create a cluster named `timoni` with the
local registry, push the blueprint module and deploy the bundle:

```shell
make build
cd test
make up
make push
make deploy
```

Teardown:

```shell
make down
```

## Fleet workflow

Create the `timoni-staging` and `timoni-production` clusters with the
local registry, push the blueprint module and deploy the bundle to
both clusters using the runtime definition:

```shell
make build
cd test
make fleet-up
make push
make fleet-deploy
```

Check the status of the bundle instances across the fleet:

```shell
make fleet-deploy-check
```

Delete the bundle from both clusters:

```shell
make fleet-deploy-uninstall
```

Teardown:

```shell
make fleet-down
```

## Runtime values

The `apps` bundle varies its config per cluster using the values
injected by the `fleet` runtime:

- `TIMONI_CLUSTER_NAME` and `TIMONI_CLUSTER_GROUP` are set by Timoni
  from the runtime cluster definition, e.g. the `frontend` instance
  runs with two replicas on the `production` group.
- `TIMONI_CLUSTER_ID` is read live from each cluster, set to the
  `kube-system` namespace UID and added as the `timoni.sh/cluster-id`
  annotation on the `frontend` workloads.

## Continuous integration

The `.github/workflows/e2e.yaml` workflow runs the fleet workflow on
every pull request: it creates the two Kind clusters with
`helm/kind-action`, starts the registry as a GitHub Actions service
container on port `5555`, then runs `make push`, `make fleet-deploy`,
`make fleet-deploy-check` and `make fleet-deploy-uninstall`.
