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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	. "github.com/onsi/gomega"
)

func Test_BundleUpdate(t *testing.T) {
	g := NewWithT(t)

	modName := rnd("my-mod")
	modURL := fmt.Sprintf("%s/%s", dockerRegistry, modName)

	for _, ver := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		_, err := executeCommand(fmt.Sprintf(
			"mod push testdata/module oci://%s -v %s --resolve-symlinks",
			modURL, ver,
		))
		g.Expect(err).ToNot(HaveOccurred())
	}

	digestOf := func(version string) string {
		digest, err := crane.Digest(fmt.Sprintf("%s:%s", modURL, version))
		g.Expect(err).ToNot(HaveOccurred())
		return digest
	}

	writeBundle := func(content string) string {
		path := filepath.Join(t.TempDir(), "bundle.cue")
		g.Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
		return path
	}

	readFile := func(path string) string {
		data, err := os.ReadFile(path)
		g.Expect(err).ToNot(HaveOccurred())
		return string(data)
	}

	bundleData := fmt.Sprintf(`
bundle: {
	apiVersion: "v1alpha1"
	name:       "test"
	instances: {
		pinned: {
			module: {
				url:     "oci://%[1]s"
				version: "1.0.0" @timoni(update:semver:1.x)
				digest:  "%[2]s"
			}
			namespace: "test"
			values: priority: 10
		}
		unmarked: {
			module: url:     "oci://%[1]s"
			module: version: "1.0.0"
			namespace: "test"
			values: priority: 10
		}
	}
}
`, modURL, digestOf("1.0.0"))

	t.Run("prints the updates without modifying the files", func(t *testing.T) {
		g := NewWithT(t)
		bundlePath := writeBundle(bundleData)

		output, err := executeCommand(fmt.Sprintf("bundle update -f %s --dry-run", bundlePath))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(output).To(ContainSubstring(fmt.Sprintf("pinned: oci://%s 1.0.0@%s -> 1.1.0@%s", modURL, digestOf("1.0.0"), digestOf("1.1.0"))))
		g.Expect(output).To(ContainSubstring("1 module reference(s) can be updated"))
		g.Expect(output).To(ContainSubstring("(dry run)"))
		g.Expect(output).To(ContainSubstring("instance unmarked skipped: update policy is none"))
		g.Expect(readFile(bundlePath)).To(Equal(bundleData))
	})

	t.Run("updates the files according to the attributes", func(t *testing.T) {
		g := NewWithT(t)
		bundlePath := writeBundle(bundleData)

		output, err := executeCommand(fmt.Sprintf("bundle update -f %s", bundlePath))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(output).To(ContainSubstring("updated"))

		updated := readFile(bundlePath)
		g.Expect(updated).To(ContainSubstring(`version: "1.1.0" @timoni(update:semver:1.x)`))
		g.Expect(updated).To(ContainSubstring(fmt.Sprintf(`digest:  "%s"`, digestOf("1.1.0"))))
		g.Expect(updated).To(ContainSubstring(`module: version: "1.0.0"`))

		// The updated bundle is valid and its instances build with the new module version.
		_, err = executeCommand(fmt.Sprintf("bundle build -f %s", bundlePath))
		g.Expect(err).ToNot(HaveOccurred())

		// A second run finds the bundle up to date.
		output, err = executeCommand(fmt.Sprintf("bundle update -f %s", bundlePath))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(output).To(ContainSubstring("all module references are up to date"))
		g.Expect(readFile(bundlePath)).To(Equal(updated))
	})

	t.Run("updates the unmarked references to the level", func(t *testing.T) {
		g := NewWithT(t)
		bundlePath := writeBundle(bundleData)

		_, err := executeCommand(fmt.Sprintf("bundle update -f %s --level minor", bundlePath))
		g.Expect(err).ToNot(HaveOccurred())

		updated := readFile(bundlePath)
		g.Expect(updated).To(ContainSubstring(`version: "1.1.0" @timoni(update:semver:1.x)`))
		g.Expect(updated).To(ContainSubstring(`module: version: "1.1.0"`))
	})

	t.Run("updates across major versions with an open constraint", func(t *testing.T) {
		g := NewWithT(t)
		bundlePath := writeBundle(strings.Replace(bundleData, "update:semver:1.x", "update:semver:*", 1))

		_, err := executeCommand(fmt.Sprintf("bundle update -f %s", bundlePath))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(readFile(bundlePath)).To(ContainSubstring(`version: "2.0.0" @timoni(update:semver:*)`))
	})

	t.Run("updates the package files of a CUE module", func(t *testing.T) {
		g := NewWithT(t)
		dir := t.TempDir()
		write := func(name, content string) string {
			path := filepath.Join(dir, name)
			g.Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			g.Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
			return path
		}
		write("cue.mod/module.cue", "module: \"example.com/fleet\"\nlanguage: version: \"v0.14.0\"\n")
		appFile := write("apps/app.cue", fmt.Sprintf(`package apps

instances: app: {
	module: url:     "oci://%s"
	module: version: "1.0.0" @timoni(update:semver:1.x)
	namespace: "test"
	values: priority: 10
}
`, modURL))
		entryData := `import "example.com/fleet/apps"

bundle: {
	apiVersion: "v1alpha1"
	name:       "apps"
	instances:  apps.instances
}
`
		entry := write("apps/bundle.cue", entryData)

		output, err := executeCommand(fmt.Sprintf("bundle update -f %s --workdir %s", entry, dir))
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(output).To(ContainSubstring("app: oci://" + modURL + " 1.0.0 -> 1.1.0"))
		g.Expect(readFile(appFile)).To(ContainSubstring(`version: "1.1.0" @timoni(update:semver:1.x)`))
		g.Expect(readFile(entry)).To(Equal(entryData))

		_, err = executeCommand(fmt.Sprintf("bundle build -f %s --workdir %s", entry, dir))
		g.Expect(err).ToNot(HaveOccurred())
	})

	t.Run("does not write when a module cannot be listed", func(t *testing.T) {
		g := NewWithT(t)
		unreachable := strings.Replace(bundleData, "module: version: \"1.0.0\"", "module: version: \"1.0.0\" @timoni(update:semver:*)", 1)
		unreachable = strings.Replace(unreachable, fmt.Sprintf("unmarked: {\n\t\t\tmodule: url:     \"oci://%s\"", modURL), fmt.Sprintf("unmarked: {\n\t\t\tmodule: url:     \"oci://%s-missing\"", modURL), 1)
		bundlePath := writeBundle(unreachable)

		_, err := executeCommand(fmt.Sprintf("bundle update -f %s", bundlePath))
		g.Expect(err).To(MatchError(ContainSubstring("instances unmarked: listing versions of")))
		g.Expect(readFile(bundlePath)).To(Equal(unreachable))
	})
}
