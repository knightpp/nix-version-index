package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/knightpp/nix-version-index/internal/store"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

//go:embed schema.dql
var gqlSchema string

var (
	rev     string
	hash    string
	query   string
	writeDb bool
	dropDb  bool
)

func init() {
	flag.StringVar(&rev, "rev", "", "git revision")
	flag.StringVar(&hash, "hash", "", "unpacked tarball hash")
	flag.StringVar(&query, "query", "", "nix attrset path")
	flag.BoolVar(&writeDb, "write", false, "write to db")
	flag.BoolVar(&dropDb, "drop", false, "drop db")
}

func main() {
	flag.Parse()

	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
	log.Logger = logger

	var err error
	switch {
	case rev != "" || hash != "":
		err = evaluate(rev, hash)
	// case writeDb:
	// 	err = dgraphRun()
	// default:
	// 	log.Print("no command specified")
	default:
		err = runDgraph(context.Background())
	}
	if err != nil {
		log.Fatal().Err(err).Send()
	}
}

func openOrCloneRepo(workTreePath string) (*git.Repository, error) {
	gitPath := filepath.Join(workTreePath, ".git")
	gitStorage := filesystem.NewStorage(osfs.New(gitPath), cache.NewObjectLRUDefault())
	workTree := osfs.New(workTreePath)

	repo, err := git.Open(gitStorage, workTree)
	if err != nil {
		switch {
		case errors.Is(err, git.ErrRepositoryNotExists):
			return cloneRepo(gitStorage, workTree)
		default:
			return nil, fmt.Errorf("open git: %w", err)
		}
	}

	return repo, nil
}

func cloneRepo(s storage.Storer, workTree billy.Filesystem) (*git.Repository, error) {
	repo, err := git.Clone(s, workTree, &git.CloneOptions{
		URL:           "https://github.com/NixOS/nixpkgs.git",
		ReferenceName: "master",
		SingleBranch:  true,
		NoCheckout:    true,
		Progress:      os.Stdout,
		Tags:          git.NoTags,
	})
	if err != nil {
		return nil, fmt.Errorf("clone nixpkgs: %w", err)
	}

	return repo, nil
}

func runDgraph(ctx context.Context) error {
	const (
		nixpkgsPath = "/tmp/nixpkgs"
		commitV014  = "6ed8a76ac64c88df0df3f01b536498983ad5ad23"
	)

	client, err := store.ConnectDgraph(ctx, "127.0.0.1:9080")
	if err != nil {
		return fmt.Errorf("connect dgraph: %w", err)
	}

	defer client.Close()

	if dropDb {
		return client.DropDatabase(ctx)
	}

	repo, err := openOrCloneRepo(nixpkgsPath)
	if err != nil {
		return fmt.Errorf("open or clone repo: %w", err)
	}

	// wt, err := repo.Worktree()
	// if err != nil {
	// 	return fmt.Errorf("worktree: %w", err)
	// }

	// log.Info().Msg("git pull")
	// err = wt.PullContext(ctx, &git.PullOptions{
	// 	RemoteName:    "upstream",
	// 	ReferenceName: "refs/heads/master",
	// 	SingleBranch:  true,
	// 	Progress:      os.Stdout,
	// })
	// if err != nil {
	// 	switch {
	// 	case errors.Is(err, git.NoErrAlreadyUpToDate):
	// 		log.Info().Msg("up to date!")
	// 	default:
	// 		return fmt.Errorf("pull: %w", err)
	// 	}
	// }

	// log.Info().Msg("git checkout")
	// err = wt.Checkout(&git.CheckoutOptions{
	// 	Branch: "refs/heads/master",
	// 	Force:  true,
	// })
	// if err != nil {
	// 	return fmt.Errorf("checkout: %w", err)
	// }

	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("head: %w", err)
	}

	err = client.CreateSchema(ctx, gqlSchema)
	if err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	iter, err := repo.Log(&git.LogOptions{
		From:  head.Hash(),
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return fmt.Errorf("git log: %w", err)
	}

	defer iter.Close()

	// for commit.Hash.String() != commitV014 {
	for i := 0; i < 10; i++ {
		commit, err := iter.Next()
		if err != nil {
			return fmt.Errorf("next commit: %w", err)
		}

		log.Info().Time("when", commit.Author.When).Str("hash", commit.Hash.String()).Msg("got commit")

		ok, err := client.CommitExists(ctx, commit.Hash.String())
		if err != nil {
			return fmt.Errorf("commit exists: %w", err)
		}

		if ok {
			log.Info().Stringer("hash", commit.Hash).Msg("already exists")
			continue
		}

		err = client.Write(ctx, store.Commit{
			Rev:   rev,
			Date:  commit.Author.When,
			DType: []string{"Commit"},
			Changes: []store.Change{
				{
					AttrPath: "python3",
					Version:  "3.0.1",
					DType:    []string{"Change"},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("write: %w", err)
		}
	}

	return nil
}

func evaluate(rev, hash string) error {
	path := fmt.Sprintf("packages/%s-%s.json", rev, hash)
	_, err := os.Stat(path)
	if err == nil {
		return nil
	} else {
		log.Printf("no cache for %q", path)
	}

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)
	cmd := exec.Command("nix", "eval", "--file", "./default.nix", "--raw", "--show-trace")
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	cmd.Env = append(cmd.Env, "COMMIT="+rev, "SHA="+hash)

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
