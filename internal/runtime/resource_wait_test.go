/*
Copyright 2026 Stefan Prodan

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package runtime

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/fluxcd/cli-utils/pkg/kstatus/status"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	apiv1 "github.com/stefanprodan/timoni/api/v1alpha1"
	"github.com/stefanprodan/timoni/internal/engine"
)

func Test_healthCheckResult(t *testing.T) {
	g := NewWithT(t)

	value := cuecontext.New().CompileString(apiv1.InstanceSchema + `
timoni: {
	apiVersion: "v1alpha1"
	instance: {}
	apply: {}
	healthChecks: "example.com/Database": #HealthCheck & {
		group: "example.com"
		kind:  "Database"
		#object: status?: {phase?: string, ...}
		current: #object.status.phase == "Ready"
		failed:  #object.status.phase == "Failed"
	}
}
`)
	g.Expect(value.Err()).ToNot(HaveOccurred())
	checks, err := (&engine.ModuleBuilder{}).GetHealthChecks(value)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(checks).To(HaveLen(1))
	hc := checks[0]

	database := func(fields map[string]any) *unstructured.Unstructured {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "example.com/v1",
			"kind":       "Database",
			"metadata":   map[string]any{"name": "db", "namespace": "default"},
		}}
		for k, v := range fields {
			u.Object[k] = v
		}
		return u
	}

	tests := []struct {
		name    string
		object  *unstructured.Unstructured
		expect  status.Status
		message string
	}{
		{
			name:    "ready phase is current",
			object:  database(map[string]any{"status": map[string]any{"phase": "Ready"}}),
			expect:  status.CurrentStatus,
			message: "passed",
		},
		{
			name:    "failed phase is failed",
			object:  database(map[string]any{"status": map[string]any{"phase": "Failed"}}),
			expect:  status.FailedStatus,
			message: "failed",
		},
		{
			name:    "pending phase is in progress",
			object:  database(map[string]any{"status": map[string]any{"phase": "Pending"}}),
			expect:  status.InProgressStatus,
			message: "in progress",
		},
		{
			// A freshly created object without a status is not a
			// definition problem: the expressions are incomplete and
			// the object keeps being polled.
			name:    "object without status is in progress",
			object:  database(nil),
			expect:  status.InProgressStatus,
			message: "in progress",
		},
		{
			name: "object scheduled for deletion is terminating",
			object: database(map[string]any{
				"metadata": map[string]any{"name": "db", "deletionTimestamp": "2026-01-01T00:00:00Z"},
				"status":   map[string]any{"phase": "Ready"},
			}),
			expect:  status.TerminatingStatus,
			message: "scheduled for deletion",
		},
		{
			// The live object contradicts the #object schema declared
			// by the module: polling cannot fix it, so the wait fails
			// fast instead of reporting Unknown until the timeout.
			name:    "object rejected by the schema is failed",
			object:  database(map[string]any{"status": map[string]any{"phase": int64(5)}}),
			expect:  status.FailedStatus,
			message: "does not match the #object schema",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result, err := healthCheckResult(hc, tt.object)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(result.Status).To(Equal(tt.expect))
			g.Expect(result.Message).To(ContainSubstring(tt.message))
			if tt.expect == status.FailedStatus {
				g.Expect(result.Conditions).To(HaveLen(1))
				g.Expect(result.Conditions[0].Type).To(Equal(status.ConditionStalled))
			}
		})
	}
}

func Test_healthCheckResult_StatusNotYetReported(t *testing.T) {
	g := NewWithT(t)

	// The check declares the status and its fields as required, the
	// strictest form a module can write. A live object created moments
	// ago has no status until its controller reports one: this is not a
	// schema conflict but an incomplete evaluation, so the resource
	// stays in progress and keeps being polled instead of failing.
	value := cuecontext.New().CompileString(apiv1.InstanceSchema + `
timoni: {
	apiVersion: "v1alpha1"
	instance: {}
	apply: {}
	healthChecks: "example.com/Database": #HealthCheck & {
		group: "example.com"
		kind:  "Database"
		#object: status: {phase: string, ...}
		current: #object.status.phase == "Ready"
		failed:  #object.status.phase == "Failed"
	}
}
`)
	g.Expect(value.Err()).ToNot(HaveOccurred())
	checks, err := (&engine.ModuleBuilder{}).GetHealthChecks(value)
	g.Expect(err).ToNot(HaveOccurred())

	result, err := healthCheckResult(checks[0], &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Database",
		"metadata":   map[string]any{"name": "db", "namespace": "default", "generation": int64(1)},
		"spec":       map[string]any{"size": "small"},
	}})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result.Status).To(Equal(status.InProgressStatus))

	// Once the controller reports the status, the same check resolves.
	result, err = healthCheckResult(checks[0], &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Database",
		"metadata":   map[string]any{"name": "db", "namespace": "default", "generation": int64(1)},
		"status":     map[string]any{"phase": "Ready"},
	}})
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result.Status).To(Equal(status.CurrentStatus))
}
