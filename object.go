package main

import (
	"bytes"
	"compress/flate"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	entryTypeBlob = 0100644 // regular file
	entryTypeTree = 0040000 // directory
)

// Object represents a generic VCS object.
type object interface {
	String() string
}

// blobObject represents a blob object.
type blobObject struct {
	content []byte
}

// String returns the string representation of the blob object.
func (b blobObject) String() string {
	return string(b.content)
}

// treeEntry represents an entry in a tree object.
type treeEntry struct {
	mode    string
	objType string
	hash    []byte // 20-byte binary hash
	name    string
}

// treeObject represents a tree object.
type treeObject struct {
	entries []treeEntry
}

// String returns the string representation of the tree object.
func (t treeObject) String() string {
	var sb strings.Builder
	for _, entry := range t.entries {
		sb.WriteString(fmt.Sprintf("%s %s %x\t%s\n", entry.mode, entry.objType, entry.hash, entry.name))
	}
	return sb.String()
}

// commitObject represents a commit object.
type commitObject struct {
	hash      []byte   // tree hash (20-byte binary)
	parents   [][]byte // parent commit hashes (20-byte binary)
	author    string
	committer string
	message   string
}

// String returns the string representation of the commit object.
func (c commitObject) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("tree %x\n", c.hash))
	if len(c.parents) > 0 {
		for _, parent := range c.parents {
			sb.WriteString(fmt.Sprintf("parent %x\n", parent))
		}
	}
	sb.WriteString(fmt.Sprintf("author %s\n", c.author))
	sb.WriteString(fmt.Sprintf("committer %s\n", c.committer))
	sb.WriteString(fmt.Sprintf("\n%s\n", c.message))
	return sb.String()
}

// createDirectoriesFiles initializes the VCS repository structure.
func createDirectoriesFiles() error {
	// create directories
	dirs := []string{
		fmt.Sprintf(".%s", vcsName),
		fmt.Sprintf(".%s/objects", vcsName),
		fmt.Sprintf(".%s/refs", vcsName),
		fmt.Sprintf(".%s/refs/heads", vcsName),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("error creating directory %s: %v", dir, err)
		}
	}

	// create files
	// HEAD file
	headPath := fmt.Sprintf(".%s/HEAD", vcsName)
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/main"), 0644); err != nil {
		return fmt.Errorf("error creating HEAD file: %v", err)
	}

	// index file
	indexPath := fmt.Sprintf(".%s/index", vcsName)
	f, err := os.Create(indexPath)
	if err != nil {
		return fmt.Errorf("error creating index file: %v", err)
	}
	f.Close()

	// config file
	configPath := fmt.Sprintf(".%s/config", vcsName)
	f, err = os.Create(configPath)
	if err != nil {
		return fmt.Errorf("error creating config file: %v", err)
	}
	f.Close()

	// main branch ref file (empty initially)
	mainRefPath := fmt.Sprintf(".%s/refs/heads/main", vcsName)
	f, err = os.Create(mainRefPath)
	if err != nil {
		return fmt.Errorf("error creating main ref file: %v", err)
	}
	f.Close()

	return nil
}

// checkVCSRepo checks if the current directory is a VCS repository.
func checkVCSRepo() error {
	_, err := os.Stat("." + vcsName)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("error: not a %s repository", vcsName)
		}
		return fmt.Errorf("error accessing %s repository: %v", vcsName, err)
	}
	return nil
}

// createObject creates a blob object from the given data and returns its hash.
func createObject(data []byte) ([]byte, error) {
	if err := checkVCSRepo(); err != nil {
		return nil, err
	}

	// create blob header: "blob <size>\0"
	header := fmt.Sprintf("blob %d\x00", len(data))
	fullData := append([]byte(header), data...)

	// compute SHA-1 hash
	hash := sha1.Sum(fullData)

	// create object directory and file
	dirPath := fmt.Sprintf(".%s/objects/%x", vcsName, hash[:1])
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, fmt.Errorf("error creating object directory: %v", err)
	}

	filePath := fmt.Sprintf("%s/%x", dirPath, hash[1:])

	// compress and write
	f, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("error creating object file: %v", err)
	}
	defer f.Close()

	w, err := flate.NewWriter(f, flate.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("error creating flate writer: %v", err)
	}
	defer w.Close()

	if _, err := w.Write(fullData); err != nil {
		return nil, fmt.Errorf("error writing object data: %v", err)
	}

	return hash[:], nil
}

