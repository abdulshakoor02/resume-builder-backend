package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// DesignRef is a user-supplied design reference image (a resume whose look the
// user wants reproduced). The bytes live in their own collection rather than on
// the resume document: the list endpoint returns full resume documents, and a
// multi-megabyte base64 blob on each would bloat every dashboard load.
type DesignRef struct {
	ResumeID  primitive.ObjectID `bson:"_id"`
	UserID    primitive.ObjectID `bson:"user_id"`
	MimeType  string             `bson:"mime_type"`
	Data      []byte             `bson:"data"`
	CreatedAt time.Time          `bson:"created_at"`
}

type DesignRefStore struct {
	coll *mongo.Collection
}

func NewDesignRefStore(db *mongo.Database) *DesignRefStore {
	return &DesignRefStore{coll: db.Collection("design_refs")}
}

// Put stores (or replaces) the reference image for a resume. Keyed by resume ID,
// so re-uploading a reference for the same resume overwrites it.
func (s *DesignRefStore) Put(ctx context.Context, resumeID, userID primitive.ObjectID, mimeType string, data []byte) error {
	_, err := s.coll.ReplaceOne(ctx,
		bson.M{"_id": resumeID, "user_id": userID},
		DesignRef{ResumeID: resumeID, UserID: userID, MimeType: mimeType, Data: data, CreatedAt: time.Now()},
		options.Replace().SetUpsert(true),
	)
	return err
}

// Get returns the stored reference image for a resume, scoped to its owner.
// Returns mongo.ErrNoDocuments when the resume has no reference image.
func (s *DesignRefStore) Get(ctx context.Context, resumeID, userID primitive.ObjectID) (*DesignRef, error) {
	var ref DesignRef
	if err := s.coll.FindOne(ctx, bson.M{"_id": resumeID, "user_id": userID}).Decode(&ref); err != nil {
		return nil, err
	}
	return &ref, nil
}

// DeleteByResumeID drops the reference image, owner-scoped. Returns rows removed.
func (s *DesignRefStore) DeleteByResumeID(ctx context.Context, resumeID, userID primitive.ObjectID) (int64, error) {
	res, err := s.coll.DeleteOne(ctx, bson.M{"_id": resumeID, "user_id": userID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
