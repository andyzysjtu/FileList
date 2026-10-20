package scan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func records(t *testing.T, data []byte) []map[string]json.RawMessage {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var result []map[string]json.RawMessage
	for {
		var record map[string]json.RawMessage
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return result
		}
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, record)
	}
}

func entries(t *testing.T, data []byte) map[string]Entry {
	t.Helper()
	result := make(map[string]Entry)
	for _, record := range records(t, data) {
		if string(record["record_type"]) != `"entry"` {
			continue
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		var entry Entry
		if err := json.Unmarshal(encoded, &entry); err != nil {
			t.Fatal(err)
		}
		result[entry.Path] = entry
	}
	return result
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestMetadataScan(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "photos")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	name := "\u65c5\u884c.jpg"
	path := filepath.Join(directory, name)
	writeFile(t, path, "example content")
	modified := time.Date(2025, 7, 8, 9, 10, 12, 0, time.UTC)
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "empty"), "")
	var output bytes.Buffer
	summary, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "archive-01"})
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Complete || summary.Errors != 0 || summary.Files != 2 || summary.Directories != 2 || summary.FileBytes != 15 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	all := records(t, output.Bytes())
	if string(all[0]["record_type"]) != `"scan"` || string(all[0]["source_id"]) != `"archive-01"` || string(all[0]["root"]) != `"."` {
		t.Fatalf("unexpected header: %s", output.String())
	}
	if string(all[0]["schema_version"]) != "2" || string(all[0]["metadata_profile"]) != `"basic-v2"` {
		t.Fatalf("unexpected schema/profile: %+v", all[0])
	}
	if string(all[len(all)-1]["record_type"]) != `"summary"` || !bytes.Equal(all[0]["scan_id"], all[len(all)-1]["scan_id"]) {
		t.Fatal("scan must end in a matching summary")
	}
	files := entries(t, output.Bytes())
	entry := files["photos/"+name]
	if entry.Type != "file" || entry.Size != 15 || !entry.ModifiedAt.Equal(modified) || entry.Status != "ok" {
		t.Fatalf("unexpected file: %+v", entry)
	}
	if entry.HardLinks == nil || *entry.HardLinks != 1 {
		t.Fatalf("unexpected hard-link count: %+v", entry)
	}
	for _, record := range all {
		for _, key := range []string{"fingerprint", "hash", "digest", "observed_at", "file_id"} {
			if _, ok := record[key]; ok {
				t.Fatalf("metadata scan must not produce %s", key)
			}
		}
	}
	if bytes.Contains(output.Bytes(), []byte(filepath.ToSlash(root))) {
		t.Fatal("output must not include the host's absolute scan root")
	}
}

func TestSubsetKeepsBase(t *testing.T) {
	base := t.TempDir()
	subdir := filepath.Join(base, "collection")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(subdir, "item.txt"), "item")
	var all, subset bytes.Buffer
	for _, test := range []struct {
		root   string
		output *bytes.Buffer
	}{{base, &all}, {subdir, &subset}} {
		if _, err := Run(context.Background(), test.output, Options{Root: test.root, Base: base, SourceID: "stable"}); err != nil {
			t.Fatal(err)
		}
	}
	wholeEntry := entries(t, all.Bytes())["collection/item.txt"]
	partEntry := entries(t, subset.Bytes())["collection/item.txt"]
	if partEntry.Path == "" || partEntry.Path != wholeEntry.Path || partEntry.Size != wholeEntry.Size || !partEntry.ModifiedAt.Equal(wholeEntry.ModifiedAt) {
		t.Fatalf("subset changed path/metadata: %+v, %+v", wholeEntry, partEntry)
	}
}

func TestBatchedDirectoryTraversal(t *testing.T) {
	root := t.TempDir()
	for index := range 300 {
		path := filepath.Join(root, "item-"+strconv.Itoa(index))
		if index%2 == 0 {
			if err := os.Mkdir(path, 0755); err != nil {
				t.Fatal(err)
			}
			path = filepath.Join(path, "nested.txt")
		}
		writeFile(t, path, "data")
	}
	var output bytes.Buffer
	summary, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "disk"})
	if err != nil || !summary.Complete || summary.Files != 300 || summary.Directories != 151 || summary.Entries != 451 {
		t.Fatalf("batched traversal missed entries: %+v, %v", summary, err)
	}
	if got := len(entries(t, output.Bytes())); got != 451 {
		t.Fatalf("collected %d distinct paths; want 451", got)
	}
}

