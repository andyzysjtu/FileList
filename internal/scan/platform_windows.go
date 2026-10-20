package scan

import (
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func modifiedTime(info os.FileInfo) time.Time {
	return filetimeToTime(info.Sys().(*syscall.Win32FileAttributeData).LastWriteTime)
}

func readMetadata(path string, info os.FileInfo) (Metadata, error) {
	attributes := info.Sys().(*syscall.Win32FileAttributeData)
	metadata := Metadata{
		AccessedAt:        windowsTime(attributes.LastAccessTime),
		CreatedAt:         windowsTime(attributes.CreationTime),
		WindowsAttributes: &attributes.FileAttributes,
	}
	name, err := windows.UTF16PtrFromString(extendedPath(path))
	if err != nil {
		return metadata, err
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return metadata, err
	}
	defer windows.CloseHandle(handle)
	var basic windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &basic); err != nil {
		return metadata, err
	}
	links := uint64(basic.NumberOfLinks)
	metadata.HardLinks = &links
	var times struct {
		CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
		FileAttributes                                          uint32
		Padding                                                 uint32
	}
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileBasicInfo,
		(*byte)(unsafe.Pointer(&times)), uint32(unsafe.Sizeof(times))); err == nil && times.ChangeTime != 0 {
		value := uint64(times.ChangeTime)
		metadata.ChangedAt = windowsTime(syscall.Filetime{
			LowDateTime: uint32(value), HighDateTime: uint32(value >> 32),
		})
	}
	return metadata, nil
}

func windowsTime(value syscall.Filetime) *time.Time {
	if value.LowDateTime == 0 && value.HighDateTime == 0 {
		return nil
	}
	t := filetimeToTime(value)
	return &t
}

func filetimeToTime(value syscall.Filetime) time.Time {
	const ticksPerSecond = 10000000
	const unixEpochOffset = 11644473600
	ticks := uint64(value.HighDateTime)<<32 | uint64(value.LowDateTime)
	// Split before converting: Unix nanoseconds overflow for dates outside 1677-2262.
	seconds := int64(ticks/ticksPerSecond) - unixEpochOffset
	nanoseconds := int64(ticks%ticksPerSecond) * 100
	return time.Unix(seconds, nanoseconds).UTC()
}

func sameFilesystem(_, _ os.FileInfo) bool { return true }

func canDescend(info os.FileInfo) bool {
	return info.Sys().(*syscall.Win32FileAttributeData).FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0
}

func resolveFilesystemPath(path string) (string, error) { return path, nil }

func volumeRoot(root string) (string, error) {
	name, err := windows.UTF16PtrFromString(extendedPath(root))
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, 32768)
	if err := windows.GetVolumePathName(name, &buffer[0], uint32(len(buffer))); err != nil {
		return "", err
	}
	path := windows.UTF16ToString(buffer)
	if strings.HasPrefix(path, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(path, `\\?\UNC\`), nil
	}
	return strings.TrimPrefix(path, `\\?\`), nil
}

func extendedPath(path string) string {
	if strings.HasPrefix(path, `\\?\`) {
		return path
	}
	if strings.HasPrefix(path, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(path, `\\`)
	}
	return `\\?\` + path
}
