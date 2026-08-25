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
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/mattn/go-shellwords"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestReadRemoteCRDManifestAcceptsBodyAtLimit(t *testing.T) {
	g := NewWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("crd!"))
	}))
	defer server.Close()

	data, err := readRemoteCRDManifest(context.Background(), server.Client(), server.URL, 4)

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(data)).To(Equal("crd!"))
}

func TestReadRemoteCRDManifestHonorsCancellation(t *testing.T) {
	g := NewWithT(t)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err := readRemoteCRDManifest(ctx, server.Client(), server.URL, 4)

	g.Expect(err).To(MatchError(ContainSubstring(context.Canceled.Error())))
}

func TestReadRemoteCRDManifestRejectsLargeContentLength(t *testing.T) {
	g := NewWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("crd!!"))
	}))
	defer server.Close()

	_, err := readRemoteCRDManifest(context.Background(), server.Client(), server.URL, 4)

	g.Expect(err).To(MatchError(ContainSubstring("exceeds the 4-byte limit")))
	g.Expect(err).To(MatchError(ContainSubstring(server.URL)))
}

func TestReadRemoteCRDManifestRejectsUnknownLengthBody(t *testing.T) {
	g := NewWithT(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("crd!!"))
	}))
	defer server.Close()

	_, err := readRemoteCRDManifest(context.Background(), server.Client(), server.URL, 4)

	g.Expect(err).To(MatchError(ContainSubstring("exceeds the 4-byte limit")))
}

func TestReadRemoteCRDManifestLimitsDecodedBody(t *testing.T) {
	g := NewWithT(t)
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, err := zw.Write(bytes.Repeat([]byte("x"), 101))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(zw.Close()).To(Succeed())
	g.Expect(compressed.Len()).To(BeNumerically("<", 100))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()

	_, err = readRemoteCRDManifest(context.Background(), server.Client(), server.URL, 100)

	g.Expect(err).To(MatchError(ContainSubstring("exceeds the 100-byte limit")))
}

func TestReadRemoteCRDManifestRejectsInsecureRedirect(t *testing.T) {
	g := NewWithT(t)
	insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("crd"))
	}))
	defer insecure.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", insecure.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer secure.Close()

	_, err := readRemoteCRDManifest(context.Background(), secure.Client(), secure.URL, 4)

	g.Expect(err).To(MatchError(ContainSubstring("redirect to insecure HTTP")))
}

func TestVendorCrd(t *testing.T) {
	// To regenerate the golden files:
	// make install
	// cd cmd/timoni/
	// timoni mod vendor crd testdata/crd/golden/ -f testdata/crd/source/cert-manager.crds.yaml
	// timoni mod vendor crd testdata/crd/golden/ -f testdata/crd/source/flagger.crds.yaml
	goldenPath := "testdata/crd/golden/cue.mod/"

	tmpDir := t.TempDir()
	genPath := path.Join(tmpDir, "cue.mod")

	g := NewWithT(t)

	err := os.MkdirAll(genPath, os.ModePerm)
	g.Expect(err).ToNot(HaveOccurred())

	for crdPath, outputMatcher := range map[string]types.GomegaMatcher{
		"testdata/crd/source/cert-manager.crds.yaml": ContainSubstring("cert-manager.io/issuer/v1"),
		"testdata/crd/source/flagger.crds.yaml":      ContainSubstring("flagger.app/canary/v1beta1"),
	} {
		output, err := executeCommand(fmt.Sprintf(
			"mod vendor crd %s -f %s",
			tmpDir,
			crdPath,
		))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(output).To(outputMatcher)
	}

	diffArgs := fmt.Sprintf("--no-pager diff --no-index %s %s", genPath, goldenPath)

	args, err := shellwords.Parse(diffArgs)
	g.Expect(err).ToNot(HaveOccurred())

	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	g.Expect(string(out)).To(BeEmpty())
	g.Expect(err).ToNot(HaveOccurred())
}

