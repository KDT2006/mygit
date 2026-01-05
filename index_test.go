package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadIndex(t *testing.T) {
	if err := createDirectoriesFiles(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(fmt.Sprintf(".%s", vcsName)); err != nil {
			t.Fatalf("Failed to clean up directories: %v", err)
		}
	}()

	validHashes := []string{}
	for range 5 {
		hash, err := generateHexString()
		if err != nil {
			t.Fatalf("Failed to generate hex string: %v", err)
		}
		validHashes = append(validHashes, hash)
	}
	validIndex := []string{
		"file1.txt|" + validHashes[0],
		"dir/file2.txt|" + validHashes[1],
		"dir/subdir/file3.txt|" + validHashes[2],
		"file4.txt|" + validHashes[3],
		"dir2/file5.txt|" + validHashes[4],
	}
	content := strings.Join(validIndex, "\n")
	if err := os.WriteFile(fmt.Sprintf(".%s/index", vcsName), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write index file: %v", err)
	}

	index, err := readIndex()
	assert.NoError(t, err, "Failed to read valid index file")

	for _, entry := range validIndex {
		parts := strings.Split(entry, "|")
		filepath := parts[0]
		expectedHash, err := hex.DecodeString(parts[1])
		assert.NoError(t, err, "Failed to decode expected hash")

		hash, ok := index[filepath]
		assert.True(t, ok, "Missing entry in index for %s", filepath)

		assert.True(t, slices.Equal(hash, expectedHash), "Hash mismatch for %s", filepath)
	}

	invalidIndex := []string{
		"entry1|hash1",
		"invalid_entry",
		"|hash3",
	}

	content = strings.Join(invalidIndex, "\n")
	err = os.WriteFile(fmt.Sprintf(".%s/index", vcsName), []byte(content), 0644)
	assert.NoError(t, err, "Failed to write valid index file")

	index, err = readIndex()
	assert.Error(t, err, "Expected error for invalid index entries")

}

func TestUpdateIndex(t *testing.T) {
	if err := createDirectoriesFiles(); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}
	defer os.RemoveAll(fmt.Sprintf(".%s", vcsName))

	// initialize test cases
	tests := []struct {
		name    string
		content []byte
	}{
		{
			name:    "testfile1.txt",
			content: []byte("Lorem Ipsum is simply dummy text of the printing and typesetting industry. Lorem Ipsum has been the industry's standard dummy text ever since the 1500s, when an unknown printer took a galley of type and scrambled it to make a type specimen book."),
		},
		{
			name:    "testfile2.txt",
			content: []byte("It has survived not only five centuries, but also the leap into electronic typesetting, remaining essentially unchanged. It was popularised in the 1960s with the release of Letraset sheets containing Lorem Ipsum passages, and more recently with desktop publishing software like Aldus PageMaker including versions of Lorem Ipsum."),
		},
		{
			name:    "testfile3.txt",
			content: []byte("It is a long established fact that a reader will be distracted by the readable content of a page when looking at its layout"),
		},
	}

	// build expected state
	expectedState := make(map[string][]byte)

	for _, tc := range tests {
		// create object
		hash, err := createObject(tc.content)
		if err != nil {
			t.Fatalf("error creating object for %s: %v", tc.name, err)
		}

		// update index
		err = updateIndex(tc.name, hash)
		if err != nil {
			t.Fatalf("error updating index for %s: %v", tc.name, err)
		}

		expectedState[tc.name] = hash
	}

	actualState, err := readIndex()
	if err != nil {
		t.Fatalf("error reading index: %v", err)
	}

	// compare expected and actual
	assert.Equal(t, len(expectedState), len(actualState), "Index state does not match expected state")

	for file, expectedHash := range expectedState {
		actualHash, exists := actualState[file]
		if !exists {
			t.Fatalf("file %s missing in index", file)
		}
		assert.Equal(t, expectedHash, actualHash, "Hash for file %s does not match", file)
	}
}

// generateHexString is a helper which generates a dummy 20-byte hex string.
func generateHexString() (string, error) {
	bytes := make([]byte, 20)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(bytes), nil
}
