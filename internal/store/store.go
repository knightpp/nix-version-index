package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dgraph-io/dgo/v230"
	"github.com/dgraph-io/dgo/v230/protos/api"
	"google.golang.org/grpc"
)

type Commit struct {
	Rev     string     `json:"rev,omitempty"`
	Date    *time.Time `json:"date,omitempty"`
	Changes []Change   `json:"changes,omitempty"`
}

type Change struct {
	AttrPath string `json:"attr_path,omitempty"`
	Version  string `json:"version,omitempty"`
}

type Dgraph struct {
	conn   *grpc.ClientConn
	client *dgo.Dgraph
}

// "127.0.0.1:9080"
func ConnectDgraph(ctx context.Context, target string) (*Dgraph, error) {
	conn, err := grpc.DialContext(ctx, target, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("grpc dial: %w", err)
	}

	client := dgo.NewDgraphClient(api.NewDgraphClient(conn))

	return &Dgraph{
		conn:   conn,
		client: client,
	}, nil
}

func (d *Dgraph) Close() error {
	return d.conn.Close()
}

func (d *Dgraph) CreateSchema(ctx context.Context, schema string) error {
	err := d.client.Alter(ctx, &api.Operation{
		Schema:          schema,
		RunInBackground: false,
	})
	if err != nil {
		return fmt.Errorf("alter schema: %w", err)
	}

	return nil
}

func (d *Dgraph) Write(ctx context.Context, commits ...Commit) error {
	txn := d.client.NewTxn()
	defer txn.Discard(ctx)

	var buf bytes.Buffer
	for _, commit := range commits {
		buf.Reset()
		err := json.NewEncoder(&buf).Encode(commit)
		if err != nil {
			return fmt.Errorf("encode json: %w", err)
		}

		_, err = txn.Mutate(ctx, &api.Mutation{
			SetJson:   buf.Bytes(),
			CommitNow: false,
		})
		if err != nil {
			return fmt.Errorf("mutate: %w", err)
		}
	}

	err := txn.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func (d *Dgraph) CommitExists(ctx context.Context) (bool, error) {
	txn := d.client.NewReadOnlyTxn()
	defer txn.Discard(ctx)

	resp, err := txn.QueryWithVars(ctx, `
		queryCommit(filter: {rev: {eq: "dwa"}}) {
    		rev
  		}
	`, map[string]string{})
	if err != nil {
		return false, fmt.Errorf("query: %w", err)
	}

	_ = resp

	return false, nil
}