// multiVersionCRD is a CRD defining two versions used to test the version selection.
const multiVersionCRD = `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    listKind: WidgetList
    plural: widgets
    singular: widget
  scope: Namespaced
  versions:
    - name: v1beta1
      served: true
      storage: false
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                size:
                  type: integer
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                size:
                  type: integer
                color:
                  type: string
`

// newCRDEntry builds a CRD entry with an object defining the given versions.
func newCRDEntry(kind, group string, versions ...string) crdEntry {
	specVersions := make([]any, 0, len(versions))
	for _, v := range versions {
		specVersions = append(specVersions, map[string]any{"name": v})
	}
	return crdEntry{
		kind:     kind,
		group:    group,
		versions: versions,
		object: &unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{"versions": specVersions},
		}},
	}
}

func TestSelectCRDs(t *testing.T) {
	crds := []crdEntry{
		newCRDEntry("Widget", "example.com", "v1beta1", "v1"),
		newCRDEntry("Widget", "other.example.com", "v1alpha1"),
		newCRDEntry("Gadget", "example.com", "v1"),
	}

	t.Run("selects all without selectors", func(t *testing.T) {
		g := NewWithT(t)
		selected, err := selectCRDs(crds, nil, nil, "crds.yaml")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(selected).To(HaveLen(3))
	})

	t.Run("matches kinds case-insensitively", func(t *testing.T) {
		g := NewWithT(t)
		selected, err := selectCRDs(crds, []string{"gadget"}, nil, "crds.yaml")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(crdNames(selected)).To(Equal([]string{"Gadget.example.com"}))
	})

	t.Run("matches all groups of a kind", func(t *testing.T) {
		g := NewWithT(t)
		selected, err := selectCRDs(crds, []string{"Widget"}, nil, "crds.yaml")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(crdNames(selected)).To(Equal([]string{"Widget.example.com", "Widget.other.example.com"}))
	})

	t.Run("matches a kind by Kind.group", func(t *testing.T) {
		g := NewWithT(t)
		selected, err := selectCRDs(crds, []string{"widget.OTHER.example.com", " gadget "}, nil, "crds.yaml")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(crdNames(selected)).To(Equal([]string{"Gadget.example.com", "Widget.other.example.com"}))
	})

	t.Run("keeps duplicate CRDs of the manifest", func(t *testing.T) {
		g := NewWithT(t)
		dup := append(crds, newCRDEntry("Gadget", "example.com", "v2"))
		selected, err := selectCRDs(dup, []string{"gadget", "Gadget.example.com"}, nil, "crds.yaml")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(selected).To(HaveLen(2))
		g.Expect(selected[0].versions).To(Equal([]string{"v1"}))
		g.Expect(selected[1].versions).To(Equal([]string{"v2"}))
	})

	t.Run("fails for empty selectors", func(t *testing.T) {
		g := NewWithT(t)
		_, err := selectCRDs(crds, []string{"Gadget", " "}, nil, "crds.yaml")
		g.Expect(err).To(MatchError("the kind selector must not be empty"))
		_, err = selectCRDs(crds, nil, []string{""}, "crds.yaml")
		g.Expect(err).To(MatchError("the version selector must not be empty"))
	})

	t.Run("fails for unknown kind", func(t *testing.T) {
		g := NewWithT(t)
		_, err := selectCRDs(crds, []string{"Widgets"}, nil, "crds.yaml")
		g.Expect(err).To(MatchError(ContainSubstring(`kind "Widgets" not found in crds.yaml`)))
		g.Expect(err).To(MatchError(ContainSubstring("Gadget.example.com, Widget.example.com, Widget.other.example.com")))
	})

	t.Run("fails for unknown version", func(t *testing.T) {
		g := NewWithT(t)
		_, err := selectCRDs(crds, []string{"Gadget"}, []string{"v2"}, "crds.yaml")
		g.Expect(err).To(MatchError(ContainSubstring(`version "v2" not found in crds.yaml`)))
		g.Expect(err).To(MatchError(ContainSubstring("available versions: v1")))
	})

	t.Run("fails when a selected kind has no matching version", func(t *testing.T) {
		g := NewWithT(t)
		_, err := selectCRDs(crds, []string{"Widget.example.com", "Gadget"}, []string{"v1beta1"}, "crds.yaml")
		g.Expect(err).To(MatchError(ContainSubstring("no versions selected for Gadget.example.com")))
	})
}

