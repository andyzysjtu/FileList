package scan

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestMountRoot(t *testing.T) {
	mountinfo := "10 1 8:1 / / rw - ext4 /dev/root rw\n" +
		"11 10 8:2 / /media/archive rw - exfat /dev/sdb1 rw\n" +
		`12 10 8:3 / /media/archive\040two rw - exfat /dev/sdc1 rw` + "\n" +
		"13 11 8:4 / /media/archive/nested rw - ext4 /dev/sdd1 rw\n" +
		`14 10 8:5 / /media/back\134slash rw - ext4 /dev/sde1 rw` + "\n"
	mountinfo += "15 10 8:6 / /media/archive\u00a0two rw - exfat /dev/sdf1 rw\n" +
		"16 10 8:7 / /media/archive\u2009three rw - exfat /dev/sdg1 rw\n" +
		"17 10 8:8 / /media/archive\rfour rw - exfat /dev/sdh1 rw\n"
	for _, test := range []struct{ root, want string }{
		{"/media/archive/photos", "/media/archive"},
		{"/media/archive/nested/photos", "/media/archive/nested"},
		{"/media/archive two/photos", "/media/archive two"},
		{"/media/archive-extra/photos", "/"},
		{`/media/back\slash/photos`, `/media/back\slash`},
		{"/media/archive\u00a0two/photos", "/media/archive\u00a0two"},
		{"/media/archive\u2009three/photos", "/media/archive\u2009three"},
		{"/media/archive\rfour/photos", "/media/archive\rfour"},
	} {
		got, err := mountRoot(strings.NewReader(mountinfo), test.root)
		if err != nil || got != test.want {
			t.Errorf("mountRoot(%q) = %q, %v; want %q", test.root, got, err, test.want)
		}
	}
}

func TestDeepTreeWithLimitedOpenFiles(t *testing.T) {
	const environmentKey = "FILELIST_TEST_LOW_NOFILE_ROOT"
	if root := os.Getenv(environmentKey); root != "" {
		var limit syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
			t.Fatal(err)
		}
		limit.Cur = min(limit.Max, 64)
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
			t.Fatal(err)
		}
		summary, err := Run(context.Background(), io.Discard, Options{Root: root, Base: root, SourceID: "disk"})
		if err != nil || !summary.Complete || summary.Directories != 91 || summary.Files != 1 {
			t.Fatalf("deep scan failed with a low file limit: %+v, %v", summary, err)
		}
		return
	}

	root := t.TempDir()
	deepest := root
	for range 90 {
		deepest = filepath.Join(deepest, "d")
	}
	if err := os.MkdirAll(deepest, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(deepest, "leaf.txt"), "data")
	// Lower the limit in a subprocess so other tests retain their original limit.
	command := exec.Command(os.Args[0], "-test.run=^TestDeepTreeWithLimitedOpenFiles$")
	command.Env = append(os.Environ(), environmentKey+"="+root)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("low-limit subprocess failed: %v\n%s", err, output)
	}
}

func TestInvalidUTF8FilenameIsLossless(t *testing.T) {
	root := t.TempDir()
	name := "raw-\xff.bin"
	writeFile(t, filepath.Join(root, name), "bytes")
	var output bytes.Buffer
	if _, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "disk"}); err != nil {
		t.Fatal(err)
	}
	entry := entries(t, output.Bytes())["raw-\uFFFD.bin"]
	if !bytes.Equal(entry.PathBytes, []byte(name)) {
		t.Fatalf("lost filename bytes: %+v", entry)
	}
}

func TestUnreadableFileContentsAreNotOpened(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "private.txt")
	writeFile(t, path, "private")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	summary, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "disk"})
	if err != nil || !summary.Complete || summary.Files != 1 {
		t.Fatalf("metadata should not require file content access: %+v, %v", summary, err)
	}
	if entry := entries(t, output.Bytes())["private.txt"]; entry.Permissions != "0000" {
		t.Fatalf("incorrect permissions: %+v", entry)
	}
}

func TestUnreadableDirectoryMakesScanIncomplete(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(locked, "hidden.txt"), "hidden")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0700) })
	var output bytes.Buffer
	summary, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "disk"})
	if err != nil || summary.Complete || summary.Errors != 1 {
		t.Fatalf("unexpected partial scan: %+v, %v", summary, err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"code":"permission_denied"`)) {
		t.Fatalf("missing permission error: %s", output.String())
	}
}

func TestFIFOIsRecordedWithoutOpening(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	summary, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "disk"})
	if err != nil || !summary.Complete || summary.Other != 1 {
		t.Fatalf("unexpected FIFO scan: %+v, %v", summary, err)
	}
	if entry := entries(t, output.Bytes())["pipe"]; entry.Type != "fifo" {
		t.Fatalf("unexpected FIFO entry: %+v", entry)
	}
}
