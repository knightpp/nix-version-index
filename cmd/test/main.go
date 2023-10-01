package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/knightpp/nix-version-index/internal/attrset"
)

type Commit struct {
	Rev  string
	Hash string
}

func (c Commit) String() string {
	return fmt.Sprintf("%s-%s", c.Rev, c.Hash)
}

var (
	rev   string
	hash  string
	query string
)

func init() {
	flag.StringVar(&rev, "rev", "", "git revision")
	flag.StringVar(&hash, "hash", "", "unpacked tarball hash")
	flag.StringVar(&query, "query", "", "nix attrset path")
}

func main() {
	flag.Parse()

	if rev != "" || hash != "" {
		err := evaluate(Commit{Rev: rev, Hash: hash})
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	if query == "" {
		log.Print("no command specified")
		return
	}

	err := executeQuery(query)
	if err != nil {
		log.Fatal(err)
	}
}

func executeQuery(query string) error {
	dirEntries, err := os.ReadDir("packages")
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}

	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join("packages", entry.Name()))
		if err != nil {
			log.Fatal(err)
		}

		var set attrset.Set
		err = json.Unmarshal(data, &set)
		if err != nil {
			log.Fatal(err)
		}

		jsonBytes, _ := json.MarshalIndent(set[query], "", "  ")
		fmt.Printf(">>> %s <<<\n%s\n", entry.Name(), jsonBytes)
		continue
	}
	return nil
}

func evaluate(commit Commit) error {
	path := "packages/" + commit.String() + ".json"
	_, err := os.Stat(path)
	if err == nil {
		return nil
	} else {
		log.Printf("no cache for %s", commit)
	}

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)
	cmd := exec.Command("nix", "eval", "--file", "./default.nix", "--raw", "--show-trace")
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	cmd.Env = append(cmd.Env, "COMMIT="+commit.Rev, "SHA="+commit.Hash)

	err = cmd.Run()
	if err != nil {
		log.Print(stderr.String())
		return fmt.Errorf("run nix: %w", err)
	}

	err = os.MkdirAll("./packages", 0770)
	if err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	err = os.WriteFile(path, stdout.Bytes(), 0644)
	if err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}
