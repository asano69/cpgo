
# cpgo

GoでCLI、ミラーツールcpgoをつくりたい。要件は

* 目的はチェックサム検知で絶対にコピー中にファイルが壊れていることを検出する
* コピーが中断しても途中から再開可能で、冪等性がある。deleteつきの差分コピー
* 全体進捗がわかる。
<<<<<<< HEAD
* 所有権、パーミション、リンクなど、できるだけ多くの属性をクローンする


Checksum-verified mirroring copy tool.

```
cpgo [flags] <src> <dst>
```

Mirrors the contents of `<src>` into `<dst>`:
- copies files that are missing or changed
- verifies every copy by re-reading the destination from disk and comparing
  a SHA-256 hash against the source, retrying on mismatch
- deletes anything in `<dst>` that no longer exists in `<src>` (unless
  `-no-delete` is given)
- preserves permissions, ownership (uid/gid), modification time, symlinks
  and hardlinks
- shows overall progress (bytes and file count) while it runs

## Flags

| Flag          | Default   | Meaning                                                            |
|---------------|-----------|---------------------------------------------------------------------|
| `-no-delete`  | false     | keep extra files in `<dst>` instead of removing them                |
| `-checksum`   | false     | also hash already-present files instead of trusting size+mtime      |
| `-dry-run`    | false     | print what would happen without touching anything                   |
| `-jobs`       | NumCPU    | number of files copied concurrently                                  |
| `-retries`    | 2         | extra attempts after a checksum mismatch before giving up on a file  |
| `-verbose`    | false     | print each action taken                                              |

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
- **Skip decision**: by default, a file is considered already in sync if its
  size and modification time match. This avoids re-hashing an entire tree on
  every run. Pass `-checksum` to also verify existing files' content, which
  catches destination-side corruption or manual tampering at the cost of
  reading every file on both sides.
- **Ownership** (`chown`) is attempted on a best-effort basis: if the process
  lacks permission (not running as root), that specific step is skipped
  without failing the whole file.
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
>>>>>>> 6611837 (init)
