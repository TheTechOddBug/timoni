package main

import (
	gadgetv1 "testing.timoni.sh/gadget/v1"
)

// Define the schema for the user-supplied values.
values: {
	replicas:    *3 | int
	minReplicas: *1 | int
	size:        *"small" | string
}

// Define how Timoni should build, validate and
// apply the Kubernetes resources.
timoni: {
	apiVersion: "v1alpha1"

	instance: {
		config: {
			metadata: {
				name:      string @tag(name)
				namespace: string @tag(namespace)
			}
			replicas:    values.replicas
			minReplicas: values.minReplicas
			size:        values.size
		}

		objects: {
			// The Widget CRD declares CEL rules and is part of the module output.
			crd: {
				apiVersion: "apiextensions.k8s.io/v1"
				kind:       "CustomResourceDefinition"
				metadata: name: "widgets.testing.timoni.sh"
				spec: {
					group: "testing.timoni.sh"
					names: {
						kind:     "Widget"
						listKind: "WidgetList"
						plural:   "widgets"
						singular: "widget"
					}
					scope: "Namespaced"
					versions: [{
						name:    "v1"
						served:  true
						storage: true
						schema: openAPIV3Schema: {
							type: "object"
							properties: spec: {
								type: "object"
								required: ["replicas"]
								properties: {
									replicas: type:    "integer"
									minReplicas: type: "integer"
								}
								"x-kubernetes-validations": [{
									rule:    "!has(self.minReplicas) || self.minReplicas <= self.replicas"
									message: "minReplicas must not exceed replicas"
								}]
							}
						}
					}]
				}
			}

			widget: {
				apiVersion: "testing.timoni.sh/v1"
				kind:       "Widget"
				metadata: {
					name:      config.metadata.name
					namespace: config.metadata.namespace
				}
				spec: {
					replicas:    config.replicas
					minReplicas: config.minReplicas
				}
			}

			// The Gadget CRD is vendored in cue.mod/gen along with its CEL rules.
			gadget: gadgetv1.#Gadget & {
				metadata: {
					name:      config.metadata.name
					namespace: config.metadata.namespace
				}
				spec: size: config.size
			}
		}
	}

	apply: app: [for obj in instance.objects {obj}]
}
