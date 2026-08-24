runtime: {
	apiVersion: "v1alpha1"
	name:       "fleet"
	clusters: {
		"staging": {
			group:       "staging"
			kubeContext: "kind-timoni-staging"
		}
		"production": {
			group:       "production"
			kubeContext: "kind-timoni-production"
		}
	}
	values: [
		{
			query: "k8s:v1:Namespace:kube-system"
			for: "TIMONI_CLUSTER_ID": "obj.metadata.uid"
		},
	]
}
