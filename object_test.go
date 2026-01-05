package main

import (
	"compress/flate"
	"crypto/sha1"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests
func TestCreateDirectoriesFiles(t *testing.T) {
	if err := createDirectoriesFiles(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}
	defer os.RemoveAll(fmt.Sprintf(".%s", vcsName))

	// verify directories
	dirs := []string{fmt.Sprintf(".%s", vcsName), fmt.Sprintf(".%s/objects", vcsName), fmt.Sprintf(".%s/refs", vcsName)}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("error verifying directory %s: %v", dir, err)
		}
	}

	// verify files
	files := []string{
		fmt.Sprintf(".%s/index", vcsName),
	}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil || info.IsDir() {
			t.Fatalf("error verifying file %s: %v", file, err)
		}
	}
}

func TestCreateObject(t *testing.T) {
	if err := createDirectoriesFiles(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}
	defer os.RemoveAll(fmt.Sprintf(".%s", vcsName))

	sampleData := []byte("Test data for object creation")
	dataHash, err := createObject(sampleData)
	if err != nil {
		t.Fatalf("error creating object: %v", err)
	}

	expectedHash := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(sampleData))), sampleData...))
	assert.Equal(t, expectedHash[:], dataHash, "Hashes do not match")

	// verify the file contents
	dirPath := fmt.Sprintf(".%s/objects/%x", vcsName, dataHash[:1])
	info, err := os.Stat(dirPath)
	if err != nil || !info.IsDir() {
		t.Fatalf("error verifying object directory %s: %v", dirPath, err)
	}

	filePath := fmt.Sprintf("%s/%x", dirPath, dataHash[1:])
	_, err = os.Stat(filePath)
	if err != nil {
		t.Fatalf("error verifying object file %s: %v", filePath, err)
	}

	file, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("error opening object file %s: %v", filePath, err)
	}
	defer file.Close()

	buf := make([]byte, 1024)
	_, err = flate.NewReader(file).Read(buf)
	if err != nil {
		if err.Error() != "EOF" {
			t.Fatalf("error reading from object file %s: %v", filePath, err)
		}
	}

	expectedFileContent := append([]byte(fmt.Sprintf("blob %d\x00", len(sampleData))), sampleData...)
	assert.Equal(t, expectedFileContent, buf[:len(expectedFileContent)], "File contents do not match expected object data")
}

func TestBuildTreeObject(t *testing.T) {
	if err := createDirectoriesFiles(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}
	defer os.RemoveAll(fmt.Sprintf(".%s", vcsName))

	// prepare index
	dummyHash := []byte("1234567890abcdef1234")
	index := map[string][]byte{
		"file1.txt":               dummyHash,
		"file2.txt":               dummyHash,
		"subdir/file3.txt":        dummyHash,
		"subdir/file4.txt":        dummyHash,
		"subdir/nested/file5.txt": dummyHash,
	}

	rootHash, err := buildTreeObject(index)
	if err != nil {
		t.Fatalf("error building tree object: %v", err)
	}

	content, err := catFile(rootHash) // rootHash is already binary
	if err != nil {
		t.Fatalf("error catting root tree object: %v", err)
	}

	// type assert to treeObject
	rootTree, ok := content.(treeObject)
	if !ok {
		t.Fatalf("expected treeObject, got %T", content)
	}

	// build a map of entry names to entries for easier verification
	rootEntries := make(map[string]treeEntry)
	for _, entry := range rootTree.entries {
		rootEntries[entry.name] = entry
	}

	// root contains the children
	assert.Contains(t, rootEntries, "file1.txt", "file1.txt missing in tree object")
	assert.Contains(t, rootEntries, "file2.txt", "file2.txt missing in tree object")
	assert.Contains(t, rootEntries, "subdir", "subdir missing in tree object")

	// root doesn't contain nested children
	assert.NotContains(t, rootEntries, "file3.txt", "file3.txt should not be in root tree object")
	assert.NotContains(t, rootEntries, "file4.txt", "file4.txt should not be in root tree object")
	assert.NotContains(t, rootEntries, "file5.txt", "file5.txt should not be in root tree object")

	// verify subdir object
	subdirEntry, exists := rootEntries["subdir"]
	assert.True(t, exists, "subdir entry should exist")
	assert.Equal(t, "tree", subdirEntry.objType, "subdir should be a tree")

	subdirContent, err := catFile(subdirEntry.hash) // hash is already binary
	if err != nil {
		t.Fatalf("error catting subdir tree object: %v", err)
	}

	subdirTree, ok := subdirContent.(treeObject)
	if !ok {
		t.Fatalf("expected treeObject for subdir, got %T", subdirContent)
	}

	subdirEntries := make(map[string]treeEntry)
	for _, entry := range subdirTree.entries {
		subdirEntries[entry.name] = entry
	}

	assert.Contains(t, subdirEntries, "file3.txt", "file3.txt missing in subdir tree object")
	assert.Contains(t, subdirEntries, "file4.txt", "file4.txt missing in subdir tree object")
	assert.Contains(t, subdirEntries, "nested", "nested missing in subdir tree object")

	// verify nested object
	nestedEntry, exists := subdirEntries["nested"]
	assert.True(t, exists, "nested entry should exist")
	assert.Equal(t, "tree", nestedEntry.objType, "nested should be a tree")

	nestedContent, err := catFile(nestedEntry.hash) // hash is already binary
	if err != nil {
		t.Fatalf("error catting nested tree object: %v", err)
	}

	nestedTree, ok := nestedContent.(treeObject)
	if !ok {
		t.Fatalf("expected treeObject for nested, got %T", nestedContent)
	}

	nestedEntries := make(map[string]treeEntry)
	for _, entry := range nestedTree.entries {
		nestedEntries[entry.name] = entry
	}

	assert.Contains(t, nestedEntries, "file5.txt", "file5.txt missing in nested tree object")
}

