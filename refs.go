package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// getHEAD reads the HEAD file to get the current branch reference.
func getHEAD() (string, error) {
	if err := checkVCSRepo(); err != nil {
		return "", err
	}

	headPath := fmt.Sprintf(".%s/HEAD", vcsName)
	content, err := os.ReadFile(headPath)
	if err != nil {
		return "", fmt.Errorf("error reading HEAD file: %v", err)
	}

	if after, ok := strings.CutPrefix(string(content), "ref: "); ok {
		return strings.TrimSpace(after), nil
	} else {
		// detached HEAD state (not handled...)
		return "", fmt.Errorf("error detached HEAD state not supported")
	}
}

// getRef reads the given ref file and returns the hash it points to.
func getRef(refPath string) ([]byte, error) {
	if err := checkVCSRepo(); err != nil {
		return nil, err
	}

	fullRefPath := fmt.Sprintf(".%s/%s", vcsName, refPath)
	content, err := os.ReadFile(fullRefPath)
	if err != nil {
		return nil, fmt.Errorf("error reading ref file %s: %v", refPath, err)
	}

	if len(content) == 0 {
		return nil, nil // initial commit case
	}

	hash, err := hex.DecodeString(strings.TrimSpace(string(content)))
	if err != nil {
		return nil, fmt.Errorf("error decoding ref hash from %s: %v", refPath, err)
	}

	return hash, nil
}

// updateRef updates the given ref file with the new hash.
func updateRef(refPath string, hash []byte) error {
	if err := checkVCSRepo(); err != nil {
		return err
	}

	fullRefPath := fmt.Sprintf(".%s/%s", vcsName, refPath)
	hexHash := fmt.Sprintf("%x", hash)
	err := os.WriteFile(fullRefPath, []byte(hexHash), 0644)
	if err != nil {
		return fmt.Errorf("error writing ref file %s: %v", refPath, err)
	}

	return nil
}

// getBranches returns a list of all branch names.
func getBranches() ([]string, error) {
	if err := checkVCSRepo(); err != nil {
		return nil, err
	}

	branchesDir := fmt.Sprintf(".%s/refs/heads", vcsName)
	entries, err := os.ReadDir(branchesDir)
	if err != nil {
		return nil, fmt.Errorf("error reading heads directory: %v", err)
	}

	var branches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			branches = append(branches, entry.Name())
		}
	}

	return branches, nil
}

// getCurrentBranch returns the name of the current branch.
func getCurrentBranch() (string, error) {
	head, err := getHEAD()
	if err != nil {
		return "", err
	}

	return filepath.Base(head), nil
}

// createBranch creates a new branch with the given name at the specified commit hash.
func createBranch(branchName string, commitHash []byte) error {
	if err := checkVCSRepo(); err != nil {
		return err
	}

	branchRefPath := fmt.Sprintf("refs/heads/%s", branchName)
	return updateRef(branchRefPath, commitHash)
}

// checkoutBranch switches the current branch to branchName
// and updates the working directory to match the branch's latest commit.
func checkoutBranch(branchName string) error {
	if err := checkVCSRepo(); err != nil {
		return err
	}

	// verify if branch exists
	branchRefPath := fmt.Sprintf(".%s/refs/heads/%s", vcsName, branchName)
	if _, err := os.Stat(branchRefPath); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("branch %s does not exist", branchName)
	}

	// update HEAD
	headPath := fmt.Sprintf(".%s/HEAD", vcsName)
	newRef := fmt.Sprintf("ref: refs/heads/%s", branchName)
	if err := os.WriteFile(headPath, []byte(newRef), 0644); err != nil {
		return fmt.Errorf("error updating HEAD: %v", err)
	}

	return nil
}

