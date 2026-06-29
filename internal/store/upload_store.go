package store

import (
	"context"

	"github.com/resume-builder/backend/internal/model"
	"go.mongodb.org/mongo-driver/mongo"
)

type UploadStore struct {
	coll *mongo.Collection
}

func NewUploadStore(db *mongo.Database) *UploadStore {
	return &UploadStore{coll: db.Collection("uploads")}
}

func (s *UploadStore) Create(ctx context.Context, upload *model.Upload) error {
	_, err := s.coll.InsertOne(ctx, upload)
	return err
}

func (s *UploadStore) FindByID(ctx context.Context, id interface{}) (*model.Upload, error) {
	var upload model.Upload
	err := s.coll.FindOne(ctx, map[string]interface{}{"_id": id}).Decode(&upload)
	if err != nil {
		return nil, err
	}
	return &upload, nil
}
