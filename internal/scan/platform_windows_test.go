package scan

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsTimestampRange(t *testing.T) {
	for _, want := range []time.Time{
		time.Date(1602, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1677, 1, 1, 0, 0, 0, 100, time.UTC),
		time.Unix(0, 0).UTC(),
		time.Date(2026, 10, 19, 7, 40, 5, 44500900, time.UTC),
		time.Date(2262, 12, 31, 23, 59, 59, 999999900, time.UTC),
		time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, 12, 31, 23, 59, 59, 999999900, time.UTC),
	} {
		t.Run(want.Format(time.RFC3339Nano), func(t *testing.T) {
			value := testFiletime(want)
			got := windowsTime(value)
			if got == nil || !got.Equal(want) {
				t.Fatalf("decoded FILETIME = %v; want %v", got, want)
			}
		})
	}
	if got := windowsTime(syscall.Filetime{}); got != nil {
		t.Fatalf("zero FILETIME should be omitted, got %v", got)
	}
}

func testFiletime(value time.Time) syscall.Filetime {
	ticks := uint64(value.Unix()+11644473600)*10000000 + uint64(value.Nanosecond()/100)
	return syscall.Filetime{LowDateTime: uint32(ticks), HighDateTime: uint32(ticks >> 32)}
}

func TestWindowsScanPreservesEarlyTimestamps(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "early.txt")
	writeFile(t, path, "early")
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(1602, 1, 1, 0, 0, 0, 0, time.UTC)
	value := testFiletime(want)
	filetime := windows.Filetime{LowDateTime: value.LowDateTime, HighDateTime: value.HighDateTime}
	setErr := windows.SetFileTime(handle, &filetime, &filetime, &filetime)
	closeErr := windows.CloseHandle(handle)
	if setErr != nil || closeErr != nil {
		t.Fatalf("set timestamps: %v; close handle: %v", setErr, closeErr)
	}
	var output bytes.Buffer
	summary, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "disk"})
	if err != nil || !summary.Complete {
		t.Fatalf("scan failed: %+v, %v", summary, err)
	}
	entry := entries(t, output.Bytes())["early.txt"]
	if !entry.ModifiedAt.Equal(want) || entry.CreatedAt == nil || !entry.CreatedAt.Equal(want) || entry.AccessedAt == nil || !entry.AccessedAt.Equal(want) {
		t.Fatalf("early timestamps were not preserved: %+v", entry)
	}
}

func TestExtendedWindowsPath(t *testing.T) {
	for _, test := range []struct{ path, want string }{
		{`E:\Photos\item.jpg`, `\\?\E:\Photos\item.jpg`},
		{`\\server\share\item.jpg`, `\\?\UNC\server\share\item.jpg`},
		{`\\?\E:\Photos\item.jpg`, `\\?\E:\Photos\item.jpg`},
	} {
		if got := extendedPath(test.path); got != test.want {
			t.Errorf("extendedPath(%q) = %q; want %q", test.path, got, test.want)
		}
	}
}

func TestWindowsLongPaths(t *testing.T) {
	root := t.TempDir()
	directory := root
	for range 4 {
		directory = filepath.Join(directory, strings.Repeat("a", 70))
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "item.txt")
	writeFile(t, path, "long path")
	var output bytes.Buffer
	summary, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "disk"})
	if err != nil || !summary.Complete || summary.Files != 1 {
		t.Fatalf("long-path scan failed: %+v, %v\n%s", summary, err, output.String())
	}
	relative, err := relativePath(root, path)
	if err != nil {
		t.Fatal(err)
	}
	entry := entries(t, output.Bytes())[relative]
	if entry.WindowsAttributes == nil || entry.CreatedAt == nil || entry.HardLinks == nil {
		t.Fatalf("missing native Windows metadata: %+v", entry)
	}
}

func TestWindowsJunctionIsNotFollowed(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(outside, "outside.txt"), "outside")
	link := filepath.Join(root, "junction")
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Skipf("junctions unavailable: %v: %s", err, output)
	}
	var output bytes.Buffer
	summary, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "disk"})
	if err != nil || !summary.Complete || summary.Files != 0 {
		t.Fatalf("junction traversed: %+v, %v\n%s", summary, err, output.String())
	}
	if entry := entries(t, output.Bytes())["junction"]; entry.Path == "" {
		t.Fatalf("junction metadata missing: %s", output.String())
	}
}
