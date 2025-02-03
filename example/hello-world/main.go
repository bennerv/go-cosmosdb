package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/bennerv/go-cosmosdb/example/cosmosdb"
	"github.com/bennerv/go-cosmosdb/example/types"

	"github.com/sirupsen/logrus"
	"github.com/ugorji/go/codec"
)

const (
	dbid   = "example-database"
	collid = "example"
)

var (
	persons = []types.Person{
		types.Person{
			ID:      "jim",
			Surname: "minter",
		},
		types.Person{
			ID:      "mangirdas",
			Surname: "judeikis",
		},
	}
)

func main() {
	ctx := context.Background()
	log := logrus.NewEntry(logrus.StandardLogger())

	err := run(ctx, log)
	if err != nil {
		log.Fatal(err)
	}

}

func run(ctx context.Context, log *logrus.Entry) error {
	// read cosmosdb account
	account, found := os.LookupEnv("COSMOSDB_ACCOUNT")
	if !found {
		return fmt.Errorf("must set COSMOSDB_ACCOUNT")
	}

	key, found := os.LookupEnv("COSMOSDB_KEY")
	if !found {
		return fmt.Errorf("must set COSMOSDB_KEY")
	}

	// jsonHandle enables custom encoders. In example field encryption or marshaling
	jsonHandle := &codec.JsonHandle{
		BasicHandle: codec.BasicHandle{
			DecodeOptions: codec.DecodeOptions{
				ErrorIfNoField: true,
			},
		},
	}

	// create authorizer for rest calls
	keyAuthorizer, err := cosmosdb.NewMasterKeyAuthorizer(key)
	if err != nil {
		return err
	}

	// get database client
	dbc := cosmosdb.NewDatabaseClient(log, http.DefaultClient, jsonHandle, account+".documents.azure.com", keyAuthorizer)

	// create database "example-database"
	db, err := dbc.Create(ctx, &cosmosdb.Database{ID: dbid})
	if err != nil && !cosmosdb.IsErrorStatusCode(err, http.StatusConflict) {
		return err
	}

	log.Info("database: %#v\n", db)

	collc := cosmosdb.NewCollectionClient(dbc, dbid)

	coll, err := collc.Create(ctx, &cosmosdb.Collection{
		ID: collid,
		PartitionKey: &cosmosdb.PartitionKey{
			Paths: []string{
				"/id",
			},
		},
	})
	if err != nil && !cosmosdb.IsErrorStatusCode(err, http.StatusConflict) {
		return err
	}
	log.Info("collection: %#v\n", coll)

	// Create persons document client in the collections above
	dc := cosmosdb.NewPersonClient(collc, collid)

	// HACK: if you are re-running this application we delete existing users first
	// As cosmosdb do not support truncate do it old fashion way
	for _, person := range persons {
		err = dc.Delete(ctx, person.ID, &person, nil)
		if err != nil {
			return err
		}
	}

	for _, person := range persons {
		_, err := dc.Create(ctx, person.ID, &person, &cosmosdb.Options{})
		if err != nil {
			return err
		}
	}

	// Read document back

	docs, err := dc.ListAll(ctx, &cosmosdb.Options{})
	if err != nil {
		return err
	}

	// Print back the values
	for _, person := range docs.People {
		log.Info("documents: %#v\n", person)
	}

	return nil
}
