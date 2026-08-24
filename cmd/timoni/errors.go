/*
Copyright 2023 Stefan Prodan

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

package main

import (
	"errors"
	"fmt"

	cueerrors "cuelang.org/go/cue/errors"

	"github.com/stefanprodan/timoni/internal/reconciler"
)

func describeErr(moduleRoot, description string, err error) error {
	return fmt.Errorf("%s:\n%s", description, cueerrors.Details(err, &cueerrors.Config{
		Cwd: moduleRoot,
	}))
}

// ExitError wraps an error with the code the process exits with,
// overriding the default exit code of 1 used for plain errors.
type ExitError struct {
	Err  error
	Code int
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// diffExitErr maps the result of an apply command run in diff mode to the
// flux CLI exit code convention: 0 when the cluster state is in sync,
// 1 when drift is detected and 2 when the dry run fails.
func diffExitErr(err error) error {
	if err == nil {
		return nil
	}
	var driftErr *reconciler.InstanceDriftError
	if errors.As(err, &driftErr) {
		return &ExitError{Err: err, Code: 1}
	}
	return &ExitError{Err: err, Code: 2}
}
