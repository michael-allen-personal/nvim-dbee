package drivers

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/kndndrj/nvim-dbee/dbee/core"
	"github.com/kndndrj/nvim-dbee/dbee/core/builders"
)

// Register client
func init() {
	c := func(url string) (core.Driver, error) {
		return NewMongo(url)
	}
	_ = register(c, "mongo", "mongodb")

	// register known types with gob
	// full list available in go.mongodb.org/.../bson godoc
	gob.Register(&mongoResponse{})
	gob.Register(bson.A{})
	gob.Register(bson.M{})
	gob.Register(bson.D{})
	gob.Register(primitive.ObjectID{})
	// gob.Register(primitive.DateTime)
	gob.Register(primitive.Binary{})
	gob.Register(primitive.Regex{})
	// gob.Register(primitive.JavaScript)
	gob.Register(primitive.CodeWithScope{})
	gob.Register(primitive.Timestamp{})
	gob.Register(primitive.Decimal128{})
	// gob.Register(primitive.MinKey{})
	// gob.Register(primitive.MaxKey{})
	// gob.Register(primitive.Undefined{})
	gob.Register(primitive.DBPointer{})
	// gob.Register(primitive.Symbol)
}

var _ core.Driver = (*Mongo)(nil)

type Mongo struct {
	c      *mongo.Client
	dbName string
}

func NewMongo(rawURL string) (*Mongo, error) {
	// get database name from url
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("mongo: invalid url: %w", err)
	}

	opts := options.Client().ApplyURI(rawURL)
	client, err := mongo.Connect(context.TODO(), opts)
	if err != nil {
		return nil, err
	}

	return &Mongo{
		c:      client,
		dbName: u.Path[1:],
	}, nil
}

func (c *Mongo) getCurrentDatabase(ctx context.Context) (string, error) {
	if c.dbName != "" {
		return c.dbName, nil
	}

	dbs, err := c.c.ListDatabaseNames(ctx, bson.D{})
	if err != nil {
		return "", fmt.Errorf("failed to select default database: %w", err)
	}
	if len(dbs) < 1 {
		return "", fmt.Errorf("no databases found")
	}
	c.dbName = dbs[0]

	return c.dbName, nil
}

func (c *Mongo) Query(ctx context.Context, query string) (core.ResultStream, error) {
	dbName, err := c.getCurrentDatabase(ctx)
	if err != nil {
		return nil, err
	}
	db := c.c.Database(dbName)

	var command any
	err = bson.UnmarshalExtJSON([]byte(query), false, &command)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal command: \"%v\" to bson: %v", query, err)
	}

	var resp bson.M
	err = db.RunCommand(ctx, command).Decode(&resp)
	if err != nil {
		return nil, err
	}

	// check if "cursor" field exists and create an appropriate func
	var next func() (core.Row, error)
	var hasNext func() bool

	cur, ok := resp["cursor"]
	if ok {
		next, hasNext = builders.NextYield(func(yield func(any)) error {
			cursor := cur.(bson.M)
			if !ok {
				return errors.New("type assertion for cursor object failed")
			}

			for _, b := range cursor {
				batch, ok := b.(bson.A)
				if !ok {
					fmt.Println(b)
					continue
				}
				for _, item := range batch {
					yield(newMongoResponse(item))
				}
			}
			return nil
		})
	} else {
		next, hasNext = builders.NextSingle(newMongoResponse(resp))
	}

	// build result
	result := builders.NewResultStreamBuilder().
		WithNextFunc(next, hasNext).
		WithHeader(core.Header{"Reply"}).
		WithMeta(&core.Meta{
			SchemaType: core.SchemaLess,
		}).
		Build()

	return result, nil
}

func (c *Mongo) Structure() ([]*core.Structure, error) {
	ctx := context.Background()

	dbName, err := c.getCurrentDatabase(ctx)
	if err != nil {
		return nil, err
	}

	collections, err := c.c.Database(dbName).ListCollectionNames(ctx, bson.D{})
	if err != nil {
		return nil, err
	}

	var structure []*core.Structure

	for _, coll := range collections {
		structure = append(structure, &core.Structure{
			Name:   coll,
			Schema: "",
			Type:   core.StructureTypeTable,
		})
	}

	return structure, nil
}

func (c *Mongo) Close() {
	_ = c.c.Disconnect(context.TODO())
}

func (c *Mongo) ListDatabases() (current string, available []string, err error) {
	ctx := context.Background()

	dbName, err := c.getCurrentDatabase(ctx)
	if err != nil {
		return "", nil, err
	}

	all, err := c.c.ListDatabaseNames(ctx, bson.D{{
		Key: "name",
		Value: bson.D{{
			Key: "$not",
			Value: bson.D{{
				Key:   "$regex",
				Value: dbName,
			}},
		}},
	}})
	if err != nil {
		return "", nil, fmt.Errorf("failed to retrieve database names: %w", err)
	}

	return dbName, all, nil
}

func (c *Mongo) SelectDatabase(name string) error {
	c.dbName = name
	return nil
}

// mongoResponse serves as a wrapper around the mongo response
// to stringify the return values
type mongoResponse struct {
	Value any
}

func newMongoResponse(val any) *mongoResponse {
	return &mongoResponse{
		Value: val,
	}
}

func (mr *mongoResponse) String() string {
	parsed, err := json.MarshalIndent(mr.Value, "", "  ")
	if err != nil {
		return fmt.Sprint(mr.Value)
	}
	return string(parsed)
}

func (mr *mongoResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(mr.Value)
}
