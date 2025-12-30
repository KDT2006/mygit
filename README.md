# mygit - A Minimal Git Clone for Learning

mygit is a small, educational re-implementation of core Git concepts written in Go. It was built to explore how Git works under the hood: object storage, trees, commits, references, and basic branch/checkout flows. This project is not intended for production use, but as a hands-on learning tool.

## Highlights

- Simple object store using SHA‑1 and flate compression in `.mygit/objects/`
- Blob/tree/commit objects with minimal, readable formats
- Lightweight index that maps file paths to blob hashes
- Basic refs in `.mygit/refs/heads/` and `HEAD`
- Core commands: `init`, `hash-object`, `add`, `rm`, `write-tree`, `cat-file`, `commit`, `log`, `branch`, `checkout`

## Quick Start

Requirements: Go installed (any recent version should work).

```bash
# Build (Windows will produce mygit.exe)
go build -o mygit

# Or run without building
go run . init
```

Initialize a repository and make your first commit:

```bash
# Initialize a new repository
./mygit init

# Add a file or a directory (recursively)
echo "Hello mygit" > hello.txt
./mygit add hello.txt

# Remove a file from the index and working directory
./mygit rm hello.txt

# Remove from index only (keep file on disk, becomes untracked)
./mygit rm --cached hello.txt

# Commit the tree
./mygit commit "Initial commit"

# View history
./mygit log
```

Branches and checkout:

```bash
# Create a new branch at current HEAD
./mygit branch feature-x

# Switch branches and restore the working tree
./mygit checkout feature-x
```

Inspect objects:

```bash
# Hash a file and store as a blob
./mygit hash-object hello.txt

# Print an object by hash (blob/tree/commit)
./mygit cat-file <object-hash>
```

On Windows, replace `./mygit` with `mygit.exe`. You can also use `go run . <command>`.

## How It Works (In Brief)

- Objects
	- Blob: raw file content; header `blob <size>\0` + bytes, stored compressed.
	- Tree: lists entries with mode, name, and the 20-byte object ID they point to.
	- Commit: references a tree and an optional parent, plus author/committer/message.
- Hashing & Storage
	- SHA‑1 of the header+content determines the object ID.
	- Stored under `.mygit/objects/aa/bb…` (first byte as directory, remainder as file).
- Index
	- A simple line-based file mapping `path|<hex object id>`.
	- `add` updates the index; `write-tree` builds the tree object graph from it.
- Refs & HEAD
	- Branches live in `.mygit/refs/heads/<name>` and store the commit ID.
	- `HEAD` contains `ref: refs/heads/<name>` (no detached HEAD handling yet).

## Commands

```text
init                      Initialize a new repository
hash-object <file>        Create a blob object for a file and print its hash
add <path>                Stage a file or directory recursively into the index
rm [--cached] <path>      Remove a file from index and disk (--cached: index only)
write-tree                Build a tree object from the index and print its hash
cat-file <hash>           Pretty-print an object (blob/tree/commit)
commit <message>          Create a commit from the current tree (and parent)
log                       Print commit history from current HEAD
branch [<name>]           List branches or create a new one at HEAD
checkout <branch>         Switch to a branch and restore the working tree
```

## Design Goals & Limitations

- Educational clarity over completeness and performance
- No networking/remotes, merges, staging metadata, or conflict resolution
- Detached `HEAD` is not supported

## Project Structure

- `main.go` — CLI entry and command routing
- `object.go` — object formats, hashing, read/write utilities
- `index.go` — index read/write and directory staging
- `refs.go` — refs, branch/checkout, and working tree restore

## Testing

```bash
go test ./...
```

## Inspiration

- Git source and documentation

If you find issues or have suggestions to make the learning experience clearer, feel free to open an issue or PR.

