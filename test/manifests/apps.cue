bundle: {
	_cluster:    "timoni-dev" @timoni(runtime:string:TIMONI_CLUSTER_NAME)
	_clusterEnv: "dev"        @timoni(runtime:string:TIMONI_CLUSTER_GROUP)
	_clusterID:  "unknown"    @timoni(runtime:string:TIMONI_CLUSTER_ID)

	apiVersion: "v1alpha1"
	name:       "apps"
	instances: {
		frontend: {
			module: {
				url:     "oci://localhost:5555/modules/blueprint"
				version: "latest"
			}
			namespace: "apps"
			values: {
				metadata: annotations: {
					"timoni.sh/cluster":    _cluster
					"timoni.sh/cluster-id": _clusterID
				}
				image: {
					repository: "ghcr.io/nginxinc/nginx-unprivileged"
					tag:        "mainline"
				}
				if _clusterEnv == "production" {
					replicas: 2
				}
			}
		}
		backend: {
			module: url: "file://../../examples/minimal"
			namespace: "apps"
			values: message: "Hello from \(_cluster)"
		}
		cache: {
			module: url: "file://../../examples/redis"
			namespace: "apps"
			values: {
				maxmemory: 128
				readonly: replicas: 1
			}
		}
	}
}
