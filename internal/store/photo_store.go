package store

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Photo holds the bytes of a resume's profile photo.
//
// The photo is written to the object store too, but that mirror is unreliable
// (its base-url prefix check fails, so every upload 404s), and the in-process
// photo cache dies with the process. Without a durable copy the bytes were gone
// after the next deploy: /photo answered 404 and a later refinement could not
// re-inline the image, so the avatar silently vanished from the document. Mongo
// is the storage that is actually reachable, so the bytes live here.
//
// They are kept out of the resume document itself because the list endpoint
// returns whole resume documents and a base64 blob on each would bloat every
// dashboard load.
type Photo struct {
	ResumeID  primitive.ObjectID `bson:"_id"`
	UserID    primitive.ObjectID `bson:"user_id"`
	MimeType  string             `bson:"mime_type"`
	Data      []byte             `bson:"data"`
	CreatedAt time.Time          `bson:"created_at"`
}

type PhotoStore struct {
	coll *mongo.Collection
}

func NewPhotoStore(db *mongo.Database) *PhotoStore {
	return &PhotoStore{coll: db.Collection("photos")}
}

// Put stores (or replaces) the profile photo for a resume, owner-scoped.
func (s *PhotoStore) Put(ctx context.Context, resumeID, userID primitive.ObjectID, mimeType string, data []byte) error {
	_, err := s.coll.ReplaceOne(ctx,
		bson.M{"_id": resumeID, "user_id": userID},
		Photo{ResumeID: resumeID, UserID: userID, MimeType: mimeType, Data: data, CreatedAt: time.Now()},
		options.Replace().SetUpsert(true),
	)
	return err
}

// Get returns a resume's stored photo. Returns mongo.ErrNoDocuments when the
// resume has no photo.
func (s *PhotoStore) Get(ctx context.Context, resumeID, userID primitive.ObjectID) (*Photo, error) {
	var p Photo
	if err := s.coll.FindOne(ctx, bson.M{"_id": resumeID, "user_id": userID}).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteByResumeID drops the photo, owner-scoped. Returns rows removed.
func (s *PhotoStore) DeleteByResumeID(ctx context.Context, resumeID, userID primitive.ObjectID) (int64, error) {
	res, err := s.coll.DeleteOne(ctx, bson.M{"_id": resumeID, "user_id": userID})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}
