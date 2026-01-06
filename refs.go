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

// Conflict represents a merge conflict for a file between two branches.
type Conflict struct {
	BaseHash     []byte
	OurHash      []byte
	TheirHash    []byte
	OurContent   []byte
	TheirContent []byte
	BranchName   string
}

// readBlobFunc is a function type for reading blob content given its hash.
type readBlobFunc func([]byte) ([]byte, error)

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

	obj, err := catFile(treeHash) // treeHash is already binary
	if err != nil {
		return nil, err
	}

	tree, ok := obj.(treeObject)
	if !ok {
		return nil, fmt.Errorf("object %x is not a tree", treeHash)
	}

	for _, entry := range tree.entries {
		entryPath := filepath.Join(dirPath, entry.name)

		switch entry.objType {
		case "blob":
			// restore file
			blobObj, err := catFile(entry.hash) // hash is already binary
			if err != nil {
				return nil, err
			}

			blob, ok := blobObj.(blobObject)
			if !ok {
				return nil, fmt.Errorf("object %x is not a blob", entry.hash)
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
			index[entryPath] = entry.hash // hash is already binary
		case "tree":
			// restore sub-tree (hash is already binary)
			subIndex, err := buildIndexFromTree(entry.hash, entryPath, write)
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
	obj, err := catFile(commitHash) // commitHash is already binary
	if err != nil {
		return err
	}

	commit, ok := obj.(commitObject)
	if !ok {
		return fmt.Errorf("object %x is not a commit", commitHash)
	}

	// retrieve the tree object hash (already binary)
	treeHash := commit.hash

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

	obj, err := catFile(treeHash) // treeHash is already binary
	if err != nil {
		return err
	}

	commit, ok := obj.(commitObject)
	if !ok {
		return fmt.Errorf("object %x is not a commit", treeHash)
	}

	// commit.hash is already binary
	commitTreeHash := commit.hash

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

// traverseCommitHistory traverses the commit history starting from the given commit
// and returns a map of commit hashes to their depth in the history.
func traverseCommitHistory(commit []byte) (map[string]int, error) {
	history := make(map[string]int)

	current := commit
	depth := 0
	for len(current) > 0 {
		hashStr := fmt.Sprintf("%x", current)
		history[hashStr] = depth
		obj, err := catFile(current) // current is already binary
		if err != nil {
			return nil, err
		}

		commitObj, ok := obj.(commitObject)
		if !ok {
			return nil, fmt.Errorf("object %s is not a commit", hashStr)
		}

		if len(commitObj.parents) == 0 {
			break // no parent
		}

		current = commitObj.parents[0]
		depth++
	}

	return history, nil
}

// findCommonAncestor finds the most recent common ancestor between two commits.
func findCommonAncestor(commitA, commitB []byte) ([]byte, error) {
	historyA, err := traverseCommitHistory(commitA)
	if err != nil {
		return nil, err
	}

	historyB, err := traverseCommitHistory(commitB)
	if err != nil {
		return nil, err
	}

	var mostRecentCommonAncestor []byte
	mostRecentDepth := -1

	for hashStr := range historyA {
		if depthB, exists := historyB[hashStr]; exists {
			if mostRecentDepth == -1 || depthB < mostRecentDepth {
				mostRecentDepth = depthB
				ancestorHash, err := hex.DecodeString(hashStr)
				if err != nil {
					return nil, fmt.Errorf("error decoding ancestor hash: %v", err)
				}
				mostRecentCommonAncestor = ancestorHash
			}
		}
	}

	return mostRecentCommonAncestor, nil
}

// readBlobFromCatFile reads a blob object using catFile and returns its content.
// This is used as a pass-in function for calculateMerge for readBlobFunc type.
func readBlobFromCatFile(hash []byte) ([]byte, error) {
	obj, err := catFile(hash)
	if err != nil {
		return nil, err
	}

	blobObj, ok := obj.(blobObject)
	if !ok {
		return nil, fmt.Errorf("object %x is not a blob", hash)
	}

	return blobObj.content, nil
}

// calculateMergeWithReadBlob is a wrapper around calculateMerge that uses readBlobFromCatFile.
func calculateMergeWithReadBlob(base, ours, theirs map[string][]byte, branchName string) (map[string][]byte, map[string]Conflict, error) {
	return calculateMerge(base, ours, theirs, branchName, readBlobFromCatFile)
}

// calculateMerge performs a three-way merge between base, ours, and theirs indexes
func calculateMerge(
	base, ours, theirs map[string][]byte, branchName string, readBlob readBlobFunc,
) (map[string][]byte, map[string]Conflict, error) {
	if readBlob == nil {
		return nil, nil, fmt.Errorf("readBlob function cannot be nil")
	}

	// collect all unique file paths
	uniquePaths := make(map[string]struct{})
	for path := range base {
		uniquePaths[path] = struct{}{}
	}

	for path := range ours {
		uniquePaths[path] = struct{}{}
	}

	for path := range theirs {
		uniquePaths[path] = struct{}{}
	}

	// perform a three-way merge
	mergedIndex := make(map[string][]byte)
	conflicts := make(map[string]Conflict)
	for path := range uniquePaths {
		baseHash, inBase := base[path]
		currentHash, inCurrent := ours[path]
		branchHash, inBranch := theirs[path]

		switch {
		case !inBase && inCurrent && !inBranch:
			// added in current only
			mergedIndex[path] = currentHash

		case !inBase && !inCurrent && inBranch:
			// added in branch only
			mergedIndex[path] = branchHash

		case !inBase && inCurrent && inBranch:
			// added in both so check for conflicts
			if slices.Equal(currentHash, branchHash) {
				mergedIndex[path] = currentHash
			} else {
				ourContentBlob, err := readBlob(currentHash)
				if err != nil {
					return nil, nil, err
				}

				theirContentBlob, err := readBlob(branchHash)
				if err != nil {
					return nil, nil, err
				}

				// add to conflicts map to write markers
				conflicts[path] = Conflict{
					BaseHash:     baseHash,
					OurHash:      currentHash,
					TheirHash:    branchHash,
					OurContent:   ourContentBlob,
					TheirContent: theirContentBlob,
					BranchName:   branchName,
				}
			}

		case inBase && !inCurrent && !inBranch:
			// deleted in both

		case inBase && inCurrent && !inBranch:
			// deleted in branch
			if slices.Equal(baseHash, currentHash) {
				// unchanged in current, so delete
			} else {
				// changed in current and deleted in branch so conflict
				ourContentBlob, err := readBlob(currentHash)
				if err != nil {
					return nil, nil, err
				}

				// add to conflicts map to write markers
				conflicts[path] = Conflict{
					BaseHash:     baseHash,
					OurHash:      currentHash,
					TheirHash:    branchHash,
					OurContent:   ourContentBlob,
					TheirContent: []byte{},
					BranchName:   branchName,
				}
			}

		case inBase && !inCurrent && inBranch:
			// deleted in current
			if slices.Equal(baseHash, branchHash) {
				// unchanged in branch, so delete
			} else {
				// changed in branch and deleted in current so conflict
				theirContentBlob, err := readBlob(branchHash)
				if err != nil {
					return nil, nil, err
				}

				// add to conflicts map to write markers
				conflicts[path] = Conflict{
					BaseHash:     baseHash,
					OurHash:      currentHash,
					TheirHash:    branchHash,
					OurContent:   []byte{},
					TheirContent: theirContentBlob,
					BranchName:   branchName,
				}
			}

		case inBase && inCurrent && inBranch:
			// present in all three
			baseCurrentEq := slices.Equal(baseHash, currentHash)
			baseBranchEq := slices.Equal(baseHash, branchHash)
			currentBranchEq := slices.Equal(currentHash, branchHash)

			switch {
			case baseCurrentEq && baseBranchEq:
				// unchanged in both
				mergedIndex[path] = baseHash

			case baseCurrentEq && !baseBranchEq:
				// changed in branch only
				mergedIndex[path] = branchHash

			case !baseCurrentEq && baseBranchEq:
				// changed in current only
				mergedIndex[path] = currentHash

			case currentBranchEq:
				// changed in both to same value
				mergedIndex[path] = currentHash

			default:
				// changed in both to different values so conflict
				ourContentBlob, err := readBlob(currentHash)
				if err != nil {
					return nil, nil, err
				}

				theirContentBlob, err := readBlob(branchHash)
				if err != nil {
					return nil, nil, err
				}

				// add to conflicts map to write markers
				conflicts[path] = Conflict{
					BaseHash:     baseHash,
					OurHash:      currentHash,
					TheirHash:    branchHash,
					OurContent:   ourContentBlob,
					TheirContent: theirContentBlob,
					BranchName:   branchName,
				}
			}
		}
	}

	return mergedIndex, conflicts, nil
}

// mergeBranch merges the specified branch into the current branch.
func mergeBranch(branchName string) error {
	if err := checkVCSRepo(); err != nil {
		return err
	}

	// find commit hash of branch to merge
	branchRefPath := fmt.Sprintf("refs/heads/%s", branchName)
	branchCommitHash, err := getRef(branchRefPath)
	if err != nil {
		return err
	}

	if branchCommitHash == nil {
		return fmt.Errorf("branch %s has no commits", branchName)
	}

	// find current branch commit hash
	currentBranch, err := getCurrentBranch()
	if err != nil {
		return err
	}

	currentBranchRefPath := fmt.Sprintf("refs/heads/%s", currentBranch)
	currentCommitHash, err := getRef(currentBranchRefPath)
	if err != nil {
		return err
	}

	if currentCommitHash == nil {
		return fmt.Errorf("current branch %s has no commits", currentBranch)
	}

	// find common ancestor
	baseHash, err := findCommonAncestor(currentCommitHash, branchCommitHash)
	if err != nil {
		return err
	}

	// check for fast-forward possibility
	if slices.Equal(baseHash, currentCommitHash) {
		// fast-forward (A is ancestor of B)
		if err := checkoutCommit(branchCommitHash); err != nil {
			return err
		}

		// update current branch (A) to point to B
		if err := updateRef(currentBranchRefPath, branchCommitHash); err != nil {
			return err
		}

		fmt.Printf("Fast-forwarded to branch %s and commit %x\n", branchName, branchCommitHash)

		return nil
	} else if slices.Equal(baseHash, branchCommitHash) {
		// already up to date (B is ancestor of A)
		fmt.Println("Already up to date")
		return nil
	}

	// three-way merge required
	// get trees for base, current, and branch commits
	baseObj, err := catFile(baseHash)
	if err != nil {
		return err
	}
	baseCommit, ok := baseObj.(commitObject)
	if !ok {
		return fmt.Errorf("object %x is not a commit", baseHash)
	}

	currentObj, err := catFile(currentCommitHash)
	if err != nil {
		return err
	}
	currentCommit, ok := currentObj.(commitObject)
	if !ok {
		return fmt.Errorf("object %x is not a commit", currentCommitHash)
	}

	branchObj, err := catFile(branchCommitHash)
	if err != nil {
		return err
	}
	branchCommit, ok := branchObj.(commitObject)
	if !ok {
		return fmt.Errorf("object %x is not a commit", branchCommitHash)
	}

	// build indexes for the three commits
	baseIndex, err := buildIndexFromTree(baseCommit.hash, "", false)
	if err != nil {
		return err
	}

	currentIndex, err := buildIndexFromTree(currentCommit.hash, "", false)
	if err != nil {
		return err
	}

	branchIndex, err := buildIndexFromTree(branchCommit.hash, "", false)
	if err != nil {
		return err
	}

	mergedIndex, conflicts, err := calculateMergeWithReadBlob(baseIndex, currentIndex, branchIndex, branchName)
	if err != nil {
		return err
	}

	// write merged index to working directory
	for path, hash := range mergedIndex {
		obj, err := catFile(hash)
		if err != nil {
			return err
		}

		blob, ok := obj.(blobObject)
		if !ok {
			return fmt.Errorf("object %x is not a blob", hash)
		}

		// create parent directories if needed
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("error creating directory %s: %v", dir, err)
			}
		}

		// write file content
		if err := os.WriteFile(path, blob.content, 0644); err != nil {
			return fmt.Errorf("error writing file %s: %v", path, err)
		}

	}

	// update index file
	if err := writeIndex(mergedIndex); err != nil {
		return err
	}

	// remove obsolete files from working directory
	if err := removeObsoleteFiles(currentIndex, mergedIndex); err != nil {
		return err
	}

	// write conflict markers
	for path, conflict := range conflicts {
		if err := writeConflictMarkers(path, conflict); err != nil {
			return err
		}
	}

	// report if conflicts exist
	if len(conflicts) > 0 {
		// write to MERGE_HEAD to indicate conflict state
		mergeHeadPath := fmt.Sprintf(".%s/MERGE_HEAD", vcsName)
		if err := os.WriteFile(mergeHeadPath, []byte(fmt.Sprintf("%x", branchCommitHash)), 0644); err != nil {
			return fmt.Errorf("error writing MERGE_HEAD: %v", err)
		}

		// write conflicted paths to MERGE_CONFLICTS
		mergeConflictsPath := fmt.Sprintf(".%s/MERGE_CONFLICTS", vcsName)
		var conflictPaths []string
		for path := range conflicts {
			conflictPaths = append(conflictPaths, path)
		}
		if err := os.WriteFile(mergeConflictsPath, []byte(strings.Join(conflictPaths, "\n")), 0644); err != nil {
			return fmt.Errorf("error writing MERGE_CONFLICTS: %v", err)
		}

		fmt.Printf("Automatic merge failed; fix conflicts and then commit.\n")
		for path := range conflicts {
			fmt.Printf("Conflict in file: %s\n", path)
		}

		return nil
	}

	// build the tree object and make a merge commit
	treeHash, err := buildTreeObject(mergedIndex)
	if err != nil {
		return err
	}

	commitHash, err := writeCommitObject(
		treeHash,
		[][]byte{currentCommitHash, branchCommitHash},
		fmt.Sprintf("Merge branch '%s' into %s", branchName, currentBranch),
	)
	if err != nil {
		return err
	}

	// update current branch to point to new merge commit
	if err := updateRef(currentBranchRefPath, commitHash); err != nil {
		return err
	}

	fmt.Printf("Merged %s into %s, commit %x\n", branchName, currentBranch, commitHash)

	return nil
}

// writeConflictMarkers writes conflict markers to the specified file path
func writeConflictMarkers(path string, conflict Conflict) error {
	content := []byte{}
	content = append(content, []byte("<<<<<<< HEAD\n")...)
	content = append(content, conflict.OurContent...)
	content = append(content, []byte("=======\n")...)
	content = append(content, conflict.TheirContent...)
	content = append(content, []byte(fmt.Sprintf(">>>>>>> %s\n", conflict.BranchName))...)

	return os.WriteFile(path, content, 0644)
}

// hasMergeConflicts checks if there are any merge conflicts present
func isMergeInProgress() (bool, error) {
	mergeHeadPath := fmt.Sprintf(".%s/MERGE_HEAD", vcsName)
	_, err := os.Stat(mergeHeadPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("error checking %s: %v", mergeHeadPath, err)
	}

	return true, nil
}

// isConflictsResolved checks if all merge conflicts have been resolved
func isConflictsResolved(index map[string][]byte) (bool, error) {
	mergeConflictsPath := fmt.Sprintf(".%s/MERGE_CONFLICTS", vcsName)
	content, err := os.ReadFile(mergeConflictsPath)
	if err != nil {
		return false, err
	}

	if string(content) == "" {
		return true, nil // no conflicts
	}

	paths := strings.Split(strings.TrimSpace(string(content)), "\n")
	for _, path := range paths {
		hash, ok := index[path]
		if !ok {
			// check if file was deleted
			_, err := os.Stat(path)
			if errors.Is(err, fs.ErrNotExist) {
				continue // file deleted, so resolved
			}

			return false, nil // still in conflict
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}

		contentHash := hashObject(content)

		if !slices.Equal(hash, contentHash) {
			return false, nil // still in conflict
		}
	}

	return true, nil
}
