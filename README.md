# cpgo
GoでCLI、ミラーツールcpgoをつくりたい。要件は

* 目的はチェックサム検知で絶対にコピー中にファイルが壊れていることを検出する
* コピーが中断しても途中から再開可能で、冪等性がある。deleteつきの差分コピー
* 全体進捗がわかる。
* 所有権、パーミション、リンクなど、できるだけ多くの属性をクローンする

Checksum-verified mirroring copy tool.

```
cpgo [flags] <src> <dst>
```

If `<src>` is a directory, mirrors its contents into `<dst>`. If `<src>` is a
single file, copies just that file — into `<dst>` if it's an existing
directory (keeping the original filename), or to the exact path `<dst>`
otherwise, creating parent directories as needed. Both modes share the same
checksum-verified, resumable copy logic.

Directory mode:
- copies files that are missing or changed
- **checksum verification is always on, with no way to disable it** — this
  is a safety tool, not a speed tool. Every file, whether newly copied or
  already present at the destination, is confirmed by hashing (SHA-256) both
  the source and the destination and comparing, retrying on mismatch.
  Metadata (size/mtime) is only ever used as a cheap pre-filter to skip
  hashing a file whose size obviously differs; it is never treated as proof
  that a file is correct on its own.
- deletes anything in `<dst>` that no longer exists in `<src>` (unless
  `-no-delete` is given)
- preserves permissions, ownership (uid/gid), modification time, symlinks
  and hardlinks
- shows overall progress (bytes and file count) while it runs

## Flags

| Flag          | Default   | Meaning                                                            |
|---------------|-----------|---------------------------------------------------------------------|
| `-no-delete`  | false     | keep extra files in `<dst>` instead of removing them (directory mode only) |
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

**Directory mode.** `<src>`'s contents are mirrored into `<dst>`, the same
way `rclone sync <src> <dst>` works: a trailing slash on `<src>` makes no
difference, and the result is the same whether `<dst>` already exists or
not.

```
# dst doesn't exist yet: created, then filled with src's contents
cpgo /data/photos /backup/photos

# dst already exists: its contents are made to match src exactly,
# including deleting anything under dst that isn't in src
cpgo /data/photos /backup/photos

# trailing slash on src is a no-op, unlike rsync
cpgo /data/photos/ /backup/photos

# preview what would happen without touching anything
cpgo -dry-run /data/photos /backup/photos

# keep files under dst that aren't in src, instead of deleting them
cpgo -no-delete /data/photos /backup/photos

# show every file copied, linked or deleted
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

```
# dst is an existing directory: copied in as photos/vacation.jpg
cpgo photo.jpg /backup/photos

# dst is not an existing directory: copied to that exact path,
# creating /backup/renamed if needed
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
