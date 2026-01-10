package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

const (
	vcsName = "mygit" // Name of the version control system
)

func main() {
	// check for valid command
	if len(os.Args) < 2 {
		fmt.Println("expected a valid command")
		os.Exit(1)
	}

	// handle commands
	switch os.Args[1] {
	case "init":
		handleInit()
	case "hash-object":
		handleHashObject()
	case "add":
		handleAdd()
	case "write-tree":
		handleWriteTree()
	case "cat-file":
		handleCatFile()
	case "commit":
		handleCommit()
	case "log":
		handleLog()
	case "branch":
		handleBranch()
	case "checkout":
		handleCheckout()
	case "rm":
		handleRemove()
	case "merge":
		handleMerge()
	case "status":
		handleStatus()
	case "reset":
		handleReset()
	case "config":
		handleConfig()
	default:
		fmt.Printf("unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// handleInit initializes the VCS repository.
func handleInit() {
	// Initialize VCS
	err := createDirectoriesFiles()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Initialized empty %s repository in .%s/\n", vcsName, vcsName)
}

// handleHashObject handles the hash-object command.
func handleHashObject() {
	// define a flag set for hash-object
	cmd := flag.NewFlagSet("hash-object", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) < 1 {
		fmt.Println("usage: " + vcsName + " hash-object <file>")
		os.Exit(1)
	}
	filePath := args[0]

	content, err := os.ReadFile(filePath)
	if err != nil {
		log.Fatalf("error reading file %s: %v", filePath, err)
	}

	dataHash, err := createObject(content)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%x\n", dataHash)
}

// handleAdd handles the add command.
func handleAdd() {
	// define a flag set for add
	cmd := flag.NewFlagSet("add", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) != 1 {
		fmt.Println("usage: " + vcsName + " add <file>")
		os.Exit(1)
	}

	targetPath := args[0]

	stat, err := os.Stat(targetPath)
	if err != nil {
		log.Fatal(err)
	}
	if stat.IsDir() {
		// handle all files within directory
		err := addDirectory(targetPath)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		content, err := os.ReadFile(targetPath)
		if err != nil {
			log.Fatalf("error reading file %s: %v", targetPath, err)
		}

		// create object and store it
		dataHash, err := createObject(content)
		if err != nil {
			log.Fatal(err)
		}

		// update the index file
		if err = updateIndex(targetPath, dataHash); err != nil {
			log.Fatal(err)
		}
	}
}

// handleWriteTree handles the write-tree command.
func handleWriteTree() {
	// define a flag set for write-tree
	cmd := flag.NewFlagSet("write-tree", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	// read the index file
	index, err := readIndex()
	if err != nil {
		log.Fatal(err)
	}

	// build the tree structure and write to disk
	treeHash, err := buildTreeObject(index)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%x\n", treeHash)
}

// handleCatFile handles the cat-file command.
func handleCatFile() {
	// define a flag set for cat-file
	cmd := flag.NewFlagSet("cat-file", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) < 1 {
		fmt.Println("usage: " + vcsName + " cat-file <hash>")
		os.Exit(1)
	}

	// decode hex string from CLI to binary hash
	hashBytes, err := hex.DecodeString(args[len(args)-1])
	if err != nil {
		log.Fatalf("invalid hash: %v", err)
	}

	content, err := catFile(hashBytes)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s\n", content)
}

// handleCommit handles the commit command.
func handleCommit() {
	// define a flag set for commit
	cmd := flag.NewFlagSet("commit", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) != 1 {
		fmt.Println("usage: " + vcsName + " commit <message>")
		os.Exit(1)
	}

	message := args[0]

	// read the index file
	index, err := readIndex()
	if err != nil {
		log.Fatal(err)
	}

	// check if merge conflicts exist
	hasConflicts, err := isMergeInProgress()
	if err != nil {
		log.Fatal(err)
	}

	if hasConflicts {
		conflictsResolved, err := isConflictsResolved(index)
		if err != nil {
			log.Fatal(err)
		}

		if !conflictsResolved {
			log.Fatal("cannot commit: merge conflicts exist, please resolve them first")
		}
	}

	// build the tree structure and write to disk
	treeHash, err := buildTreeObject(index)
	if err != nil {
		log.Fatal(err)
	}

	// get parent commit hash from HEAD
	head, err := getHEAD()
	if err != nil {
		log.Fatal(err)
	}

	refHash, err := getRef(head)
	if err != nil {
		log.Fatal(err)
	}

	commitParents := [][]byte{refHash}

	// create commit object
	if hasConflicts {
		mergeHead, err := os.ReadFile(fmt.Sprintf(".%s/MERGE_HEAD", vcsName))
		if err != nil {
			log.Fatal(err)
		}

		mergeHeadBinary, err := hex.DecodeString(strings.TrimSpace(string(mergeHead)))
		if err != nil {
			log.Fatal(err)
		}

		commitParents = append(commitParents, mergeHeadBinary)

		fmt.Println("All conflicts resolved. Creating merge commit.")
	}

	commitHash, err := writeCommitObject(treeHash, commitParents, message)
	if err != nil {
		log.Fatal(err)
	}

	// update HEAD to point to new commit
	err = updateRef(head, commitHash)
	if err != nil {
		log.Fatal(err)
	}

	if hasConflicts {
		// delete merge state files
		files := []string{
			fmt.Sprintf(".%s/MERGE_HEAD", vcsName),
			fmt.Sprintf(".%s/MERGE_CONFLICTS", vcsName),
		}

		for _, file := range files {
			if err := os.Remove(file); err != nil {
				log.Fatal(err)
			}
		}
	}

	fmt.Printf("%x\n", commitHash)
}

func handleLog() {
	// define a flag set for log
	cmd := flag.NewFlagSet("log", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	// read the HEAD to get current branch
	head, err := getHEAD()
	if err != nil {
		log.Fatal(err)
	}

	// get the latest commit from HEAD
	refHash, err := getRef(head)
	if err != nil {
		log.Fatal(err)
	}

	// traverse and print commit history
	if err = printCommitHistory(refHash); err != nil {
		log.Fatal(err)
	}
}

func handleBranch() {
	// define a flag set for branch
	cmd := flag.NewFlagSet("branch", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) > 1 {
		fmt.Println("usage: " + vcsName + " branch [<branch-name>]")
		os.Exit(1)
	}

	switch len(args) {
	case 0:
		// list branches
		branches, err := getBranches()
		if err != nil {
			log.Fatal(err)
		}

		currentBranch, err := getCurrentBranch()
		if err != nil {
			log.Fatal(err)
		}

		for _, branch := range branches {
			if branch == currentBranch {
				fmt.Printf("* %s\n", branch)
			} else {
				fmt.Printf("%s\n", branch)
			}
		}
	case 1:
		// create new branch at current HEAD
		head, err := getHEAD()
		if err != nil {
			log.Fatal(err)
		}

		commitHash, err := getRef(head)
		if err != nil {
			log.Fatal(err)
		}

		if commitHash == nil {
			log.Fatal("cannot create branch: no commits yet")
		}

		if err := createBranch(args[0], commitHash); err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Created new branch %s\n", args[0])

	default:
		fmt.Println("usage: " + vcsName + " branch [<branch-name>]")
		os.Exit(1)
	}
}

func handleCheckout() {
	// define a flag set for checkout
	cmd := flag.NewFlagSet("checkout", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) != 1 {
		fmt.Println("usage: " + vcsName + " checkout <branch-name>")
		os.Exit(1)
	}

	branchName := args[0]

	// check for uncommitted changes
	if err := checkUncommittedChanges(); err != nil {
		log.Fatal("please commit your changes before switching branches")
	}

	// check for unstaged changes
	if err := checkUnstagedChanges(); err != nil {
		log.Fatal("please stage your changes before switching branches")
	}

	// check if branch is current branch
	currentBranch, err := getCurrentBranch()
	if err != nil {
		log.Fatal(err)
	}
	if branchName == currentBranch {
		fmt.Printf("Already on branch %s\n", branchName)
		return
	}

	// get commit hash for target branch
	refPath := fmt.Sprintf("refs/heads/%s", branchName)
	commitHash, err := getRef(refPath)
	if err != nil {
		log.Fatal(err)
	}

	if commitHash == nil {
		log.Fatalf("branch %s has no commits", branchName)
	}

	// restore working directory to that commit
	if err := checkoutCommit(commitHash); err != nil {
		log.Fatal(err)
	}

	// update HEAD to point to the new branch
	if err := checkoutBranch(branchName); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Switched to branch %s\n", branchName)
}

func handleRemove() {
	// define a flag set for rm
	cmd := flag.NewFlagSet("rm", flag.ExitOnError)
	cached := cmd.Bool("cached", false, "remove from index only, not from working directory")

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) != 1 {
		fmt.Println("usage: " + vcsName + " rm [--cached] <file>")
		os.Exit(1)
	}

	targetPath := args[0]

	// remove file from working directory if not --cached
	if !*cached {
		if err := os.Remove(targetPath); err != nil {
			log.Fatalf("error removing file %s: %v", targetPath, err)
		}
	}

	// remove file from index
	index, err := readIndex()
	if err != nil {
		log.Fatal(err)
	}

	if _, ok := index[targetPath]; !ok {
		log.Fatalf("file %s is not in the index", targetPath)
	}

	delete(index, targetPath)

	err = writeIndex(index)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Removed %s\n", targetPath)
}

func handleMerge() {
	// define a flag set for merge
	cmd := flag.NewFlagSet("merge", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) != 1 {
		fmt.Println("usage: " + vcsName + " merge <branch-name>")
		os.Exit(1)
	}

	branchName := args[0]

	// check for uncommitted changes
	if err := checkUncommittedChanges(); err != nil {
		log.Fatal("please commit your changes before merging branches")
	}

	// check for unstaged changes
	if err := checkUnstagedChanges(); err != nil {
		log.Fatal("please stage your changes before merging branches")
	}

	// check for existing merge in progress
	if yes, err := isMergeInProgress(); err != nil {
		log.Fatal(err)
	} else if yes {
		log.Fatal("merge in progress; please resolve conflicts and commit before merging again")
	}

	// merge the specified branch into the current branch
	if err := mergeBranch(branchName); err != nil {
		log.Fatal(err)
	}
}

func handleStatus() {
	// define a flag set for status
	cmd := flag.NewFlagSet("status", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	modifiedFiles, unstagedFiles, err := getStatus()
	if err != nil {
		log.Fatal(err)
	}

	printStatus(modifiedFiles, unstagedFiles)
}

func handleReset() {
	// define a flag set for reset
	cmd := flag.NewFlagSet("reset", flag.ExitOnError)

	soft := cmd.Bool("soft", false, "move HEAD only (keep index and working tree)")
	mixed := cmd.Bool("mixed", false, "move HEAD and reset index (keep working tree) (default)")
	hard := cmd.Bool("hard", false, "move HEAD, reset index and working tree")

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) != 1 {
		fmt.Println("usage: " + vcsName + " reset [--soft|--mixed|--hard] <commit-hash>")
		os.Exit(1)
	}

	// ensure only one is set
	modeCount := 0
	if *soft {
		modeCount++
	}
	if *mixed {
		modeCount++
	}
	if *hard {
		modeCount++
	}
	if modeCount > 1 {
		fmt.Println("please specify only one of --soft, --mixed, or --hard")
		os.Exit(1)
	}

	mode := resetModeMixed // default
	if *soft {
		mode = resetModeSoft
	} else if *hard {
		mode = resetModeHard
	}

	// decode hex string to binary
	commitHash, err := hex.DecodeString(args[0])
	if err != nil {
		log.Fatalf("invalid commit hash: %v", err)
	}

	if err := resetToCommit(commitHash, mode); err != nil {
		log.Fatal(err)
	}
}

func handleConfig() {
	// define a flag set for config
	cmd := flag.NewFlagSet("config", flag.ExitOnError)

	cmd.Parse(os.Args[2:])

	args := cmd.Args()
	if len(args) != 1 && len(args) != 2 {
		fmt.Println("usage: " + vcsName + " config <user.[name|email]> [<value>]")
		os.Exit(1)
	}

	parts := strings.SplitN(args[0], ".", 2)
	key := parts[1]
	if len(args) == 1 {
		value, err := getConfig(key)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Println(value)
		return
	}

	if err := updateConfig(key, args[1]); err != nil {
		log.Fatal(err)
	}
}
