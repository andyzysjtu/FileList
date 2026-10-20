package scan

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinFirmlinkPaths(t *testing.T) {
	for _, path := range []string{"/Users", "/private/var"} {
		t.Run(path, func(t *testing.T) {
			physical := filepath.Join("/System/Volumes/Data", strings.TrimPrefix(path, "/"))
			logicalInfo, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			physicalInfo, err := os.Stat(physical)
			if os.IsNotExist(err) {
				t.Skip("this macOS installation does not use the standard Data volume layout")
			}
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(logicalInfo, physicalInfo) {
				t.Skip("path is not a firmlink into the Data volume")
			}
			resolved, err := canonicalDirectory(path)
			if err != nil {
				t.Fatal(err)
			}
			if resolved != physical {
				t.Fatalf("canonicalDirectory(%q) = %q; want %q", path, resolved, physical)
			}
			base, err := volumeRoot(resolved)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := relativePath(base, resolved); err != nil {
				t.Fatalf("resolved path %q is outside volume root %q: %v", resolved, base, err)
			}
		})
	}
}

func TestDarwinFirmlinkScanPathsAreStable(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(root, "/private/var/") && !strings.HasPrefix(root, "/Users/") {
		t.Skip("temporary directory is not under a standard Data volume firmlink")
	}
	physical := filepath.Join("/System/Volumes/Data", strings.TrimPrefix(root, "/"))
	logicalInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	physicalInfo, err := os.Stat(physical)
	if os.IsNotExist(err) {
		t.Skip("this macOS installation does not use the standard Data volume layout")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(logicalInfo, physicalInfo) {
		t.Skip("temporary directory is not a firmlink into the Data volume")
	}
	writeFile(t, filepath.Join(root, "item.txt"), "data")
	base, err := volumeRoot(physical)
	if err != nil {
		t.Fatal(err)
	}
	want, err := relativePath(base, filepath.Join(physical, "item.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, root, base, want string }{
		{"logical", root, "", want},
		{"physical", physical, "", want},
		{"logical root and physical base", root, physical, "item.txt"},
		{"physical root and logical base", physical, root, "item.txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			summary, err := Run(context.Background(), &output, Options{
				Root: test.root, Base: test.base, SourceID: "disk",
			})
			if err != nil || !summary.Complete || summary.Files != 1 || summary.Directories != 1 {
				t.Fatalf("firmlink scan failed: %+v, %v", summary, err)
			}
			entry := entries(t, output.Bytes())[test.want]
			if entry.Type != "file" || entry.Size != 4 || entry.Status != "ok" {
				t.Fatalf("expected inventory path %q, got output:\n%s", test.want, output.String())
			}
		})
	}
}