func TestAutomaticVolumeBase(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "item.txt"), "item")
	canonicalRoot, err := canonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	base, err := volumeRoot(canonicalRoot)
	if err != nil {
		t.Fatal(err)
	}
	want, err := relativePath(base, filepath.Join(canonicalRoot, "item.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := Run(context.Background(), &output, Options{Root: root, SourceID: "disk"}); err != nil {
		t.Fatal(err)
	}
	entry := entries(t, output.Bytes())[want]
	if entry.Path == "" || filepath.VolumeName(entry.Path) != "" || strings.HasPrefix(entry.Path, "/") {
		t.Fatalf("invalid automatic relative path: %+v, want %q", entry, want)
	}
	if first := records(t, output.Bytes())[0]; string(first["path_base"]) != `"volume_root"` {
		t.Fatalf("unexpected base kind: %s", first["path_base"])
	}
}

func TestOutputAndItsHardLinksAreIncluded(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "output.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	files := uint64(2)
	hasHardLink := false
	if err := os.Link(path, filepath.Join(root, "output-link.jsonl")); err == nil {
		files++
		hasHardLink = true
	}
	writeFile(t, filepath.Join(root, "input.txt"), "keep")
	summary, err := Run(context.Background(), file, Options{Root: root, Base: root, SourceID: "disk"})
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Complete || summary.Files != files || summary.Skipped != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	collected := entries(t, data)
	if entry := collected["output.jsonl"]; entry.Type != "file" || entry.Status != "ok" {
		t.Fatalf("output file not collected: %+v", entry)
	}
	if hasHardLink {
		entry := collected["output-link.jsonl"]
		if entry.Type != "file" || entry.Status != "ok" || entry.HardLinks == nil || *entry.HardLinks != 2 {
			t.Fatalf("output hard link not collected: %+v", entry)
		}
	}
}

func TestSymlinksAreNotFollowed(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(outside, "not-in-source.txt"), "outside")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	summary, err := Run(context.Background(), &output, Options{Root: root, Base: root, SourceID: "disk"})
	if err != nil {
		t.Fatal(err)
	}
	if !summary.Complete || summary.Files != 0 || summary.Symlinks != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	files := entries(t, output.Bytes())
	if files["broken"].LinkTarget != "missing" || files["link"].Type != "symlink" {
		t.Fatalf("missing link metadata: %+v", files)
	}
}

func TestInvalidOptionsProduceNoRecords(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	file := filepath.Join(root, "file.txt")
	writeFile(t, file, "file")
	for _, options := range []Options{
		{Root: root},
		{SourceID: "disk"},
		{Root: filepath.Join(root, "missing"), SourceID: "disk"},
		{Root: file, SourceID: "disk"},
		{Root: root, Base: other, SourceID: "disk"},
	} {
		var output bytes.Buffer
		if _, err := Run(context.Background(), &output, options); err == nil {
			t.Fatalf("expected invalid options: %+v", options)
		}
		if output.Len() != 0 {
			t.Fatal("invalid options produced partial records")
		}
	}
}

func TestCancellationProducesIncompleteSummary(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	summary, err := Run(ctx, &output, Options{Root: root, Base: root, SourceID: "disk"})
	if !errors.Is(err, context.Canceled) || summary.Complete || !summary.Interrupted {
		t.Fatalf("unexpected cancellation result: %+v, %v", summary, err)
	}
	all := records(t, output.Bytes())
	if len(all) != 2 || string(all[1]["complete"]) != "false" {
		t.Fatalf("missing incomplete summary: %s", output.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestOutputFailureIsFatal(t *testing.T) {
	root := t.TempDir()
	if _, err := Run(context.Background(), failingWriter{}, Options{Root: root, Base: root, SourceID: "disk"}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("expected output failure, got %v", err)
	}
}

func TestErrorsArePortableAndCounted(t *testing.T) {
	var output bytes.Buffer
	s := scanner{encoder: json.NewEncoder(&output)}
	err := &os.PathError{Op: "open", Path: "/private/machine/secret", Err: os.ErrPermission}
	if err := s.recordError("private/item", "read_directory", err); err != nil {
		t.Fatal(err)
	}
	if s.summary.Errors != 1 || strings.Contains(output.String(), "machine") || !strings.Contains(output.String(), "permission_denied") {
		t.Fatalf("unexpected error record: %s", output.String())
	}
}

func TestRelativePathRejectsEscape(t *testing.T) {
	base := t.TempDir()
	if _, err := relativePath(base, filepath.Dir(base)); err == nil {
		t.Fatal("accepted path outside base")
	}
}