func TestExtractCRDs(t *testing.T) {
	t.Run("defaults the singular name", func(t *testing.T) {
		g := NewWithT(t)
		crds, err := extractCRDs([]byte(strings.Replace(multiVersionCRD, "    singular: widget\n", "", 1)))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(crds).To(HaveLen(1))
		singular, _, err := unstructured.NestedString(crds[0].object.Object, "spec", "names", "singular")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(singular).To(Equal("widget"))
	})

	t.Run("fails when no versions are defined", func(t *testing.T) {
		g := NewWithT(t)
		manifest := multiVersionCRD[:strings.Index(multiVersionCRD, "  versions:")] + "  versions: []\n"
		_, err := extractCRDs([]byte(manifest))
		g.Expect(err).To(MatchError("CRD widgets.example.com defines no versions"))
	})

	t.Run("fails for names that are not path segments", func(t *testing.T) {
		g := NewWithT(t)
		_, err := extractCRDs([]byte(strings.Replace(multiVersionCRD, "group: example.com", "group: ../../escape", 1)))
		g.Expect(err).To(MatchError(`CRD widgets.example.com has an invalid group "../../escape"`))
		_, err = extractCRDs([]byte(strings.Replace(multiVersionCRD, "singular: widget", "singular: ..", 1)))
		g.Expect(err).To(MatchError(`CRD widgets.example.com has an invalid singular name ".."`))
		_, err = extractCRDs([]byte(strings.Replace(multiVersionCRD, "- name: v1beta1", "- name: v1/beta1", 1)))
		g.Expect(err).To(MatchError(`CRD widgets.example.com has an invalid version name "v1/beta1"`))
	})

	t.Run("ignores objects that are not CRDs", func(t *testing.T) {
		g := NewWithT(t)
		crds, err := extractCRDs([]byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: test\n"))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(crds).To(BeEmpty())
	})
}

func TestSelectCRDsFiltersVersions(t *testing.T) {
	g := NewWithT(t)

	crds, err := extractCRDs([]byte(multiVersionCRD))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(crds).To(HaveLen(1))
	g.Expect(crds[0].versions).To(Equal([]string{"v1beta1", "v1"}))

	selected, err := selectCRDs(crds, nil, []string{"V1"}, "crds.yaml")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(selected).To(HaveLen(1))
	g.Expect(selected[0].versions).To(Equal([]string{"v1"}))

	versions, _, err := unstructured.NestedSlice(selected[0].object.Object, "spec", "versions")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(versions).To(HaveLen(1))
	g.Expect(versions[0].(map[string]any)["name"]).To(Equal("v1"))

	// The original object is left untouched.
	g.Expect(crds[0].versions).To(Equal([]string{"v1beta1", "v1"}))
	original, _, err := unstructured.NestedSlice(crds[0].object.Object, "spec", "versions")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(original).To(HaveLen(2))
}

func TestVendorCrdSelect(t *testing.T) {
	g := NewWithT(t)

	tmpDir := t.TempDir()
	genPath := path.Join(tmpDir, "cue.mod", "gen")
	g.Expect(os.MkdirAll(path.Join(tmpDir, "cue.mod"), os.ModePerm)).To(Succeed())

	crdFile := path.Join(tmpDir, "widgets.yaml")
	g.Expect(os.WriteFile(crdFile, []byte(multiVersionCRD), 0644)).To(Succeed())

	output, err := executeCommand(fmt.Sprintf(
		"mod vendor crd %s -f %s --kind widget.example.com -v V1",
		tmpDir,
		crdFile,
	))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(output).To(ContainSubstring("example.com/widget/v1"))
	g.Expect(output).ToNot(ContainSubstring("example.com/widget/v1beta1"))

	g.Expect(path.Join(genPath, "example.com", "widget", "v1beta1")).ToNot(BeADirectory())

	generated, err := os.ReadFile(path.Join(genPath, "example.com", "widget", "v1", "types_gen.cue"))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(generated)).To(HavePrefix(generatedMarker))
	g.Expect(string(generated)).To(ContainSubstring(
		fmt.Sprintf("//timoni:generate timoni mod vendor crd -f %s --kind widget.example.com --version V1\n", crdFile)))
	g.Expect(string(generated)).To(ContainSubstring("package v1"))
	g.Expect(string(generated)).To(ContainSubstring("color?: string"))

	_, err = executeCommand(fmt.Sprintf(
		"mod vendor crd %s -f %s --kind Widget -v v2",
		tmpDir,
		crdFile,
	))
	g.Expect(err).To(MatchError(ContainSubstring(`version "v2" not found`)))
	g.Expect(err).To(MatchError(ContainSubstring("available versions: v1, v1beta1")))

	_, err = executeCommand(fmt.Sprintf(
		"mod vendor crd %s -f testdata/crd/source/flagger.crds.yaml --kind Canary,Gauge",
		tmpDir,
	))
	g.Expect(err).To(MatchError(ContainSubstring(`kind "Gauge" not found in testdata/crd/source/flagger.crds.yaml`)))
	g.Expect(path.Join(genPath, "flagger.app")).ToNot(BeADirectory())

	_, err = executeCommand(fmt.Sprintf("mod vendor crd %s -f %s --kind ,", tmpDir, crdFile))
	g.Expect(err).To(MatchError("the kind selector must not be empty"))

	noCRDs := path.Join(tmpDir, "namespace.yaml")
	g.Expect(os.WriteFile(noCRDs, []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: test\n"), 0644)).To(Succeed())
	_, err = executeCommand(fmt.Sprintf("mod vendor crd %s -f %s", tmpDir, noCRDs))
	g.Expect(err).To(MatchError(fmt.Sprintf("no CRDs found in %s", noCRDs)))
}

func TestVendorCrdList(t *testing.T) {
	g := NewWithT(t)

	tmpDir := t.TempDir()
	crdFile := path.Join(tmpDir, "widgets.yaml")
	g.Expect(os.WriteFile(crdFile, []byte(multiVersionCRD), 0644)).To(Succeed())

	// Listing does not require a CUE module.
	stdout, _, err := executeCommandWithOutErr(fmt.Sprintf("mod vendor crd -f %s --list", crdFile))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(stdout).To(ContainSubstring("KIND"))
	g.Expect(stdout).To(ContainSubstring("VERSION"))
	g.Expect(stdout).To(MatchRegexp(`Widget\.example\.com\s+v1\s`))
	g.Expect(stdout).To(MatchRegexp(`Widget\.example\.com\s+v1beta1\s`))
	g.Expect(path.Join(tmpDir, "cue.mod")).ToNot(BeADirectory())

	stdout, _, err = executeCommandWithOutErr(
		"mod vendor crd -f testdata/crd/source/cert-manager.crds.yaml --list --kind issuer,order.acme.cert-manager.io")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(stdout).To(MatchRegexp(`Issuer\.cert-manager\.io\s+v1\s`))
	g.Expect(stdout).To(MatchRegexp(`Order\.acme\.cert-manager\.io\s+v1\s`))
	g.Expect(stdout).ToNot(ContainSubstring("ClusterIssuer"))
	g.Expect(stdout).ToNot(ContainSubstring("Certificate"))

	err = executeCommandWithStreams(fmt.Sprintf("mod vendor crd -f %s --list --prune", crdFile),
		nil, io.Discard, io.Discard)
	g.Expect(err).To(MatchError(ContainSubstring("--list and --prune are mutually exclusive")))
}

func TestVendorCrdPrune(t *testing.T) {
	g := NewWithT(t)

	tmpDir := t.TempDir()
	genPath := path.Join(tmpDir, "cue.mod", "gen")
	g.Expect(os.MkdirAll(path.Join(tmpDir, "cue.mod"), os.ModePerm)).To(Succeed())

	for _, crdFile := range []string{
		"testdata/crd/source/cert-manager.crds.yaml",
		"testdata/crd/source/flagger.crds.yaml",
	} {
		_, err := executeCommand(fmt.Sprintf("mod vendor crd %s -f %s", tmpDir, crdFile))
		g.Expect(err).ToNot(HaveOccurred())
	}

	// A hand-written package in a vendored group must survive pruning.
	customDir := path.Join(genPath, "cert-manager.io", "custom", "v1")
	g.Expect(os.MkdirAll(customDir, os.ModePerm)).To(Succeed())
	g.Expect(os.WriteFile(path.Join(customDir, "types_gen.cue"), []byte("package v1\n"), 0644)).To(Succeed())

	// A hand-written file next to a generated one must survive pruning.
	extraFile := path.Join(genPath, "cert-manager.io", "certificaterequest", "v1", "extra.cue")
	g.Expect(os.WriteFile(extraFile, []byte("package v1\n"), 0644)).To(Succeed())

	// A symlinked group must not be followed.
	linkTarget := path.Join(tmpDir, "outside", "acme.cert-manager.io")
	g.Expect(os.MkdirAll(path.Dir(linkTarget), os.ModePerm)).To(Succeed())
	g.Expect(os.Rename(path.Join(genPath, "acme.cert-manager.io"), linkTarget)).To(Succeed())
	g.Expect(os.Symlink(linkTarget, path.Join(genPath, "acme.cert-manager.io"))).To(Succeed())

	output, err := executeCommand(fmt.Sprintf(
		"mod vendor crd %s -f testdata/crd/source/cert-manager.crds.yaml --kind Issuer --prune",
		tmpDir,
	))
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(output).To(ContainSubstring("schemas vendored: cert-manager.io/issuer/v1"))
	g.Expect(output).To(ContainSubstring("schemas pruned: cert-manager.io/certificate/v1"))
	g.Expect(output).To(ContainSubstring("schemas pruned: cert-manager.io/certificaterequest/v1"))
	g.Expect(output).ToNot(ContainSubstring("acme.cert-manager.io/order/v1"))
	g.Expect(output).To(ContainSubstring("schemas skipped: cert-manager.io/custom/v1"))

	g.Expect(path.Join(genPath, "cert-manager.io", "issuer", "v1", "types_gen.cue")).To(BeARegularFile())
	g.Expect(path.Join(genPath, "cert-manager.io", "custom", "v1", "types_gen.cue")).To(BeARegularFile())
	g.Expect(path.Join(genPath, "cert-manager.io", "certificate")).ToNot(BeADirectory())
	g.Expect(path.Join(genPath, "cert-manager.io", "clusterissuer")).ToNot(BeADirectory())
	g.Expect(path.Join(genPath, "cert-manager.io", "certificaterequest", "v1", "types_gen.cue")).ToNot(BeAnExistingFile())
	g.Expect(extraFile).To(BeARegularFile())
	g.Expect(path.Join(linkTarget, "order", "v1", "types_gen.cue")).To(BeARegularFile())
	g.Expect(path.Join(linkTarget, "challenge", "v1", "types_gen.cue")).To(BeARegularFile())

	// Groups absent from the manifest are left untouched.
	g.Expect(path.Join(genPath, "flagger.app", "canary", "v1beta1", "types_gen.cue")).To(BeARegularFile())
	g.Expect(path.Join(genPath, "flagger.app", "alertprovider", "v1beta1", "types_gen.cue")).To(BeARegularFile())
}