// hashObject hashes the given data and returns its hash without storing it.
func hashObject(data []byte) []byte {
	// create blob header: "blob <size>\0"
	header := fmt.Sprintf("blob %d\x00", len(data))
	fullData := append([]byte(header), data...)

	// compute SHA-1 hash
	hash := sha1.Sum(fullData)

	return hash[:]
}

// writeTreeObject creates a tree object and returns its hash.
func writeTreeObject(entries []treeEntry) ([]byte, error) {
	if err := checkVCSRepo(); err != nil {
		return nil, err
	}

	// sort entries by name
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	// build tree content in git's binary format
	var buf bytes.Buffer
	for _, entry := range entries {
		// format: "<mode> <name>\0<20-byte hash>"
		buf.WriteString(entry.mode)
		buf.WriteByte(' ')
		buf.WriteString(entry.name)
		buf.WriteByte(0)
		buf.Write(entry.hash) // hash is already binary
	}

	// create tree header
	content := buf.Bytes()
	header := fmt.Sprintf("tree %d\x00", len(content))
	fullData := append([]byte(header), content...)

	// compute hash
	hash := sha1.Sum(fullData)

	// write to object store
	dirPath := fmt.Sprintf(".%s/objects/%x", vcsName, hash[:1])
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, fmt.Errorf("error creating object directory: %v", err)
	}

	filePath := fmt.Sprintf("%s/%x", dirPath, hash[1:])

	f, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("error creating object file: %v", err)
	}
	defer f.Close()

	w, err := flate.NewWriter(f, flate.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("error creating flate writer: %v", err)
	}
	defer w.Close()

	if _, err := w.Write(fullData); err != nil {
		return nil, fmt.Errorf("error writing tree data: %v", err)
	}

	return hash[:], nil
}

// buildTreeObject builds a tree object from the index and returns its hash.
func buildTreeObject(index map[string][]byte) ([]byte, error) {
	if err := checkVCSRepo(); err != nil {
		return nil, err
	}

	return buildTreeRecursive(index, "")
}

// buildTreeRecursive recursively builds tree objects for the given prefix.
func buildTreeRecursive(index map[string][]byte, prefix string) ([]byte, error) {
	var entries []treeEntry
	subdirs := make(map[string]map[string][]byte)

	for path, hash := range index {
		// check if this path belongs under current prefix
		var relativePath string
		if prefix == "" {
			relativePath = path
		} else if strings.HasPrefix(path, prefix+"/") {
			relativePath = strings.TrimPrefix(path, prefix+"/")
		} else {
			continue
		}

		// split into first component and rest
		parts := strings.SplitN(relativePath, "/", 2)

		if len(parts) == 1 {
			// direct child - it's a blob
			entries = append(entries, treeEntry{
				mode:    fmt.Sprintf("%06o", entryTypeBlob),
				objType: "blob",
				hash:    hash, // hash is already binary
				name:    parts[0],
			})
		} else {
			// nested path - collect for subdirectory
			subdir := parts[0]
			if subdirs[subdir] == nil {
				subdirs[subdir] = make(map[string][]byte)
			}
			subdirs[subdir][parts[1]] = hash
		}
	}

	// recursively build subdirectories
	for subdir, subIndex := range subdirs {
		subTreeHash, err := buildTreeRecursive(subIndex, "")
		if err != nil {
			return nil, err
		}

		entries = append(entries, treeEntry{
			mode:    fmt.Sprintf("%06o", entryTypeTree),
			objType: "tree",
			hash:    subTreeHash, // hash is already binary
			name:    subdir,
		})
	}

	return writeTreeObject(entries)
}

