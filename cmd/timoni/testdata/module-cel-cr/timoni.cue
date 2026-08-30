package main

values: {
	replicas:    *3 | int
	minReplicas: *1 | int
}

timoni: {
	apiVersion: "v1alpha1"

	instance: {
		config: metadata: {
			name:      string @tag(name)
			namespace: string @tag(namespace)
		}
		objects: widget: {
			apiVersion: "testing.timoni.sh/v1"
			kind:       "Widget"
			metadata: {
				name:      config.metadata.name
				namespace: config.metadata.namespace
			}
			spec: {
				replicas:    values.replicas
				minReplicas: values.minReplicas
			}
		}
	}

	apply: app: [for obj in instance.objects {obj}]
}
