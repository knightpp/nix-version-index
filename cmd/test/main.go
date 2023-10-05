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
	"strings"

	"github.com/gocql/gocql"
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
	rev     string
	hash    string
	query   string
	writeDb bool
)

func init() {
	flag.StringVar(&rev, "rev", "", "git revision")
	flag.StringVar(&hash, "hash", "", "unpacked tarball hash")
	flag.StringVar(&query, "query", "", "nix attrset path")
	flag.BoolVar(&writeDb, "write", false, "write to db")
}

func main() {
	flag.Parse()

	var err error
	switch {
	case rev != "" || hash != "":
		err = evaluate(Commit{Rev: rev, Hash: hash})
	case query != "":
		err = executeQuery(query)
	case writeDb:
		err = writeToDb()
	default:
		log.Print("no command specified")
	}
	if err != nil {
		log.Fatal(err)
	}
}

func createKeyspaceAndTable() error {
	cluster := gocql.NewCluster("127.0.0.1")
	cluster.Keyspace = "system"

	session, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	defer session.Close()

	err = session.Query("CREATE KEYSPACE IF NOT EXISTS versions WITH REPLICATION = {'class':'SimpleStrategy','replication_factor':1}").Exec()
	if err != nil {
		return fmt.Errorf("create keyspace: %w", err)
	}

	err = session.Query(`
		CREATE TABLE IF NOT EXISTS versions.db (
			rev text,
			attr text,
			version text,
			PRIMARY KEY ((attr,rev),version)
		) 
	`).Exec()
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	err = session.Query(`CREATE INDEX IF NOT EXISTS rev_index ON versions.db (rev)`).Exec()
	if err != nil {
		return fmt.Errorf("create rev index: %w", err)
	}

	err = session.Query(`CREATE INDEX IF NOT EXISTS attr_index ON versions.db (attr)`).Exec()
	if err != nil {
		return fmt.Errorf("create attr index: %w", err)
	}

	return nil
}

func writeToDb() error {
	err := createKeyspaceAndTable()
	if err != nil {
		return fmt.Errorf("create keyspace and table: %w", err)
	}

	cluster := gocql.NewCluster("127.0.0.1")
	cluster.Keyspace = "versions"

	session, err := cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	defer session.Close()

	dirEntries, err := os.ReadDir("packages")
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}

	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}

		rev, _, _ := strings.Cut(entry.Name(), "-")
		var count int

		err = session.Query("SELECT COUNT(*) FROM db WHERE rev = ?", rev).Scan(&count)
		if err != nil {
			return fmt.Errorf("query count: %w", err)
		}

		if count != 0 {
			continue
		}

		data, err := os.ReadFile(filepath.Join("packages", entry.Name()))
		if err != nil {
			return err
		}

		var set attrset.Set
		err = json.Unmarshal(data, &set)
		if err != nil {
			return err
		}

		batch := session.NewBatch(gocql.UnloggedBatch)

		var i int
		attrToVersion := attrset.Flatten(set)
		for attr, version := range attrToVersion {
			i++
			if i%1000 == 0 {
				fmt.Printf("processing %d/%d\n", i, len(attrToVersion))
			}

			if batch.Size() >= 500 {
				err = session.ExecuteBatch(batch)
				if err != nil {
					return fmt.Errorf("execute batch: %w", err)
				}

				batch = session.NewBatch(gocql.UnloggedBatch)
			}

			batch.Query("INSERT into db (rev,attr,version) VALUES (?, ?, ?)", rev, attr, version)
		}

		err = session.ExecuteBatch(batch)
		if err != nil {
			return fmt.Errorf("execute batch: %w", err)
		}
	}

	return nil
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
