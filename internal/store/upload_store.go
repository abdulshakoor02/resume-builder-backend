package store

import (
	"context"

	"github.com/resume-builder/backend/internal/model"
	"go.mongodb.org/mongo-driver/bson/primitive"
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

// FindByResumeID returns the source uploads recorded for a resume. Owner-scoped
// so a delete can never reach another user's upload rows.
func (s *UploadStore) FindByResumeID(ctx context.Context, resumeID, userID primitive.ObjectID) ([]*model.Upload, error) {
	cursor, err := s.coll.Find(ctx, map[string]interface{}{"resume_id": resumeID, "user_id": userID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var uploads []*model.Upload
	if err := cursor.All(ctx, &uploads); err != nil {
		return nil, err
	}
	return uploads, nil
}

// DeleteByResumeID removes every upload row recorded for a resume.
func (s *UploadStore) DeleteByResumeID(ctx context.Context, resumeID, userID primitive.ObjectID) (int64, error) {
	res, err := s.coll.DeleteMany(ctx, map[string]interface{}{"resume_id": resumeID, "user_id": userID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
