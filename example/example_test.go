package example

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/ugorji/go/codec"

	"github.com/bennerv/go-cosmosdb/example/cosmosdb"
	"github.com/bennerv/go-cosmosdb/example/types"
)

const (
	dbid      = "testdb"
	collid    = "people"
	triggerid = "trigger"
	personid  = "jim"
	userid    = "reader"
	permid    = "reader-perm"

	triggerbody = `function trigger() {
	var request = getContext().getRequest();
	var body = request.getBody();
	var ts = new Date();
	body["updateTime"] = ts.getTime();
	request.setBody(body);
}`
)

const oneYear = time.Hour * 24 * 365

func TestE2E(t *testing.T) {
	ctx := context.Background()
	log := logrus.NewEntry(logrus.StandardLogger())

	account, found := os.LookupEnv("COSMOSDB_ACCOUNT")
	if !found {
		t.Fatal("must set COSMOSDB_ACCOUNT")
	}

	key, found := os.LookupEnv("COSMOSDB_KEY")
	if !found {
		t.Fatal("must set COSMOSDB_KEY")
	}

	jsonHandle := &codec.JsonHandle{
		BasicHandle: codec.BasicHandle{
			DecodeOptions: codec.DecodeOptions{
				ErrorIfNoField: true,
			},
		},
	}

	keyAuthorizer, err := cosmosdb.NewMasterKeyAuthorizer(key)
	if err != nil {
		t.Error(err)
	}

	dbc := cosmosdb.NewDatabaseClient(log, http.DefaultClient, jsonHandle, account+".documents.azure.com", keyAuthorizer)

	db, err := dbc.Create(ctx, &cosmosdb.Database{ID: dbid})
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", db)

	dbs, err := dbc.ListAll(ctx)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", dbs)

	db, err = dbc.Get(ctx, dbid)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", db)

	userc := cosmosdb.NewUserClient(dbc, dbid)

	user, err := userc.Create(ctx, &cosmosdb.User{ID: userid})
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", user)

	users, err := userc.ListAll(ctx)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", users)

	user, err = userc.Get(ctx, userid)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", user)

	collc := cosmosdb.NewCollectionClient(dbc, dbid)

	coll, err := collc.Create(ctx, &cosmosdb.Collection{
		ID: collid,
		PartitionKey: &cosmosdb.PartitionKey{
			Paths: []string{
				"/id",
			},
		},
	})
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", coll)

	colls, err := collc.ListAll(ctx)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", colls)

	coll, err = collc.Get(ctx, collid)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", coll)

	pkrs, err := collc.PartitionKeyRanges(ctx, collid)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", pkrs)

	triggerc := cosmosdb.NewTriggerClient(collc, collid)

	trigger, err := triggerc.Create(ctx, &cosmosdb.Trigger{
		ID:               triggerid,
		TriggerOperation: cosmosdb.TriggerOperationAll,
		TriggerType:      cosmosdb.TriggerTypePre,
		Body:             triggerbody,
	})
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", trigger)

	triggers, err := triggerc.ListAll(ctx)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", triggers)

	trigger, err = triggerc.Get(ctx, triggerid)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", trigger)

	permc := cosmosdb.NewPermissionClient(userc, userid)

	perm, err := permc.Create(ctx, &cosmosdb.Permission{
		ID:             permid,
		PermissionMode: cosmosdb.PermissionModeAll,
		Resource:       "dbs/" + dbid + "/colls/" + collid,
	})
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", perm)

	perms, err := permc.ListAll(ctx)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", perms)

	perm, err = permc.Get(ctx, permid)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", perm)

	dc := cosmosdb.NewPersonClient(collc, collid)

	doc, err := dc.Create(ctx, personid, &types.Person{
		ID:      personid,
		Surname: "Minter",
	}, &cosmosdb.Options{PreTriggers: []string{triggerid}})
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", doc)

	docs, err := dc.ListAll(ctx, nil)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", docs)

	doc, err = dc.Get(ctx, personid, personid, nil)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", doc)

	docs, err = dc.QueryAll(ctx, personid, &cosmosdb.Query{
		Query: "SELECT * FROM people WHERE people.surname = @surname",
		Parameters: []cosmosdb.Parameter{
			{
				Name:  "@surname",
				Value: "Minter",
			},
		},
	}, nil)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", docs)

	i := dc.ChangeFeed(nil)
	docs, err = i.Next(ctx, 1)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", docs)
	if len(docs.People) != 1 || docs.People[0].Surname != "Minter" {
		t.Error(len(docs.People))
	}

	docs, err = i.Next(ctx, 1)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", docs)
	if docs != nil {
		t.Error(docs)
	}

	oldETag := doc.ETag
	doc, err = dc.Replace(ctx, personid, &types.Person{
		ID:      personid,
		ETag:    doc.ETag,
		Surname: "Morrison",
	}, &cosmosdb.Options{PreTriggers: []string{triggerid}})
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", doc)

	_, err = dc.Replace(ctx, personid, &types.Person{
		ID:      personid,
		ETag:    oldETag,
		Surname: "Henson",
	}, &cosmosdb.Options{PreTriggers: []string{triggerid}})
	if !cosmosdb.IsErrorStatusCode(err, http.StatusPreconditionFailed) {
		t.Error(err)
	}

	docs, err = i.Next(ctx, 1)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", docs)
	if len(docs.People) != 1 || docs.People[0].Surname != "Morrison" {
		t.Error(len(docs.People))
	}

	docs, err = i.Next(ctx, 1)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", docs)
	if docs != nil {
		t.Error(docs)
	}

	// dummy token expiration auth
	tokenAuth := cosmosdb.NewTokenAuthorizer(perm.Token, time.Now().Add(oneYear), func(ctx context.Context) (token string, newExpiration time.Time, err error) {
		return perm.Token, time.Now().Add(oneYear), nil
	})

	tokendbc := cosmosdb.NewDatabaseClient(log, http.DefaultClient, jsonHandle, account+".documents.azure.com", tokenAuth)
	tokencollc := cosmosdb.NewCollectionClient(tokendbc, dbid)
	tokendc := cosmosdb.NewPersonClient(tokencollc, collid)

	doc, err = tokendc.Get(ctx, personid, personid, nil)
	if err != nil {
		t.Error(err)
	}
	t.Logf("%#v\n", doc)

	err = permc.Delete(ctx, perm)
	if err != nil {
		t.Error(err)
	}

	err = dc.Delete(ctx, personid, doc, nil)
	if err != nil {
		t.Error(err)
	}

	err = triggerc.Delete(ctx, trigger)
	if err != nil {
		t.Error(err)
	}

	err = collc.Delete(ctx, coll)
	if err != nil {
		t.Error(err)
	}

	err = userc.Delete(ctx, user)
	if err != nil {
		t.Error(err)
	}

	err = dbc.Delete(ctx, db)
	if err != nil {
		t.Error(err)
	}
}
