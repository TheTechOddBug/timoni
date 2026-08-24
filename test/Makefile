# Copyright 2026 Stefan Prodan
# SPDX-License-Identifier: Apache-2.0

# Timoni local dev multi-cluster setup
# Requirements: docker, kind, kubectl

.ONESHELL:
.SHELLFLAGS += -e

REPOSITORY_ROOT := $(shell git rev-parse --show-toplevel)

.PHONY: up
up: # Start a local Kind cluster and a container registry on port 5555
	$(REPOSITORY_ROOT)/test/scripts/kind-up.sh

.PHONY: down
down: # Teardown the Kind cluster and registry
	$(REPOSITORY_ROOT)/test/scripts/kind-down.sh

.PHONY: fleet-up
fleet-up: # Start local Kind clusters (staging and production) and a container registry on port 5555
	CLUSTER_NAME=timoni-staging $(REPOSITORY_ROOT)/test/scripts/kind-up.sh
	CLUSTER_NAME=timoni-production $(REPOSITORY_ROOT)/test/scripts/kind-up.sh

.PHONY: fleet-down
fleet-down: # Teardown the Kind clusters and registry
	kind delete cluster --name timoni-staging
	kind delete cluster --name timoni-production
	docker rm -f timoni-registry

.PHONY: push
push: # Push the blueprint module to the local registry using the locally built timoni
	$(REPOSITORY_ROOT)/test/scripts/mod-push.sh

.PHONY: deploy
deploy: # Deploy the apps bundle on the single cluster using the locally built timoni
	$(REPOSITORY_ROOT)/bin/timoni bundle apply --kube-context kind-timoni \
	-f $(REPOSITORY_ROOT)/test/manifests/apps.cue

.PHONY: fleet-deploy
fleet-deploy: # Deploy the apps bundle across the fleet using the locally built timoni
	$(REPOSITORY_ROOT)/bin/timoni bundle apply \
	-f $(REPOSITORY_ROOT)/test/manifests/apps.cue \
	-r $(REPOSITORY_ROOT)/test/manifests/runtime.cue

.PHONY: fleet-deploy-check
fleet-deploy-check: # Check the status of the apps bundle across the fleet using the locally built timoni
	$(REPOSITORY_ROOT)/bin/timoni bundle status \
	-f $(REPOSITORY_ROOT)/test/manifests/apps.cue \
	-r $(REPOSITORY_ROOT)/test/manifests/runtime.cue

.PHONY: fleet-deploy-uninstall
fleet-deploy-uninstall: # Delete the apps bundle across the fleet using the locally built timoni
	$(REPOSITORY_ROOT)/bin/timoni bundle delete \
	-f $(REPOSITORY_ROOT)/test/manifests/apps.cue \
	-r $(REPOSITORY_ROOT)/test/manifests/runtime.cue

.PHONY: help
help:  ## Display this help menu
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
