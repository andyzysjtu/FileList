package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIOutputAndNoOverwrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "list.jsonl")
	args := []string{"--root", root, "--base", root, "--source-id", "archive", "--output", path}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), args, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatal("file output also wrote to stdout")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(before))
	var record map[string]any
	if err := decoder.Decode(&record); err != nil || record["record_type"] != "scan" {
		t.Fatalf("invalid JSONL: %s, %v", before, err)
	}
	foundOutput := false
	for {
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record["record_type"] == "entry" && record["path"] == "list.jsonl" {
			foundOutput = record["type"] == "file" && record["status"] == "ok"
		}
	}
	if !foundOutput {
		t.Fatal("CLI did not collect its output file")
	}
	if code := run(context.Background(), args, &stdout, &stderr); code != 1 {
		t.Fatalf("overwriting output should fail, got %d", code)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing output was modified")
	}
}

func TestCLIRedirectedStdoutIncludesOutput(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "redirected.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var stderr bytes.Buffer
	args := []string{"--root", root, "--base", root, "--source-id", "archive"}
	if code := run(context.Background(), args, file, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var record map[string]any
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			t.Fatal("redirected output file not collected")
		} else if err != nil {
			t.Fatal(err)
		}
		if record["record_type"] == "entry" && record["path"] == "redirected.jsonl" {
			if record["type"] != "file" || record["status"] != "ok" {
				t.Fatalf("unexpected output entry: %+v", record)
			}
			return
		}
	}
}

func TestCLIHelpAndRequiredOptions(t *testing.T) {
	for _, test := range []struct {
		args []string
		want int
	}{
		{[]string{"--help"}, 0},
		{nil, 1},
		{[]string{"--root", t.TempDir()}, 1},
		{[]string{"--unknown"}, 1},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), test.args, &stdout, &stderr); code != test.want {
			t.Errorf("run(%v) = %d; want %d", test.args, code, test.want)
		}
	}
}

func TestCLIStdoutAndCancellation(t *testing.T) {
	root := t.TempDir()
	args := []string{"--root", root, "--base", root, "--source-id", "archive"}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), args, &stdout, &stderr); code != 0 || stdout.Len() == 0 {
		t.Fatalf("stdout scan failed: %d, %s", code, stderr.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stdout.Reset()
	if code := run(ctx, args, &stdout, &stderr); code != 130 {
		t.Fatalf("cancellation exit = %d; want 130", code)
	}
}
