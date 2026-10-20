package scan

import "time"

const SchemaVersion = 2

type Header struct {
	RecordType      string    `json:"record_type"`
	SchemaVersion   int       `json:"schema_version"`
	ScanID          string    `json:"scan_id"`
	SourceID        string    `json:"source_id"`
	Platform        string    `json:"platform"`
	MetadataProfile string    `json:"metadata_profile"`
	Root            string    `json:"root"`
	RootBytes       []byte    `json:"root_bytes_base64,omitempty"`
	PathBase        string    `json:"path_base"`
	StartedAt       time.Time `json:"started_at"`
}

type Metadata struct {
	AccessedAt        *time.Time `json:"accessed_at,omitempty"`
	CreatedAt         *time.Time `json:"created_at,omitempty"`
	ChangedAt         *time.Time `json:"changed_at,omitempty"`
	Permissions       string     `json:"permissions,omitempty"`
	UID               *uint32    `json:"uid,omitempty"`
	GID               *uint32    `json:"gid,omitempty"`
	HardLinks         *uint64    `json:"hard_links,omitempty"`
	AllocatedBytes    *int64     `json:"allocated_bytes,omitempty"`
	DeviceID          string     `json:"device_id,omitempty"`
	WindowsAttributes *uint32    `json:"windows_attributes,omitempty"`
}

type Entry struct {
	RecordType      string    `json:"record_type"`
	Path            string    `json:"path"`
	PathBytes       []byte    `json:"path_bytes_base64,omitempty"`
	Type            string    `json:"type"`
	Size            int64     `json:"size"`
	ModifiedAt      time.Time `json:"modified_at"`
	Status          string    `json:"status"`
	LinkTarget      string    `json:"link_target,omitempty"`
	LinkTargetBytes []byte    `json:"link_target_bytes_base64,omitempty"`
	Metadata
}

type ErrorRecord struct {
	RecordType string `json:"record_type"`
	Path       string `json:"path"`
	PathBytes  []byte `json:"path_bytes_base64,omitempty"`
	Operation  string `json:"operation"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

type SkipRecord struct {
	RecordType string `json:"record_type"`
	Path       string `json:"path"`
	PathBytes  []byte `json:"path_bytes_base64,omitempty"`
	Reason     string `json:"reason"`
}

type Summary struct {
	RecordType  string    `json:"record_type"`
	ScanID      string    `json:"scan_id"`
	FinishedAt  time.Time `json:"finished_at"`
	Complete    bool      `json:"complete"`
	Interrupted bool      `json:"interrupted"`
	Entries     uint64    `json:"entries"`
	Files       uint64    `json:"files"`
	Directories uint64    `json:"directories"`
	Symlinks    uint64    `json:"symlinks"`
	Other       uint64    `json:"other"`
	FileBytes   uint64    `json:"file_bytes"`
	Errors      uint64    `json:"errors"`
	Skipped     uint64    `json:"skipped"`
}

func utcTime(sec, nsec int64) *time.Time {
	t := time.Unix(sec, nsec).UTC()
	return &t
}