// writeCommitObject creates a commit object and returns its hash.
func writeCommitObject(treeHash []byte, parentHashes [][]byte, message string) ([]byte, error) {
	if err := checkVCSRepo(); err != nil {
		return nil, err
	}

	// build commit content
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("tree %x\n", treeHash))

	for _, parentHash := range parentHashes {
		buf.WriteString(fmt.Sprintf("parent %x\n", parentHash))
	}

	// replace with actual author/committer info (use same for both here)
	user, err := getConfig("email")
	if err != nil {
		return nil, err
	}

	author := fmt.Sprintf("Author <%s>", user)
	committer := fmt.Sprintf("Committer <%s>", user)

	buf.WriteString(fmt.Sprintf("author %s\n", author))
	buf.WriteString(fmt.Sprintf("committer %s\n", committer))
	buf.WriteString("\n")
	buf.WriteString(message)
	buf.WriteString("\n")

	content := buf.Bytes()

	// create commit header
	header := fmt.Sprintf("commit %d\x00", len(content))
	fullData := append([]byte(header), content...)

	// compute hash
	hash := sha1.Sum(fullData)

	// write to object store
	dirPath := fmt.Sprintf(".%s/objects/%x", vcsName, hash[:1])
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return nil, fmt.Errorf("error creating object directory: %v", err)
	}

	filePath := fmt.Sprintf("%s/%x", dirPath, hash[1:])

	f, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("error creating object file: %v", err)
	}
	defer f.Close()

	w, err := flate.NewWriter(f, flate.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("error creating flate writer: %v", err)
	}
	defer w.Close()

	if _, err := w.Write(fullData); err != nil {
		return nil, fmt.Errorf("error writing commit data: %v", err)
	}

	return hash[:], nil
}

// catFile reads and parses an object file by its hash.
func catFile(fileHash []byte) (object, error) {
	if err := checkVCSRepo(); err != nil {
		return nil, err
	}

	// convert binary hash to hex string for file path
	hashStr := fmt.Sprintf("%x", fileHash)

	// build file path
	filePath := fmt.Sprintf(".%s/objects/%s/%s", vcsName, hashStr[:2], hashStr[2:])

	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening object file: %v", err)
	}
	defer f.Close()

	// decompress
	r := flate.NewReader(f)
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("error reading object file: %v", err)
	}

	// parse header to determine type
	nullIndex := bytes.IndexByte(data, 0)
	if nullIndex == -1 {
		return nil, fmt.Errorf("error invalid object: missing header terminator")
	}

	header := string(data[:nullIndex])
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("error invalid object header")
	}

	objType := parts[0]

	switch objType {
	case "blob":
		return parseBlobObject(data)
	case "tree":
		return parseTreeObject(data)
	case "commit":
		return parseCommitObject(data)
	default:
		return nil, fmt.Errorf("error unknown object type: %s", objType)
	}
}

// parseBlobObject parses a blob object and returns its content.
func parseBlobObject(data []byte) (blobObject, error) {
	nullIndex := bytes.IndexByte(data, 0)
	if nullIndex == -1 {
		return blobObject{}, fmt.Errorf("error invalid blob object: missing header terminator")
	}

	return blobObject{content: data[nullIndex+1:]}, nil
}

// parseTreeObject parses a tree object and returns its entries.
func parseTreeObject(data []byte) (treeObject, error) {
	// skip the object header
	headerEnd := bytes.IndexByte(data, 0)
	if headerEnd == -1 {
		return treeObject{}, fmt.Errorf("error invalid tree object: missing header terminator")
	}

	var obj treeObject

	i := headerEnd + 1
	for i < len(data) {
		// find space to get the mode
		spaceIndex := bytes.Index(data[i:], []byte(" "))
		if spaceIndex == -1 {
			return treeObject{}, fmt.Errorf("error invalid tree object: missing space after mode")
		}

		// extract mode and convert to octal
		modeString := string(data[i : i+spaceIndex])
		mode, err := strconv.ParseInt(modeString, 8, 0)
		if err != nil {
			return treeObject{}, fmt.Errorf("error parsing mode in tree object: %v", err)
		}
		i = spaceIndex + i + 1

		// find null byte to get the name
		nullIndex := bytes.IndexByte(data[i:], 0)
		if nullIndex == -1 {
			return treeObject{}, fmt.Errorf("error invalid tree object: missing null byte after name")
		}
		name := string(data[i : i+nullIndex])
		i = i + nullIndex + 1

		// extract the 20-byte hash
		if i+20 > len(data) {
			return treeObject{}, fmt.Errorf("error invalid tree object: incomplete hash")
		}
		hash := data[i : i+20]
		i += 20

		// determine the type based on mode
		var objectType string
		switch mode {
		case entryTypeBlob:
			objectType = "blob"
		case entryTypeTree:
			objectType = "tree"
		default:
			return treeObject{}, fmt.Errorf("error unknown entry type in tree object: %o", mode)
		}

		// append the entry to the tree object
		entry := treeEntry{
			mode:    fmt.Sprintf("%06o", mode),
			objType: objectType,
			hash:    hash, // store as binary (already 20 bytes)
			name:    name,
		}
		obj.entries = append(obj.entries, entry)
	}

	return obj, nil
}