// buildIndexFromTree builds an index map from the given tree hash
// and writes files to the working directory if write is true.
func buildIndexFromTree(treeHash []byte, dirPath string, write bool) (map[string][]byte, error) {
	index := make(map[string][]byte)

	hexHash := fmt.Sprintf("%x", treeHash)
	obj, err := catFile([]byte(hexHash))
	if err != nil {
		return nil, err
	}

	tree, ok := obj.(treeObject)
	if !ok {
		return nil, fmt.Errorf("object %s is not a tree", hexHash)
	}

	for _, entry := range tree.entries {
		entryPath := filepath.Join(dirPath, entry.name)

		switch entry.objType {
		case "blob":
			// restore file
			blobObj, err := catFile([]byte(entry.hash))
			if err != nil {
				return nil, err
			}

			blob, ok := blobObj.(blobObject)
			if !ok {
				return nil, fmt.Errorf("object %s is not a blob", entry.hash)
			}

			// write to disk if needed
			if write {
				// create parent directories if needed
				if dir := filepath.Dir(entryPath); dir != "." {
					if err := os.MkdirAll(dir, 0755); err != nil {
						return nil, fmt.Errorf("error creating directory %s: %v", dir, err)
					}
				}

				// write file content
				if err := os.WriteFile(entryPath, blob.content, 0644); err != nil {
					return nil, fmt.Errorf("error writing file %s: %v", entryPath, err)
				}
			}

			// add to index
			hashBytes, err := hex.DecodeString(entry.hash)
			if err != nil {
				return nil, fmt.Errorf("error decoding blob hash %s: %v", entry.hash, err)
			}

			index[entryPath] = hashBytes
		case "tree":
			// restore sub-tree
			subTreeHash, err := hex.DecodeString(entry.hash)
			if err != nil {
				return nil, fmt.Errorf("error decoding tree hash %s: %v", entry.hash, err)
			}

			subIndex, err := buildIndexFromTree(subTreeHash, entryPath, write)
			if err != nil {
				return nil, err
			}

			// merge sub-index into main index
			for k, v := range subIndex {
				index[k] = v
			}
		}
	}

	return index, nil
}

// removeObsoleteFiles removes files from the working directory that are present in the
// old index but not in the new index.
func removeObsoleteFiles(oldIndex, newIndex map[string][]byte) error {
	for filepath := range oldIndex {
		if _, exists := newIndex[filepath]; !exists {
			if err := os.Remove(filepath); err != nil {
				return fmt.Errorf("error removing obsolete file %s: %v", filepath, err)
			}
		}
	}

	return nil
}

// checkoutCommit checks out the working directory to match the state
// of the given commit hash.
func checkoutCommit(commitHash []byte) error {
	hexHash := fmt.Sprintf("%x", commitHash)
	obj, err := catFile([]byte(hexHash))
	if err != nil {
		return err
	}

	commit, ok := obj.(commitObject)
	if !ok {
		return fmt.Errorf("object %s is not a commit", hexHash)
	}

	// retrieve the tree object hash
	treeHash, err := hex.DecodeString(string(commit.hash))
	if err != nil {
		return fmt.Errorf("error decoding tree hash: %v", err)
	}

	// read the old index
	oldIndex, err := readIndex()
	if err != nil {
		return fmt.Errorf("error reading old index: %v", err)
	}

	// restore the working dir from tree
	index, err := buildIndexFromTree(treeHash, "", true)
	if err != nil {
		return fmt.Errorf("error restoring tree: %v", err)
	}

	// update the index file
	err = writeIndex(index)
	if err != nil {
		return fmt.Errorf("error updating index: %v", err)
	}

	// remove files not in the new index
	if err := removeObsoleteFiles(oldIndex, index); err != nil {
		return fmt.Errorf("error removing non-indexed files: %v", err)
	}

	return nil
}

// checkUncommittedChanges checks if there are any uncommitted changes in the working directory
func checkUncommittedChanges() error {
	index, err := readIndex()
	if err != nil {
		return err
	}

	head, err := getHEAD()
	if err != nil {
		return err
	}

	treeHash, err := getRef(head)
	if err != nil {
		return err
	}

	hexHash := fmt.Sprintf("%x", treeHash)
	obj, err := catFile([]byte(hexHash))
	if err != nil {
		return err
	}

	commit, ok := obj.(commitObject)
	if !ok {
		return fmt.Errorf("object %s is not a commit", hexHash)
	}

	commitTreeHash, err := hex.DecodeString(string(commit.hash))
	if err != nil {
		return fmt.Errorf("error decoding tree hash: %v", err)
	}

	// build index from commit tree without writing files
	commitIndex, err := buildIndexFromTree(commitTreeHash, "", false)
	if err != nil {
		return fmt.Errorf("error building index from commit tree: %v", err)
	}

	// check for staged changes
	for path, storedHash := range index {
		commitHash, exists := commitIndex[path]
		if !exists || !slices.Equal(storedHash, commitHash) {
			return fmt.Errorf("file %s has uncommitted changes", path)
		}
	}

	// check for staged deletions
	for path := range commitIndex {
		if _, exists := index[path]; !exists {
			return fmt.Errorf("file %s has uncommitted deletions", path)
		}
	}

	return nil
}

// checkUnstagedChanges checks if there's any unstaged changes in the working directory
func checkUnstagedChanges() error {
	index, err := readIndex()
	if err != nil {
		return err
	}

	for targetPath, storedHash := range index {
		content, err := os.ReadFile(targetPath)
		if err != nil {
			return fmt.Errorf("error reading file %s: %v", targetPath, err)
		}

		contentHash := hashObject(content)
		if !slices.Equal(storedHash, contentHash) {
			return fmt.Errorf("file %s has been modified", targetPath)
		}
	}

	return nil
}
