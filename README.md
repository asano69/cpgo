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
| `-jobs`       | NumCPU    | number of files copied concurrently (directory mode only)             |
| `-retries`    | 2         | extra attempts after a checksum mismatch before giving up on a file  |
| `-verbose`    | false     | print each action taken                                              |

Note: because every already-present file is re-hashed on every run, a large
already-synced tree costs one full read pass through both `<src>` and `<dst>`
even when nothing changed. That's the deliberate tradeoff for a tool whose
purpose is to guarantee correctness rather than to be fast on repeat runs.

## Design notes / tradeoffs

- **Resume granularity is per file, not per byte range.** An interrupted copy
  leaves a `*.cpgo.tmp` file next to the destination; a later run recopies
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
