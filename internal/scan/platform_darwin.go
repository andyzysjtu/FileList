package scan

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func modifiedTime(info os.FileInfo) time.Time { return info.ModTime().UTC() }

func readMetadata(_ string, info os.FileInfo) (Metadata, error) {
	stat := info.Sys().(*syscall.Stat_t)
	links, allocated := uint64(stat.Nlink), stat.Blocks*512
	return Metadata{
		AccessedAt:  utcTime(stat.Atimespec.Sec, stat.Atimespec.Nsec),
		CreatedAt:   utcTime(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec),
		ChangedAt:   utcTime(stat.Ctimespec.Sec, stat.Ctimespec.Nsec),
		Permissions: fmt.Sprintf("%04o", stat.Mode&07777),
		UID:         &stat.Uid, GID: &stat.Gid, HardLinks: &links, AllocatedBytes: &allocated,
		DeviceID: strconv.FormatUint(uint64(uint32(stat.Dev)), 10),
	}, nil
}

func sameFilesystem(root, info os.FileInfo) bool {
	return root.Sys().(*syscall.Stat_t).Dev == info.Sys().(*syscall.Stat_t).Dev
}

func canDescend(_ os.FileInfo) bool { return true }

func resolveFilesystemPath(path string) (string, error) {
	// Event-only access resolves the vnode without requiring directory read access.
	fd, err := unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer unix.Close(fd)
	var buffer [unix.PathMax]byte
	// EvalSymlinks leaves firmlinks intact; statfs reports their physical volume.
	_, _, errno := unix.Syscall(unix.SYS_FCNTL, uintptr(fd), unix.F_GETPATH_NOFIRMLINK,
		uintptr(unsafe.Pointer(&buffer[0])))
	if errno != 0 {
		return "", &os.PathError{Op: "fcntl", Path: path, Err: errno}
	}
	return unix.ByteSliceToString(buffer[:]), nil
}

func volumeRoot(root string) (string, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return "", err
	}
	var mount []byte
	for _, value := range stat.Mntonname {
		if value == 0 {
			break
		}
		mount = append(mount, byte(value))
	}
	return string(mount), nil
}
