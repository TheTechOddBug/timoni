/*
Copyright 2024 Stefan Prodan

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

package dyff

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/fluxcd/pkg/ssa"
	ssaerr "github.com/fluxcd/pkg/ssa/errors"
	ssautil "github.com/fluxcd/pkg/ssa/utils"
	"github.com/go-logr/logr"
	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	apiv1 "github.com/stefanprodan/timoni/api/v1alpha1"
	"github.com/stefanprodan/timoni/internal/logger"
)

// DyffPrinter is a printer that prints dyff reports.
type DyffPrinter struct {
	OmitHeader bool
}

// NewDyffPrinter returns a new DyffPrinter.
func NewDyffPrinter() *DyffPrinter {
	return &DyffPrinter{
		OmitHeader: true,
	}
}

// Print prints the given args to the given writer.
func (p *DyffPrinter) Print(w io.Writer, args ...any) error {
	for _, arg := range args {
		switch arg := arg.(type) {
		case dyff.Report:
			reportWriter := &dyff.HumanReport{
				Report:     arg,
				OmitHeader: p.OmitHeader,
			}

			if err := reportWriter.WriteReport(w); err != nil {
				return fmt.Errorf("failed to print report: %w", err)
			}
		default:
			return fmt.Errorf("unsupported type %T", arg)
		}
	}
	return nil
}

func DiffYAML(liveFile, mergedFile string, output io.Writer) error {
	from, to, err := ytbx.LoadFiles(liveFile, mergedFile)
	if err != nil {
		return fmt.Errorf("failed to load input files: %w", err)
	}

	report, err := dyff.CompareInputFiles(from, to,
		dyff.IgnoreOrderChanges(false),
		dyff.KubernetesEntityDetection(true),
	)
	if err != nil {
		return fmt.Errorf("failed to compare input files: %w", err)
	}

	printer := NewDyffPrinter()
	return printer.Print(output, report)
}

// InstanceDryRunDiff performs a server-side apply dry run of the instance
// objects and logs the pending change of each one. When withDiff is set, the
// field changes of the objects that differ from the cluster state are printed
// to w in YAML diff format, and the dry-run failures are aggregated into the
// returned error. It returns true if applying the instance would change the
// cluster state, i.e. if any object would be created, configured or deleted.
// Mirroring the apply and prune semantics, existing objects annotated as
// one-off are skipped, and stale objects that are absent from the cluster or
// annotated with prune disabled are not counted as pending deletions.
func InstanceDryRunDiff(ctx context.Context,
	rm *ssa.ResourceManager,
	objects []*unstructured.Unstructured,
	staleObjects []*unstructured.Unstructured,
	nsExists bool,
	tmpDir string,
	withDiff bool,
	w io.Writer) (bool, error) {
	log := logr.FromContextOrDiscard(ctx)
	diffOpts := ssa.DefaultDiffOptions()
	diffOpts.IfNotPresentSelector = map[string]string{
		apiv1.IfNotPresentAction: apiv1.EnabledValue,
	}
	sort.Sort(ssa.SortableUnstructureds(objects))

	changed := !nsExists
	failed := 0

	for _, r := range objects {
		if !nsExists {
			log.Info(logger.ColorizeJoin(r, ssa.CreatedAction, logger.DryRunServer))
			continue
		}

		change, liveObject, mergedObject, err := rm.Diff(ctx, r, diffOpts)
		if err != nil {
			if ssaerr.IsImmutableError(err) {
				changed = true
				if ssautil.AnyInMetadata(r, map[string]string{
					apiv1.ForceAction: apiv1.EnabledValue,
				}) {
					log.Info(logger.ColorizeJoin(r, ssa.CreatedAction, logger.DryRunServer))
				} else {
					log.Error(nil, logger.ColorizeJoin(r, "immutable", logger.DryRunServer))
				}
			} else {
				failed++
				log.Error(err, logger.ColorizeUnstructured(r))
			}

			continue
		}

		if change.Action == ssa.CreatedAction || change.Action == ssa.ConfiguredAction {
			changed = true
		}

		log.Info(logger.ColorizeJoin(change, logger.DryRunServer))
		if withDiff && change.Action == ssa.ConfiguredAction {
			liveYAML, _ := yaml.Marshal(liveObject)
			liveFile := filepath.Join(tmpDir, "live.yaml")
			if err := os.WriteFile(liveFile, liveYAML, 0644); err != nil {
				return changed, err
			}

			mergedYAML, _ := yaml.Marshal(mergedObject)
			mergedFile := filepath.Join(tmpDir, "merged.yaml")
			if err := os.WriteFile(mergedFile, mergedYAML, 0644); err != nil {
				return changed, err
			}

			if err := DiffYAML(liveFile, mergedFile, w); err != nil {
				return changed, err
			}
		}
	}

	for _, r := range staleObjects {
		existingObject := &unstructured.Unstructured{}
		existingObject.SetGroupVersionKind(r.GroupVersionKind())
		err := rm.Client().Get(ctx, client.ObjectKeyFromObject(r), existingObject)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err == nil && ssautil.AnyInMetadata(existingObject, map[string]string{
			apiv1.PruneAction: apiv1.DisabledValue,
		}) {
			log.Info(logger.ColorizeJoin(r, ssa.SkippedAction, logger.DryRunServer))
			continue
		}
		changed = true
		log.Info(logger.ColorizeJoin(r, ssa.DeletedAction, logger.DryRunServer))
	}

	if withDiff && failed > 0 {
		return changed, fmt.Errorf("dry run failed for %d resource(s)", failed)
	}

	return changed, nil
}
