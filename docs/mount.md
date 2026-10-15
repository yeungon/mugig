# Playing music from the cloud with rclone

Mugig plays files on disk. It has no streaming or cloud code, and it
doesn't need any: [rclone](https://rclone.org) can mount Google Drive, a
NAS, S3, SFTP and about 70 other services as an ordinary folder, and Mugig
treats that folder like any other. These notes say what works, what goes
wrong, and how to set it up so it doesn't.

Tested on macOS (Apple silicon), rclone v1.70.3, Mugig at commit 0c438e1,
October 2026. The tests mounted a local folder through rclone's NFS server,
which exercises the same mount, cache and macOS NFS client as a cloud
remote, but not real network speed. Linux and Windows setups below are
from rclone's documentation and Mugig's code, not tried.

---

## 1. Quick start (macOS)

macFUSE isn't needed. `rclone nfsmount` runs a small NFS server on
localhost and mounts it with macOS's built-in NFS client, without a kernel
extension or `sudo`.

```sh
mkdir -p ~/Cloud/Drive
rclone nfsmount google_drive:  ~/Cloud/Drive \
  --read-only \
  --vfs-cache-mode full --vfs-cache-max-size 20G \
  -o soft -o timeo=10 -o retrans=2
```

Then in Mugig, add **`~/Cloud/Drive/Music`**, the folder *inside* the
mount, not `~/Cloud/Drive` itself. Section 4 explains why.

To stop: `umount ~/Cloud/Drive`. If rclone has crashed and that hangs,
`umount -f ~/Cloud/Drive`.

## 2. `rclone nfsmount` or `rclone mount`?

Use **`nfsmount` on macOS**, **`mount` on Linux and Windows**. Both give
Mugig the same kind of folder and take the same options
(`--vfs-cache-mode`, `--read-only`, `--dir-cache-time`, `--rc`); they
differ in what must be installed and in how they fail.

| | `rclone nfsmount` | `rclone mount` |
|---|---|---|
| How it works | rclone runs an NFS server on localhost; the system's own NFS client mounts it | rclone serves the files through FUSE |
| macOS needs | Nothing | macFUSE (a kernel extension; on Apple silicon, security must be lowered in Recovery mode to allow it) or FUSE-T (no kernel extension, NFS underneath) |
| Linux needs | Root to mount (`--sudo`) | The `fuse3` package |
| Windows | Not available | WinFsp; mounts on a drive letter |
| When rclone crashes | Hard mount (the default): programs touching the folder hang until `umount -f`. Soft mount: reads fail in about 2 s | Programs get an error at once ("Device not configured", "Transport endpoint is not connected") |
| Caches | rclone's, then macOS's NFS cache (5–60 s) | rclone's only (`--attr-timeout`, default 1 s) |
| Writes to a `--read-only` mount | Refused with an error ("Permission denied"; deleting: "Input/output error") | Refused with macFUSE; fail *silently* with FUSE-T |
| Stopping | `umount <dir>`, or SIGTERM to rclone if started with `--daemon` | `umount <dir>`, or Ctrl-C in the foreground |

Speed doesn't decide it: a song needs well under 1 MB/s, far less than
either gives.

On macOS, installing macFUSE only for quicker errors when rclone crashes
isn't worth lowering the system's security; a soft `nfsmount` comes close.
FUSE-T avoids the kernel extension, but it is NFS underneath, with
`nfsmount`'s caching, and rclone's documentation notes that Finder
looking at files can change their dates on it.

The `nfsmount` crash and read-only rows were tried on this Mac (section
3); the rest is from `rclone nfsmount --help` and `rclone mount --help`.

## 3. What was tested, and what happened

