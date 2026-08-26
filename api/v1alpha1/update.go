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

package v1alpha1

import (
	"fmt"
	"strings"
)

const (
	// UpdateKind is the name of the Timoni update CUE attributes.
	UpdateKind string = "update"

	// UpdatePolicySemver selects the newest module version matching
	// a semver constraint, e.g. '@timoni(update:semver:8.x)'.
	UpdatePolicySemver string = "semver"

	// UpdatePolicyDigest keeps the module version and refreshes
	// the digest to the one currently tagged with that version,
	// e.g. '@timoni(update:digest)'.
	UpdatePolicyDigest string = "digest"

	// UpdatePolicyNone excludes the module reference from updates,
	// e.g. '@timoni(update:none)'.
	UpdatePolicyNone string = "none"
)

// UpdateAttribute holds the update policy declared on a bundle
// module version field and the semver constraint of the policy.
type UpdateAttribute struct {
	Policy     string
	Constraint string
}

// NewUpdateAttribute returns an UpdateAttribute from the given CUE attribute.
// If the CUE attribute doesn't match one of the expected formats
// '@timoni(update:semver:[CONSTRAINT])', '@timoni(update:digest)' or
// '@timoni(update:none)', an error is returned.
func NewUpdateAttribute(key, body string) (*UpdateAttribute, error) {
	if !IsUpdateAttribute(key, body) {
		return nil, fmt.Errorf("invalid format, must be @timoni(%[1]s%[2]s%[3]s%[2]s[CONSTRAINT]), @timoni(%[1]s%[2]s%[4]s) or @timoni(%[1]s%[2]s%[5]s)",
			UpdateKind, RuntimeDelimiter, UpdatePolicySemver, UpdatePolicyDigest, UpdatePolicyNone)
	}
	parts := strings.SplitN(body, RuntimeDelimiter, 3)
	attr := &UpdateAttribute{Policy: parts[1]}
	if len(parts) == 3 {
		attr.Constraint = strings.TrimSpace(parts[2])
	}
	return attr, nil
}

// IsUpdateAttribute returns true if the given
// CUE attribute matches one of the expected formats.
func IsUpdateAttribute(key, body string) bool {
	if key != FieldManager {
		return false
	}

	parts := strings.SplitN(body, RuntimeDelimiter, 3)
	if len(parts) < 2 || parts[0] != UpdateKind {
		return false
	}

	switch parts[1] {
	case UpdatePolicySemver:
		return len(parts) == 3 && strings.TrimSpace(parts[2]) != ""
	case UpdatePolicyDigest, UpdatePolicyNone:
		return len(parts) == 2
	}
	return false
}