func TestCatFile(t *testing.T) {
	if err := createDirectoriesFiles(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}
	defer os.RemoveAll(fmt.Sprintf(".%s", vcsName))

	// create the objects and trees
	sampleData1 := []byte("Sample data for cat-file test 1")
	hash1, err := createObject(sampleData1)
	if err != nil {
		t.Fatalf("error creating object 1: %v", err)
	}

	sampleData2 := []byte("Sample data for cat-file test 2")
	hash2, err := createObject(sampleData2)
	if err != nil {
		t.Fatalf("error creating object 2: %v", err)
	}

	sampleData3 := []byte("Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.")
	hash3, err := createObject(sampleData3)
	if err != nil {
		t.Fatalf("error creating object 3: %v", err)
	}

	// create the index and tree
	index := map[string][]byte{
		"catfile1.txt":     hash1,
		"catfile2.txt":     hash2,
		"dir/catfile3.txt": hash3,
	}

	rootHash, err := buildTreeObject(index)
	if err != nil {
		t.Fatalf("error building tree object: %v", err)
	}

	// verify the root tree object using type assertion
	actualRootObject, err := catFile(rootHash) // rootHash is already binary
	if err != nil {
		t.Fatalf("error catting root tree object: %v", err)
	}

	rootTree, ok := actualRootObject.(treeObject)
	if !ok {
		t.Fatalf("expected treeObject, got %T", actualRootObject)
	}

	// build a map of entries for easier verification
	rootEntries := make(map[string]treeEntry)
	for _, entry := range rootTree.entries {
		rootEntries[entry.name] = entry
	}

	// verify catfile1.txt entry
	catfile1Entry, exists := rootEntries["catfile1.txt"]
	assert.True(t, exists, "catfile1.txt should exist in root tree")
	assert.Equal(t, "blob", catfile1Entry.objType, "catfile1.txt should be a blob")
	assert.Equal(t, hash1, catfile1Entry.hash, "catfile1.txt hash mismatch")
	assert.Equal(t, fmt.Sprintf("%06o", entryTypeBlob), catfile1Entry.mode, "catfile1.txt mode mismatch")

	// verify catfile2.txt entry
	catfile2Entry, exists := rootEntries["catfile2.txt"]
	assert.True(t, exists, "catfile2.txt should exist in root tree")
	assert.Equal(t, "blob", catfile2Entry.objType, "catfile2.txt should be a blob")
	assert.Equal(t, hash2, catfile2Entry.hash, "catfile2.txt hash mismatch")
	assert.Equal(t, fmt.Sprintf("%06o", entryTypeBlob), catfile2Entry.mode, "catfile2.txt mode mismatch")

	// verify dir entry exists and is a tree
	dirEntry, exists := rootEntries["dir"]
	assert.True(t, exists, "dir should exist in root tree")
	assert.Equal(t, "tree", dirEntry.objType, "dir should be a tree")

	// verify dir tree object
	actualDirObject, err := catFile(dirEntry.hash) // hash is already binary
	if err != nil {
		t.Fatalf("error catting dir tree object: %v", err)
	}

	dirTree, ok := actualDirObject.(treeObject)
	if !ok {
		t.Fatalf("expected treeObject for dir, got %T", actualDirObject)
	}

	// verify catfile3.txt in dir
	assert.Equal(t, 1, len(dirTree.entries), "dir should have exactly one entry")
	catfile3Entry := dirTree.entries[0]
	assert.Equal(t, "catfile3.txt", catfile3Entry.name, "entry name should be catfile3.txt")
	assert.Equal(t, "blob", catfile3Entry.objType, "catfile3.txt should be a blob")
	assert.Equal(t, hash3, catfile3Entry.hash, "catfile3.txt hash mismatch")
	assert.Equal(t, fmt.Sprintf("%06o", entryTypeBlob), catfile3Entry.mode, "catfile3.txt mode mismatch")
}