| Test | Result |
|---|---|
| `rclone nfsmount` without macFUSE | Mounts as the user; files read back byte for byte. |
| Mugig scan, tags, decoding to the end, full quality check (FLAC and MP3) through the mount | Same results as from the local disk. |
| A file added on the remote side while Mugig watches the folder | **No file event**, after 20 s. Mugig's live watching doesn't see remote changes. |
| Folder listing after a file is added or deleted on the remote | **Stale**: still the old list when looked at again (rclone keeps folder lists for up to 5 min by default). |
| Reading a file deleted on the remote but still listed | Fails with "Error reading". Mugig would show the song but not play it. |
| `rclone rc vfs/refresh recursive=true` after a remote change | The new file shows in a listing within about 5 s (macOS's NFS cache). |
| rclone killed (`kill -9`) with the default *hard* mount | A `stat` on the folder **hung for over 40 s** and only returned, with "Input/output error", after `umount -f`. |
| The same with `-o soft -o timeo=10 -o retrans=2` | Reading a song fails in about 2 s with "Operation timed out". `stat` still answers from cache. |
| Creating and deleting a file on a `--read-only` mount | Refused: "Permission denied" and "Input/output error"; the source untouched. |

## 4. Edge cases

### The mount isn't running: Mugig forgets the folder's songs

From Mugig's code (`scanLibrary` and `collect` in `internal/ui`): a folder
that exists but is empty is taken at its word. When rclone isn't running,
the mount point `~/Cloud/Drive` is still there, as an empty folder. If that
folder is in the library, the next scan finds nothing, counts the scan as
complete, takes every song in it off the list and drops them from the
library cache. When the mount comes back, everything is read again from
the cloud, which is slow.

**Avoid it by adding a folder inside the mount** (`~/Cloud/Drive/Music`).
Without the mount, that folder doesn't exist, so Mugig marks it as
missing, keeps its songs for later, and looks for it every 3 seconds.
When the mount is back, it scans again, and the cache makes that quick:
unchanged files are matched by size and date, not read.

On Windows the problem doesn't arise: rclone mounts on a drive letter
(`M:`), which disappears when unmounted, and Mugig shows the folder as
"Not connected".

### rclone crashes or the network drops

With NFS's default *hard* mount, any program touching the folder waits
until the server returns, which for a crashed rclone is never. Mugig would
freeze where it reads the folder: playback, the scan, and the folder check
that runs every 3 seconds. Mount **soft** (`-o soft -o timeo=10 -o
retrans=2`) so reads fail after a few seconds instead. Mugig then shows
the usual "can't play" message for the song. A soft mount can also turn a
very slow network into read errors, but the mount is read-only, so nothing
on the remote can be damaged.

If the mount is already stuck: `umount -f <mount point>`, then start
rclone again.

### Changes made elsewhere don't show up

Mugig follows folders with FSEvents (inotify on Linux), which only hears
about changes made on this computer. Songs added from the phone or another
computer reach Mugig only by a rescan (**⌘R**, or Rescan on the folder).
Even then, rclone may still hand out its cached folder list:

- **Google Drive** and other remotes that report their changes: rclone
  checks every minute (`--poll-interval`, default `1m`), so a rescan a
  minute or two after the change finds it.
- **SFTP, a NAS (SMB), a local disk, S3**, which don't report changes:
  rclone keeps folder lists for `--dir-cache-time` (default `5m`). Either
  wait, lower it (`--dir-cache-time 30s`, at the cost of more requests), or
  refresh by hand before rescanning:

  ```sh
  rclone rc vfs/refresh recursive=true   # needs --rc on the mount command
  ```

### A song deleted on the remote still shows

Until rclone's folder list expires, a deleted file is still listed, and
Mugig keeps it. Playing it fails. A rescan after the list refreshes takes
it off.

### Reading whole songs costs time and data

These read every byte of a song, which on a cloud remote means downloading
it:

- **Quality check** (Check Audio Quality, Check All Songs). A FLAC album
  is 300–500 MB.
- **Volume Levelling**, when on, measures each song that starts playing
  and hasn't been measured, at most two at once. Playing it was going to
  download it anyway, but skipping through songs downloads each one you
  land on.
- **The first scan** reads the start of every file for its tags: one
  request per file, plus one per folder. A library of 10,000 songs on Google
  Drive can take a long while the first time; later scans use the cache.

Results are saved, so each song is checked and measured once.

### Seeking and gapless playback

Mugig decodes 3 seconds ahead. Over a slow link, seeking to a part not yet
downloaded, or starting the next song, can stutter or pause. With
`--vfs-cache-mode full`, rclone keeps what has been downloaded on disk and
fetches ranges for seeks; without it, seeking backwards may download the
file again, and some reads aren't possible at all. Songs played once are
then local until the cache fills (`--vfs-cache-max-size`).

### Paths change with the mount point

Mugig remembers songs, playlists, quality results and loudness by their
full path. Mount at the same place every time. Mounting somewhere else
makes them all look like new songs; playlists point at the old paths.
Exported M3U playlists use paths relative to the playlist's folder when
the songs are under it, so a playlist saved inside the mount moves with
it.

### Google Drive specifics

- rclone's built-in Google client ID is shared by all rclone users and
  rate-limited. For big libraries, create your own in `rclone config`
  (rclone's docs: "Making your own client_id").
- Google Docs and shortcuts appear as files; Mugig ignores anything that
  isn't audio.
- Two files can have the same name in one Drive folder. rclone shows the
  second with a suffix; run `rclone dedupe` if songs look doubled.

### Other systems

- **Linux:** `rclone mount remote: ~/Cloud/Drive` uses FUSE (`fuse3`
  package), and FUSE mounts fail with "Transport endpoint is not
  connected" rather than hanging when rclone dies. The empty mount point
  problem is the same: add a folder inside the mount.
- **Windows:** needs [WinFsp](https://winfsp.dev). `rclone mount remote:
  M: --vfs-cache-mode full` mounts on a drive letter, which avoids the
  empty-folder problem.

## 5. Best practice

1. **Add a folder inside the mount** to Mugig, never the mount point.
2. **Mount read-only** (`--read-only`). Mugig only writes to music
   folders for Move to Trash. On a writable mount that would delete the
   song from the cloud; read-only, it just fails.
3. **Mount soft** on macOS (`-o soft -o timeo=10 -o retrans=2`).
4. **Use `--vfs-cache-mode full`** with a size limit that fits your disk.
   Keep the cache on the internal disk (`--cache-dir`), not on a USB disk.
5. **Mount at the same path every time**, before opening Mugig.
6. **Leave Volume Levelling off** for cloud folders on a metered or slow
   connection, and check quality a folder at a time rather than with
   Check All Songs.
7. **After changes on another computer**, wait a minute (Drive) or run
   `vfs/refresh`, then press ⌘R.
8. **Start the mount at login** so it is there before Mugig, for example
   with a LaunchAgent (macOS) or a systemd user service (Linux) that runs
   the mount command with `--daemon` left off, so the service manager
   restarts it if it dies.

A LaunchAgent for macOS, saved as
`~/Library/LaunchAgents/org.rclone.music.plist` and started with
`launchctl load ~/Library/LaunchAgents/org.rclone.music.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>org.rclone.music</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/rclone</string>
    <string>nfsmount</string>
    <string>google_drive:</string>
    <string>/Users/YOU/Cloud/Drive</string>
    <string>--read-only</string>
    <string>--vfs-cache-mode</string><string>full</string>
    <string>--vfs-cache-max-size</string><string>20G</string>
    <string>-o</string><string>soft</string>
    <string>-o</string><string>timeo=10</string>
    <string>-o</string><string>retrans=2</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict>
</plist>
```

Replace `YOU` and the remote name. The mount point folder must exist.

## 6. Changes Mugig could make

None of these are needed for the setup above to work. In order of value:

1. **Tell a missing mount from an empty folder.** Remember each folder's
   device ID (`Stat_t.Dev`) when it is scanned. If a later scan finds it
   empty *and* on the same device as its parent folder when it used to be
   on another, the mount is gone: treat the folder as not connected and
   keep its songs, as for an unplugged USB disk. This removes the
   biggest trap in section 4.
2. **Never read a music folder on the UI thread.** Some `os.Stat` calls
   run there (Show in Finder, opening chosen or dropped files); on a
   stuck hard mount they would freeze the whole window, not just
   playback. Run them on a goroutine with a timeout.
3. **Say "Not connected" for mounts**, not "Not found". `missingText` only
   knows `/Volumes`, `/media` and `/mnt`; it could also check whether the
   nearest existing folder is a mount point that is now empty.
4. **A "Network folder" setting per folder** that turns off automatic
   loudness measuring and asks before a quality check of many songs.
5. **Buffer more on network folders**: decode further ahead than 3 s when
   the file is on a network or FUSE file system (`statfs` type `nfs`,
   `smbfs`, `fuse`, `macfuse`).
6. **Built-in rclone** (`librclone`, or calling `rclone cat`/`rclone serve`)
   to browse remotes without a mount. Large and adds a dependency of about
   50 MB; the mount gives the same result today.
