package scan

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Options struct {
	Root     string
	Base     string
	SourceID string
}

type scanner struct {
	ctx      context.Context
	encoder  *json.Encoder
	base     string
	rootInfo os.FileInfo
	summary  Summary
}

func Run(ctx context.Context, output io.Writer, options Options) (Summary, error) {
	if strings.TrimSpace(options.SourceID) == "" {
		return Summary{}, errors.New("source-id is required and must be reused for this data source")
	}
	if options.Root == "" {
		return Summary{}, errors.New("root is required")
	}
	root, err := canonicalDirectory(options.Root)
	if err != nil {
		return Summary{}, fmt.Errorf("resolve scan root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return Summary{}, fmt.Errorf("stat scan root: %w", err)
	}
	if !rootInfo.IsDir() {
		return Summary{}, errors.New("scan root must be a directory")
	}
	baseKind := "volume_root"
	var base string
	if options.Base != "" {
		baseKind = "explicit"
		base, err = canonicalDirectory(options.Base)
	} else {
		base, err = volumeRoot(root)
	}
	if err != nil {
		return Summary{}, fmt.Errorf("resolve path base (use --base to override): %w", err)
	}
	baseInfo, err := os.Stat(base)
	if err != nil || !baseInfo.IsDir() {
		return Summary{}, errors.New("path base must be an accessible directory")
	}
	relativeRoot, err := relativePath(base, root)
	if err != nil {
		return Summary{}, fmt.Errorf("scan root must be inside path base: %w", err)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Summary{}, fmt.Errorf("generate scan ID: %w", err)
	}
	scanID := hex.EncodeToString(id[:])
	s := scanner{
		ctx: ctx, encoder: json.NewEncoder(output), base: base,
		rootInfo: rootInfo,
		summary:  Summary{RecordType: "summary", ScanID: scanID},
	}
	s.encoder.SetEscapeHTML(false)
	header := Header{
		RecordType: "scan", SchemaVersion: SchemaVersion, ScanID: scanID,
		SourceID: options.SourceID, Platform: runtime.GOOS,
		MetadataProfile: "basic-v2", PathBase: baseKind, StartedAt: time.Now().UTC(),
	}
	header.Root, header.RootBytes = portableText(relativeRoot)
	if err := s.encoder.Encode(header); err != nil {
		return s.summary, err
	}
	err = s.walk(root)
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return s.summary, err
	}
	s.summary.Interrupted = err != nil
	s.summary.Complete = err == nil && s.summary.Errors == 0
	s.summary.FinishedAt = time.Now().UTC()
	if writeErr := s.encoder.Encode(s.summary); writeErr != nil {
		return s.summary, writeErr
	}
	return s.summary, err
}

func (s *scanner) walk(root string) error {
	pending := []string{root}
	for len(pending) > 0 {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		last := len(pending) - 1
		path := pending[last]
		pending[last] = ""
		pending = pending[:last]
		info, err := os.Lstat(path)
		if err != nil {
			relative, _ := relativePath(s.base, path)
			if err := s.recordError(relative, "stat", err); err != nil {
				return err
			}
			continue
		}
		descend, err := s.visit(path, info)
		if err != nil {
			return err
		}
		if !descend {
			continue
		}
		children, err := s.readDirectory(path)
		if err != nil {
			return err
		}
		pending = append(pending, children...)
	}
	return nil
}

func (s *scanner) visit(path string, info os.FileInfo) (bool, error) {
	if err := s.ctx.Err(); err != nil {
		return false, err
	}
	relative, err := relativePath(s.base, path)
	if err != nil {
		return false, err
	}
	entry := Entry{
		RecordType: "entry", Type: fileType(info.Mode()), Size: info.Size(),
		ModifiedAt: modifiedTime(info), Status: "ok",
	}
	entry.Path, entry.PathBytes = portableText(relative)
	metadata, metadataErr := readMetadata(path, info)
	entry.Metadata = metadata
	if metadataErr != nil {
		entry.Status = "partial"
	}
	var linkErr error
	if info.Mode()&os.ModeSymlink != 0 {
		var target string
		target, linkErr = os.Readlink(path)
		entry.LinkTarget, entry.LinkTargetBytes = portableText(target)
		if linkErr != nil {
			entry.Status = "partial"
		}
	}
	if err := s.encoder.Encode(entry); err != nil {
		return false, err
	}
	s.summary.Entries++
	switch entry.Type {
	case "file":
		s.summary.Files++
		if entry.Size > 0 {
			s.summary.FileBytes += uint64(entry.Size)
		}
	case "directory":
		s.summary.Directories++
	case "symlink":
		s.summary.Symlinks++
	default:
		s.summary.Other++
	}
	if metadataErr != nil {
		if err := s.recordError(relative, "read_metadata", metadataErr); err != nil {
			return false, err
		}
	}
	if linkErr != nil {
		if err := s.recordError(relative, "read_link", linkErr); err != nil {
			return false, err
		}
	}
	if !info.IsDir() {
		return false, nil
	}
	if !sameFilesystem(s.rootInfo, info) {
		return false, s.skip(relative, "different_filesystem")
	}
	if !canDescend(info) {
		return false, s.skip(relative, "reparse_point")
	}
	return true, nil
}

func (s *scanner) readDirectory(path string) ([]string, error) {
	relative, err := relativePath(s.base, path)
	if err != nil {
		return nil, err
	}
	directory, err := os.Open(path)
	if err != nil {
		return nil, s.recordError(relative, "read_directory", err)
	}
	defer directory.Close()
	var pending []string
	// Queue only directories; close this handle before descending into any of them.
	for {
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		children, readErr := directory.ReadDir(256)
		for _, child := range children {
			childPath := filepath.Join(path, child.Name())
			// Lstat afresh: the entry type may have changed since enumeration.
			childInfo, statErr := os.Lstat(childPath)
			if statErr != nil {
				childRelative, _ := relativePath(s.base, childPath)
				if err := s.recordError(childRelative, "stat", statErr); err != nil {
					return nil, err
				}
				continue
			}
			if childInfo.IsDir() {
				pending = append(pending, childPath)
				continue
			}
			if _, err := s.visit(childPath, childInfo); err != nil {
				return nil, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return pending, nil
		}
		if readErr != nil {
			return pending, s.recordError(relative, "read_directory", readErr)
		}
	}
}

func (s *scanner) skip(path, reason string) error {
	record := SkipRecord{RecordType: "skip", Reason: reason}
	record.Path, record.PathBytes = portableText(path)
	s.summary.Skipped++
	return s.encoder.Encode(record)
}

func (s *scanner) recordError(path, operation string, err error) error {
	record := ErrorRecord{RecordType: "error", Operation: operation, Code: "io_error"}
	record.Path, record.PathBytes = portableText(path)
	switch {
	case errors.Is(err, fs.ErrPermission):
		record.Code = "permission_denied"
	case errors.Is(err, fs.ErrNotExist):
		record.Code = "not_found"
	}
	// Keep host-specific absolute paths out of portable error records.
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		err = linkErr.Err
	}
	record.Message = err.Error()
	s.summary.Errors++
	return s.encoder.Encode(record)
}

func fileType(mode os.FileMode) string {
	switch {
	case mode.IsRegular():
		return "file"
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsDir():
		return "directory"
	case mode&os.ModeNamedPipe != 0:
		return "fifo"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeDevice != 0:
		return "device"
	default:
		return "other"
	}
}
