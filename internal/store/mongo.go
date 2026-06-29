package store

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoStore struct {
	Client   *mongo.Client
	DB       *mongo.Database
}

func NewMongoStore(ctx context.Context, uri, dbName string) (*MongoStore, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetConnectTimeout(10*time.Second))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	db := client.Database(dbName)

	if err := ensureIndexes(ctx, db); err != nil {
		return nil, fmt.Errorf("ensure indexes: %w", err)
	}

	return &MongoStore{Client: client, DB: db}, nil
}

func ensureIndexes(ctx context.Context, db *mongo.Database) error {
	userIdx := mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	}
	if _, err := db.Collection("users").Indexes().CreateOne(ctx, userIdx); err != nil {
		return err
	}

	resumeIdx := mongo.IndexModel{
		Keys: bson.D{
			{Key: "user_id", Value: 1},
			{Key: "created_at", Value: -1},
		},
	}
	if _, err := db.Collection("resumes").Indexes().CreateOne(ctx, resumeIdx); err != nil {
		return err
	}

	uploadIdx := mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}},
	}
	if _, err := db.Collection("uploads").Indexes().CreateOne(ctx, uploadIdx); err != nil {
		return err
	}

	return nil
}

func (s *MongoStore) Close(ctx context.Context) error {
	return s.Client.Disconnect(ctx)
}
