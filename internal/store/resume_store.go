package store

import (
	"context"
	"time"

	"github.com/resume-builder/backend/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ResumeStore struct {
	coll *mongo.Collection
}

func NewResumeStore(db *mongo.Database) *ResumeStore {
	return &ResumeStore{coll: db.Collection("resumes")}
}

func (s *ResumeStore) Create(ctx context.Context, resume *model.Resume) error {
	if resume.ID.IsZero() {
		resume.ID = primitive.NewObjectID()
	}
	now := time.Now()
	resume.CreatedAt = now
	resume.UpdatedAt = now
	if resume.Revisions == nil {
		resume.Revisions = []model.Revision{}
	}
	_, err := s.coll.InsertOne(ctx, resume)
	return err
}

func (s *ResumeStore) FindByID(ctx context.Context, id primitive.ObjectID) (*model.Resume, error) {
	var resume model.Resume
	err := s.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&resume)
	if err != nil {
		return nil, err
	}
	return &resume, nil
}

func (s *ResumeStore) FindByResumeIDString(resumeIDStr string) (*model.Resume, error) {
	id, err := primitive.ObjectIDFromHex(resumeIDStr)
	if err != nil {
		return nil, err
	}
	return s.FindByID(context.Background(), id)
}

func (s *ResumeStore) FindByUserID(ctx context.Context, userID primitive.ObjectID) ([]*model.Resume, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cursor, err := s.coll.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var resumes []*model.Resume
	if err := cursor.All(ctx, &resumes); err != nil {
		return nil, err
	}
	if resumes == nil {
		resumes = []*model.Resume{}
	}
	return resumes, nil
}

func (s *ResumeStore) Update(ctx context.Context, id primitive.ObjectID, update bson.M) error {
	update["updated_at"] = time.Now()
	_, err := s.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": update})
	return err
}

func (s *ResumeStore) PushRevision(ctx context.Context, id primitive.ObjectID, revision model.Revision) error {
	_, err := s.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$push": bson.M{"revisions": revision},
		"$set": bson.M{
			"current_pdf_path": revision.PDFPath,
			"current_pdf_url":  revision.PDFURL,
			"updated_at":       time.Now(),
		},
	})
	return err
}

func (s *ResumeStore) SetStatus(ctx context.Context, id primitive.ObjectID, status model.ResumeStatus) error {
	return s.Update(ctx, id, bson.M{"status": status})
}
