package main

import (
	"slices"
	"testing"
)

// readBlob is a mock implementation of readBlobFunc for testing.
var readBlob readBlobFunc = func(hash []byte) ([]byte, error) {
	return hash, nil // just return the hash as content for testing
}

// calculateMergeTest is a test wrapper around calculateMerge that uses readBlob.
func calculateMergeTest(base, ours, theirs map[string][]byte, branchName string) (map[string][]byte, map[string]Conflict, error) {
	return calculateMerge(base, ours, theirs, branchName, readBlob)
}

func TestCalculateMerge(t *testing.T) {
	tests := []struct {
		name               string
		base, ours, theirs map[string][]byte
		expectedIndex      map[string][]byte
		expectedConflicts  []string
	}{
		{
			name: "no changes",
			base: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			ours: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			theirs: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			expectedIndex: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			expectedConflicts: []string{},
		},
		{
			name: "changed in ours only",
			base: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			ours: map[string][]byte{
				"file1.txt": []byte("v2"),
			},
			theirs: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			expectedIndex: map[string][]byte{
				"file1.txt": []byte("v2"),
			},
			expectedConflicts: []string{},
		},
		{
			name: "changed in theirs only",
			base: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			ours: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			theirs: map[string][]byte{
				"file1.txt": []byte("v2"),
			},
			expectedIndex: map[string][]byte{
				"file1.txt": []byte("v2"),
			},
			expectedConflicts: []string{},
		},
		{
			name: "changed in both to different values",
			base: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			ours: map[string][]byte{
				"file1.txt": []byte("v2"),
			},
			theirs: map[string][]byte{
				"file1.txt": []byte("v3"),
			},
			expectedIndex:     map[string][]byte{},
			expectedConflicts: []string{"file1.txt"},
		},
		{
			name: "deleted in both",
			base: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			ours:              map[string][]byte{},
			theirs:            map[string][]byte{},
			expectedIndex:     map[string][]byte{},
			expectedConflicts: []string{},
		},
		{
			name: "deleted in theirs but unchanged in ours",
			base: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			ours: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			theirs:            map[string][]byte{},
			expectedIndex:     map[string][]byte{},
			expectedConflicts: []string{},
		},
		{
			name: "deleted in theirs but changed in ours",
			base: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			ours: map[string][]byte{
				"file1.txt": []byte("v2"),
			},
			theirs:            map[string][]byte{},
			expectedIndex:     map[string][]byte{},
			expectedConflicts: []string{"file1.txt"},
		},
		{
			name: "deleted in current but unchanged in theirs",
			base: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			ours: map[string][]byte{},
			theirs: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			expectedIndex:     map[string][]byte{},
			expectedConflicts: []string{},
		},
		{
			name: "deleted in current but changed in theirs",
			base: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			ours: map[string][]byte{},
			theirs: map[string][]byte{
				"file1.txt": []byte("v2"),
			},
			expectedIndex:     map[string][]byte{},
			expectedConflicts: []string{"file1.txt"},
		},
		{
			name: "added in ours only",
			base: map[string][]byte{},
			ours: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			theirs: map[string][]byte{},
			expectedIndex: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			expectedConflicts: []string{},
		},
		{
			name: "added in theirs only",
			base: map[string][]byte{},
			ours: map[string][]byte{},
			theirs: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			expectedIndex: map[string][]byte{
				"file1.txt": []byte("v1"),
			},
			expectedConflicts: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged, conflicts, err := calculateMergeTest(tt.base, tt.ours, tt.theirs, "branch")
			if err != nil {
				t.Fatalf("calculateMerge() error = %v", err)
			}

			if len(merged) != len(tt.expectedIndex) {
				t.Errorf("calculateMerge() merged length = %v, expected %v", len(merged), len(tt.expectedIndex))
			}

			for k, v := range tt.expectedIndex {
				if mv, ok := merged[k]; !ok || !slices.Equal(mv, v) {
					t.Errorf("calculateMerge() merged[%v] = %v, expected %v", k, mv, v)
				}
			}

			if len(conflicts) != len(tt.expectedConflicts) {
				t.Errorf("calculateMerge() conflicts length = %v, expected %v", len(conflicts), len(tt.expectedConflicts))
			}

			for _, ec := range tt.expectedConflicts {
				found := false
				for path, _ := range conflicts {
					if path == ec {
						found = true
						break
					}
				}

				if !found {
					t.Errorf("calculateMerge() missing expected conflict %v", ec)
				}
			}
		})
	}
}
