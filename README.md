# FileList

A metadata-only file inventory CLI for Windows, macOS, and Linux. It writes
versioned JSON Lines (JSONL) for import into a separate inventory system. It does
not read regular file contents or calculate fingerprints, manage inventories,
or compare scans.

## Build

Go 1.26 or newer is required to build. Users of the binaries do not need Go.

```sh
go build -o dist/filelist ./cmd/filelist
go test ./...
```

Build all six Windows/macOS/Linux, x64/ARM64 binaries on one machine, without C
compilers or platform SDKs:

```sh
sh scripts/build.sh
```

On Windows, use `./scripts/build.ps1` from PowerShell instead. Both scripts disable
CGO. The GitHub Actions workflow runs tests and `go vet` independently on Linux,
Windows, and macOS. After all three pass, it builds all six targets on a single
Linux runner and uploads the binaries. Native tests use each runner's CPU
architecture; the other architectures are verified by cross-compilation only.
macOS binaries are unsigned and not notarized.

## Scan

Windows (PowerShell):

```powershell
.\dist\filelist-windows-amd64.exe --root 'E:\' --source-id archive-01 --output 'C:\Lists\archive-01.jsonl'
```

Linux:

```sh
./dist/filelist-linux-amd64 --root /media/user/Archive --source-id archive-01 --output archive-01.jsonl
```

macOS:

```sh
./dist/filelist-darwin-arm64 --root /Volumes/Archive --source-id archive-01 --output archive-01.jsonl
```

The output defaults to stdout (`--output -`). Diagnostics go to stderr. Output
files are created exclusively: existing files are never overwritten. Output files
and their hard links inside the scan scope are included like other files. Their
size and timestamps describe the moment they are visited, not the final output
after writing finishes. Prefer `--output`
over shell redirection, which can truncate an existing file before FileList runs.

`--source-id` is required and must remain stable for the same logical data source.
It is supplied by your configuration or inventory system, not generated from the
drive letter, volume label, or current mount path. The scanner does not write an
identity marker onto the source disk.

### Paths and scan scope

By default, paths are relative to the containing volume/mount root:

| Host path | Inventory path |
| --- | --- |
| `E:\Photos\2025\trip.jpg` | `Photos/2025/trip.jpg` |
| `G:\Photos\2025\trip.jpg` on a later mount | `Photos/2025/trip.jpg` |
| `/Volumes/Archive/Photos/2025/trip.jpg` | `Photos/2025/trip.jpg` |
| `/media/user/Archive/Photos/2025/trip.jpg` | `Photos/2025/trip.jpg` |

Paths use `/`, preserve case, and have no leading slash or drive letter. `.` is
the base directory itself. Scanning only a subdirectory retains the same volume
base, so paths remain comparable to a whole-volume scan.

For a logical collection rather than a whole volume, set a fixed `--base`:

```sh
go run ./cmd/filelist --root /data/collection/photos --base /data/collection --source-id collection-01 --output photos.jsonl
```

This emits `photos/...`. The root must be inside the base. Keep the same base
convention for every scan of a source. Linux autodetection uses
`/proc/self/mountinfo`, macOS uses `statfs`, and Windows uses `GetVolumePathNameW`.
If automatic detection is unavailable, specify `--base` explicitly.
On macOS, firmlink paths such as `/Users` and `/private/var` are resolved to
their physical Data volume paths before computing relative inventory paths.
Scanning through either the firmlink or its physical path produces the same
inventory paths.

The root directory is included. Directory links are not followed; broken links
are recorded. Windows directory reparse points are not traversed. Unix directories
on a different device are recorded but not traversed. The scanner reports these
intentional exclusions with `skip` records. It does not implement include/exclude
patterns or a point-in-time filesystem snapshot.

### JSONL protocol, version 2

Version 2 removes the per-entry `observed_at` and filesystem `file_id` fields.
Scan start and finish times remain in the header and summary.

Each line is one UTF-8 JSON object. A stream contains:

1. A `scan` header with `schema_version`, unique `scan_id`, persistent `source_id`,
   `platform`, `metadata_profile: "basic-v2"`, relative `root`, `path_base`
   (`volume_root` or `explicit`), and `started_at`.
2. `entry` records, interleaved with `error` and `skip` records as needed.
3. A `summary` with the matching `scan_id`, completion state, finish time, counts,
   and sum of regular-file logical sizes.

Source and scan IDs are inherited from the header by subsequent records. Directory
order is unspecified; entries are processed in batches rather than sorted and
loaded into memory all at once. Timestamps use UTC RFC 3339 with fractional seconds
when available. No physical path base is stored in the header.

Every entry includes `path`, `type`, `size`, `modified_at`, and
`status` (`ok` or `partial`). Types are `file`, `directory`, `symlink`, `fifo`,
`socket`, `device`, or `other`. `size` is the filesystem-reported logical size;
directory and special-file sizes are platform-specific. Symlinks also carry
`link_target` in its original OS syntax; it can be absolute and is not an inventory
path. A symlink target may therefore contain a drive letter or mount point.

Additional metadata is emitted when supported:

| Field | Availability and meaning |
| --- | --- |
| `accessed_at` | Last access time; filesystem precision/update policy varies |
| `created_at` | Windows/macOS creation time; omitted on Linux in this version |
| `changed_at` | Unix inode change time; Windows change time when available |
| `hard_links` | Filesystem-reported link count |
| `permissions` | Unix octal mode including setuid/setgid/sticky bits |
| `uid`, `gid` | Unix numeric owner/group IDs |
| `allocated_bytes` | Unix `st_blocks * 512`; not unique physical disk usage |
| `device_id` | Unix device number; local to the current mount environment |
| `windows_attributes` | Raw Windows file attribute bitmask |

Missing fields mean unavailable or not collected; they must not erase a previously
collected value on import. There are no content hashes in `basic-v2`. Consumers
should tolerate unknown fields and check the schema/profile before interpreting
records. For Unix names that are not valid UTF-8, `path` is a display string and
`path_bytes_base64` holds the authoritative relative path bytes. The same convention
applies to `root_bytes_base64` and `link_target_bytes_base64`.

An `error` contains the relative `path`, `operation`, `code` (`permission_denied`,
`not_found`, or `io_error`), and diagnostic `message`. A metadata/readlink failure
also marks its entry `partial`. A directory enumeration failure can occur after
the directory entry was emitted. A `skip` contains the relative `path` and `reason`
(`different_filesystem` or `reparse_point`). For directory skips,
the reason applies to its descendants.

`summary.complete` is true only if there were no collection errors or interruption;
intentional skips are counted separately. `file_bytes` counts hard-linked files
once per path and is not a measure of disk space used. A missing summary means
the stream is unfinished even if preceding lines are valid. Ctrl+C attempts to
write an incomplete summary. Output failures may prevent a summary from being
written or flushed.

### Identity and later merges

Use `source_id + path` to associate the same location across scans. Filesystem
file IDs are not collected. Moves and renames cannot be reliably matched using
this metadata alone. The Unix device number is informational and may change
after remounting.

Metadata alone cannot guarantee that contents stayed unchanged. Size and
timestamps are hints, not content verification. A scan is not atomic; files
can change while it runs.

When importing, retain scan time/provenance. A later scan may have a narrower
`root` or intentional exclusions. Do not infer deletion outside that scope, under
skipped directories, or from a failed/incomplete scan.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Scan completed without collection errors, or help was shown |
| `1` | Invalid arguments, setup failure, or output failure |
| `2` | Scan finished with collection errors; inspect error records |
| `130` | Interrupted with Ctrl+C; incomplete summary written if possible |
