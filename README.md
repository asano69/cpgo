# cpgo

Checksum-verified mirroring copy tool.


## Installation

```sh
$ go install github.com/asano69/cpgo@latest
```

To uninstall:

```sh
$ rm "$(which cpgo)" # Remove an executable
```

## Basic Usage

```
cpgo [flags] <src>...<dst>
```

Behaves like `cp -dR --preserve=all`, always. If `<src>` is a directory, it
is copied recursively — into `<dst>/<basename of src>` if `<dst>` already
exists as a directory, or to `<dst>` itself otherwise, same as `cp -R`. If
`<src>` is a single file, it's copied into `<dst>` if that's an existing
directory (keeping the original filename), or to the exact path `<dst>`
otherwise, matching `cp`: the destination directory must already exist.
Both modes share the same checksum-verified, resumable copy logic.

More than one `<src>` can be given, exactly like `cp a b c dst`: every
source is then copied into `<dst>`, which must already exist as a
directory (an error, not something cpgo creates for you, if it doesn't). A
failure on one source doesn't stop the others from being attempted, but the
process exits non-zero if any of them failed.

Directory mode:
- copies files that are missing or changed
- **checksum verification is always on, with no way to disable it** — this
  is a safety tool, not a speed tool. Every file, whether newly copied or
  already present at the destination, is confirmed by hashing (SHA-256) both
  the source and the destination and comparing, retrying on mismatch.
  Metadata (size/mtime) is only ever used as a cheap pre-filter to skip
  hashing a file whose size obviously differs; it is never treated as proof
  that a file is correct on its own.
- preserves permissions, ownership (uid/gid), modification time, symlinks
  and hardlinks
- shows overall progress (bytes and file count) while it runs
- never deletes anything at `<dst>` — extra files there are left alone, just
  like `cp -R` never touches files that aren't in `<src>`

## Flags

| Flag          | Default   | Meaning                                                            |
|---------------|-----------|---------------------------------------------------------------------|
| `-dry-run`    | false     | print what would happen without touching anything                   |
| `-in-place`   | false     | write directly to the destination instead of a temp file + rename    |
| `-jobs`       | NumCPU    | number of files copied concurrently (directory mode only)             |
| `-retries`    | 2         | extra attempts after a checksum mismatch before giving up on a file  |
| `-verbose`    | false     | print each action taken                                              |

Note: because every already-present file is re-hashed on every run, a large
already-synced tree costs one full read pass through both `<src>` and `<dst>`
even when nothing changed. That's the deliberate tradeoff for a tool whose
purpose is to guarantee correctness rather than to be fast on repeat runs.

## Examples

**Directory mode.** Behaves like `cp -dR --preserve=all <src> <dst>`: a
trailing slash on `<src>` makes no difference, but whether `<dst>` already
exists as a directory changes where things land, just like `cp -R`.

```
# dst doesn't exist yet: created as a copy of src itself
# (dst/one.txt, dst/two.txt, ...)
cpgo /data/photos /backup/photos

# dst already exists as a directory: src is copied *into* it
# (/backup/photos/photos/one.txt, ...), matching `cp -R`
cpgo /data/photos /backup/photos

# trailing slash on src is a no-op, like cp and unlike rsync
cpgo /data/photos/ /backup/photos

# preview what would happen without touching anything
cpgo -dry-run /data/photos /backup/photos

# show every file copied or linked
cpgo -verbose /data/photos /backup/photos

# limit concurrency (e.g. for a slow disk) and allow more retries
# on checksum mismatch before giving up on a file
cpgo -jobs 2 -retries 5 /data/photos /backup/photos

# write directly to the destination instead of temp file + rename
# (uses less spare disk space, at the cost of leaving a half-written
# file behind if a copy is interrupted -- see Design notes below)
cpgo -in-place /data/photos /backup/photos
```

**Single-file mode.** `<src>` is a regular file, so only that file is
copied; this behaves like `cp`, not `rclone`.

```sh
# dst is an existing file: overwritten (after checksum verification)

cpgo photo.jpg /backup/photos/photo.jpg

# multiple sources: dst must already exist as a directory, like cp

cpgo photo1.jpg photo2.jpg /data/more-photos /backup/photos

# dst is not an existing directory: copied to that exact path
# (/backup/renamed must already exist, just like `cp`)
cpgo photo.jpg /backup/renamed/photo-2026.jpg

# dst is an existing file: overwritten (after checksum verification)
cpgo photo.jpg /backup/photos/photo.jpg
```

## Design notes / tradeoffs

- **Resume granularity is per file, not per byte range.** An interrupted copy
  leaves a `*.NNNNNNNNN.partial` file next to the destination (named after
  rclone's own local `.partial` convention); a later run recopies
  that file from scratch rather than resuming mid-file. This keeps the
  integrity guarantee simple (every finished file has been fully re-verified)
  at the cost of re-transferring a large file that was interrupted near the
  end. Files that were already fully copied and verified are skipped.
- **Idempotency**: destination files are only ever replaced via a temp file +
  atomic rename, and only after their checksum has been confirmed by reading
  them back from disk. So re-running `cpgo` after any kind of interruption
  converges on the same correct result.
- **Destination directories are never auto-created past one level.** Like
  `cp`, cpgo requires the directory a destination will land in to already
  exist; in directory mode it will create the single `dst` directory itself
  (mirroring `cp -R`), but it never invents missing parent directories for
  you, in either mode.
- **A trailing slash on `<dst>` forces it to be treated as a directory** in
  single-file mode, matching `cp`: `cpgo file dst/` fails if `dst` doesn't
  already exist, rather than creating a regular file named `dst`.
- **Ownership** (`chown`) is attempted on a best-effort basis: if the process
  lacks permission (not running as root), that specific step is skipped
  without failing the whole file.
- **`-in-place`** writes directly to the destination file instead of a temp
  file + rename, so updating a file never needs headroom for two copies of
  it at once. This is a real safety trade-off, not a free optimization: the
  destination is truncated the moment the copy starts, so a copy that fails
  or is interrupted partway leaves it overwritten with bad or partial data,
  instead of the default mode's guarantee that a failed copy never disturbs
  what was already there.
- **Confirmed corruption stops the run immediately.** If a checksum mismatch
  survives every retry, that's treated as evidence of a real problem (bad
  storage, bad RAM, etc.), not just a bad file, so cpgo aborts the whole sync
  right away instead of pressing on — remaining files, hardlink recreation,
  attribute fixup and deletion are all skipped. Errors are printed via
  logrus; confirmed corruption is logged at error level (red), other
  per-file failures at warning level (yellow).
- **Not implemented**: extended attributes (xattr) and ACLs, and special
  files (device nodes, FIFOs, sockets). These were left out to keep the
  implementation dependency-free and simple; the standard library doesn't
  expose xattr syscalls cleanly, and pulling in `golang.org/x/sys` for one
  feature seemed like the wrong tradeoff for this tool's size. If you need
  them, `setAttrs` in `sync.go` is the place to extend.

## Building

With Go directly:

```
go build -o cpgo .
```

With Nix (flake included, targets `nixos-26.05`):

```
nix build
./result/bin/cpgo --help
```