// parseCommitObject parses a commit object and returns its content.
func parseCommitObject(data []byte) (commitObject, error) {
	// skip the object header
	headerEnd := bytes.IndexByte(data, 0)
	if headerEnd == -1 {
		return commitObject{}, fmt.Errorf("error invalid commit object: missing header terminator")
	}

	object := commitObject{}

	target := string(data[headerEnd+1:])
	lines := strings.Split(target, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "tree ") {
			treeHex := strings.TrimPrefix(line, "tree ")
			treeHash, err := hex.DecodeString(treeHex)
			if err != nil {
				return commitObject{}, fmt.Errorf("error decoding tree hash in commit object: %v", err)
			}
			object.hash = treeHash
			continue
		}

		if strings.HasPrefix(line, "parent ") {
			parentHex := strings.TrimPrefix(line, "parent ")
			parentHash, err := hex.DecodeString(parentHex)
			if err != nil {
				return commitObject{}, fmt.Errorf("error decoding parent hash in commit object: %v", err)
			}
			object.parents = append(object.parents, parentHash)
			continue
		}

		if strings.HasPrefix(line, "author ") {
			object.author = strings.TrimPrefix(line, "author ")
			continue
		}

		if strings.HasPrefix(line, "committer") {
			object.committer = strings.TrimPrefix(line, "committer ")
			continue
		}
	}

	// parse commit message
	messageIndex := strings.Index(target, "\n\n")
	if messageIndex != -1 {
		object.message = strings.TrimSpace(target[messageIndex+2:])
	}

	return object, nil
}

// printCommitHistory prints the commit history starting from the given commit hash.
func printCommitHistory(commitHash []byte) error {
	if len(commitHash) == 0 {
		return nil // base case: no more commits
	}

	// read the commit object (commitHash is already binary)
	obj, err := catFile(commitHash)
	if err != nil {
		return fmt.Errorf("error reading commit object %x: %v", commitHash, err)
	}

	commitObj, ok := obj.(commitObject)
	if !ok {
		return fmt.Errorf("error object %x is not a commit object", commitHash)
	}

	// print commit details
	fmt.Printf("commit %x\n", commitHash)
	fmt.Printf("Author: %s\n", commitObj.author)
	fmt.Printf("Committer: %s\n\n", commitObj.committer)
	fmt.Printf("    %s\n\n", commitObj.message)

	// recursive call to print parent commit
	if len(commitObj.parents) == 0 {
		return nil
	}

	return printCommitHistory(commitObj.parents[0])
}

// getConfig retrieves the value for the given key from the config file.
func getConfig(key string) (string, error) {
	if err := checkVCSRepo(); err != nil {
		return "", err
	}

	configPath := fmt.Sprintf(".%s/config", vcsName)
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("error reading config file: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		parts := strings.Split(line, "=")
		if len(parts) != 2 {
			continue
		}

		if strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1]), nil
		}
	}

	return "", fmt.Errorf("key %s not found in config", key)
}

// updateConfig updates the config file with the new key-value pair.
func updateConfig(key, value string) error {
	if err := checkVCSRepo(); err != nil {
		return err
	}

	configPath := fmt.Sprintf(".%s/config", vcsName)
	content, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("error reading config file: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	updated := false
	for i, line := range lines {
		parts := strings.Split(line, "=")
		if len(parts) != 2 {
			continue
		}

		if strings.TrimSpace(parts[0]) == key {
			lines[i] = fmt.Sprintf("%s=%s", key, value)
			updated = true
			break
		}
	}

	if !updated {
		lines = append(lines, fmt.Sprintf("%s=%s", key, value))
	}

	newContent := strings.Join(lines, "\n")
	err = os.WriteFile(configPath, []byte(newContent), 0644)
	if err != nil {
		return fmt.Errorf("error writing config file: %v", err)
	}

	return nil
}
