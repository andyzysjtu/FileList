package scan

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func modifiedTime(info os.FileInfo) time.Time { return info.ModTime().UTC() }

func readMetadata(_ string, info os.FileInfo) (Metadata, error) {
	stat := info.Sys().(*syscall.Stat_t)
	links, allocated := uint64(stat.Nlink), stat.Blocks*512
	return Metadata{
		AccessedAt:  utcTime(stat.Atim.Sec, stat.Atim.Nsec),
		ChangedAt:   utcTime(stat.Ctim.Sec, stat.Ctim.Nsec),
		Permissions: fmt.Sprintf("%04o", stat.Mode&07777),
		UID:         &stat.Uid, GID: &stat.Gid, HardLinks: &links, AllocatedBytes: &allocated,
		DeviceID: strconv.FormatUint(uint64(stat.Dev), 10),
	}, nil
}

func sameFilesystem(root, info os.FileInfo) bool {
	return root.Sys().(*syscall.Stat_t).Dev == info.Sys().(*syscall.Stat_t).Dev
}

func canDescend(_ os.FileInfo) bool { return true }

func resolveFilesystemPath(path string) (string, error) { return path, nil }

func volumeRoot(root string) (string, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	defer file.Close()
	return mountRoot(file, root)
}

func mountRoot(reader io.Reader, root string) (string, error) {
	lines := bufio.NewScanner(reader)
	lines.Buffer(make([]byte, 4096), 1024*1024)
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	best := ""
	for lines.Scan() {
		// Other whitespace can be part of a mount path, including Unicode spaces.
		fields := strings.FieldsFunc(lines.Text(), func(r rune) bool { return r == ' ' })
		if len(fields) < 10 {
			continue
		}
		mount := filepath.Clean(unescape.Replace(fields[4]))
		if !filepath.IsAbs(mount) {
			continue
		}
		if _, err := relativePath(mount, root); err == nil && len(mount) > len(best) {
			best = mount
		}
	}
	if err := lines.Err(); err != nil {
		return "", err
	}
	if best == "" {
		return "", errors.New("no containing mount point found")
	}
	return best, nil
}
