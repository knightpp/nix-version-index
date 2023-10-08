package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/dgraph-io/dgo/v230"
	"github.com/dgraph-io/dgo/v230/protos/api"
	"google.golang.org/grpc"
)

type Commit struct {
	Uid     string    `json:"uid,omitempty"`
	Rev     string    `json:"rev,omitempty"`
	Date    time.Time `json:"date,omitempty"`
	Changes []Change  `json:"changes,omitempty"`
	DType   []string  `json:"dgraph.type,omitempty"`
}

type Change struct {
	Uid      string   `json:"uid,omitempty"`
	AttrPath string   `json:"attr_path,omitempty"`
	Version  string   `json:"version,omitempty"`
	DType    []string `json:"dgraph.type,omitempty"`
}

type Dgraph struct {
	conn   *grpc.ClientConn
	client *dgo.Dgraph
}

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

	commitType := []string{"Commit"}
	changeType := []string{"Change"}
	for i := range commits {
		commits[i].DType = commitType
		commits[i].Uid = "_:commit" + strconv.Itoa(i)
		for j := range commits[i].Changes {
			commits[j].Changes[j].DType = changeType
			commits[j].Changes[j].Uid = fmt.Sprintf("_:commit%d-%d", i, j)
		}
	}

	buf, err := json.Marshal(map[string]any{
		"set": commits,
	})
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}

	_, err = txn.Mutate(ctx, &api.Mutation{
		SetJson:   buf,
		CommitNow: false,
	})
	if err != nil {
		return fmt.Errorf("mutate: %w", err)
	}

	err = txn.Commit(ctx)
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func (d *Dgraph) CommitExists(ctx context.Context, rev string) (bool, error) {
	txn := d.client.NewReadOnlyTxn()
	defer txn.Discard(ctx)

	resp, err := txn.QueryWithVars(ctx, `
		query commitExists($targetRev: string){
			findCommit(func: eq(rev, $targetRev)) {
				count(uid)
			}
		}
	`, map[string]string{"$targetRev": rev})
	if err != nil {
		return false, fmt.Errorf("query: %w", err)
	}

	var body struct {
		FindCommit []struct {
			Count int `json:"count"`
		} `json:"findCommit"`
	}

	err = json.Unmarshal(resp.Json, &body)
	if err != nil {
		return false, fmt.Errorf("json umarshal: %w", err)
	}

	return body.FindCommit[0].Count > 0, nil
}

func (d *Dgraph) DropDatabase(ctx context.Context) error {
	return d.client.Alter(ctx, &api.Operation{
		DropAll: true,
	})
}
