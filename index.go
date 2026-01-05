package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// readIndex reads and parses the index file into a map.
func readIndex() (map[string][]byte, error) {
	if err := checkVCSRepo(); err != nil {
		return nil, err
	}

	// index map represents the parsed index file
	index := make(map[string][]byte)

	f, err := os.Open(fmt.Sprintf(".%s/index", vcsName))
	if err != nil {
		if os.IsNotExist(err) {
			return index, nil
		}
		return nil, fmt.Errorf("error opening index file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "|")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid index entry: %s", scanner.Text())
		}

		filepath := parts[0]
		if filepath == "" {
			return nil, fmt.Errorf("empty filepath in index entry: %s", scanner.Text())
		}

		// decode hex string to byte slice
		hash, err := hex.DecodeString(parts[1])
		if err != nil {
			return nil, err
		}

		index[filepath] = hash
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning index file: %v", err)
	}

	return index, nil
}

// updateIndex updates the index file with the new object entry.
func updateIndex(filepath string, dataHash []byte) error {
	if err := checkVCSRepo(); err != nil {
		return err
	}

	// read current index
	index, err := readIndex()
	if err != nil {
		return err
	}

	// update current index
	index[filepath] = dataHash

	// write back the entire index
	return writeIndex(index)
}

// writeIndex writes the entire index map back to the index file.
func writeIndex(index map[string][]byte) error {
	if err := checkVCSRepo(); err != nil {
		return err
	}

	f, err := os.Create(fmt.Sprintf(".%s/index", vcsName))
	if err != nil {
		return fmt.Errorf("error creating index file: %v", err)
	}
	defer f.Close()

	for filepath, hash := range index {
		_, err := fmt.Fprintf(f, "%s|%x\n", filepath, hash)
		if err != nil {
			return fmt.Errorf("error writing to index file: %v", err)
		}
	}

	return nil
}

// addDirectory adds all the files within the given directory to the staging area.
func addDirectory(dirPath string) error {
	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() && d.Name() == "."+vcsName {
			return filepath.SkipDir // skip VCS dir
		}

		if !d.IsDir() {
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("error reading file %s: %v", path, err)
			}

			// create object and store it
			dataHash, err := createObject(content)
			if err != nil {
				return fmt.Errorf("error creating object for file %s: %v", path, err)
			}

			// update the index file
			if err = updateIndex(path, dataHash); err != nil {
				return fmt.Errorf("error updating index for file %s: %v", path, err)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error adding directory %s: %v", dirPath, err)
	}

	return nil
}
