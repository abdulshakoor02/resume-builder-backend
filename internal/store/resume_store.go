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

// FindByIDForUser scopes a lookup to the owner. Any handler that takes a resume
// ID from the URL must go through this (or compare UserID itself): with a bare
// FindByID one authenticated user can read, refine or delete another user's
// resume by guessing/leaking its ObjectID.
func (s *ResumeStore) FindByIDForUser(ctx context.Context, id, userID primitive.ObjectID) (*model.Resume, error) {
	var resume model.Resume
	if err := s.coll.FindOne(ctx, bson.M{"_id": id, "user_id": userID}).Decode(&resume); err != nil {
		return nil, err
	}
	return &resume, nil
}

// DeleteByID removes a resume, scoped to its owner. Returns rows deleted, so a
// caller can tell "not yours / not found" from "deleted".
func (s *ResumeStore) DeleteByID(ctx context.Context, id, userID primitive.ObjectID) (int64, error) {
	res, err := s.coll.DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
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

// FailStaleGenerating marks resumes still in "generating" that haven't been
// touched since cutoff as failed.
//
// Generation runs in-process, so a deploy or crash mid-build orphans the
// document in "generating" forever: the dashboard polls it and spins with no way
// out (the frontend only stops on completed/failed). Startup reconciles whatever
// the previous process left behind; the periodic sweep catches a worker that
// died on its own.
func (s *ResumeStore) FailStaleGenerating(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.coll.UpdateMany(ctx,
		bson.M{
			"status": model.StatusGenerating,
			"$or": []bson.M{
				{"updated_at": bson.M{"$lt": cutoff}},
				{"updated_at": bson.M{"$exists": false}, "created_at": bson.M{"$lt": cutoff}},
			},
		},
		bson.M{"$set": bson.M{"status": model.StatusFailed, "updated_at": time.Now()}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (s *ResumeStore) SetStatus(ctx context.Context, id primitive.ObjectID, status model.ResumeStatus) error {
	return s.Update(ctx, id, bson.M{"status": status})
}

func (s *ResumeStore) CountCompletedResumes(ctx context.Context, userID primitive.ObjectID) (int, error) {
	count, err := s.coll.CountDocuments(ctx, bson.M{
		"user_id": userID,
		"status":  model.StatusCompleted,
	})
	return int(count), err
}

func (s *ResumeStore) CountTotalRevisions(ctx context.Context, userID primitive.ObjectID) (int, error) {
	// $size errors on a document where `revisions` is absent — it is omitempty and
	// is only pushed once a generation succeeds — which made /api/usage answer
	// 500 "failed to count revisions" (and the dashboard show a broken usage
	// banner) for a window after every create. $ifNull treats a missing array as
	// empty.
	revisionCount := bson.M{"$size": bson.M{"$ifNull": []interface{}{"$revisions", []interface{}{}}}}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"user_id": userID}}},
		{{Key: "$project", Value: bson.M{"revision_count": revisionCount}}},
		{{Key: "$group", Value: bson.M{"_id": nil, "total": bson.M{"$sum": "$revision_count"}}}},
	}
	cursor, err := s.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		Total int `bson:"total"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return results[0].Total, nil
}
