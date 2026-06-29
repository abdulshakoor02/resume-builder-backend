package store

import (
	"context"
	"time"

	"github.com/resume-builder/backend/internal/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type UserStore struct {
	coll *mongo.Collection
}

func NewUserStore(db *mongo.Database) *UserStore {
	return &UserStore{coll: db.Collection("users")}
}

func (s *UserStore) Create(ctx context.Context, email, passwordHash string) (*model.User, error) {
	now := time.Now()
	user := &model.User{
		ID:           primitive.NewObjectID(),
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err := s.coll.InsertOne(ctx, user)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UserStore) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var user model.User
	err := s.coll.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *UserStore) FindByID(ctx context.Context, id primitive.ObjectID) (*model.User, error) {
	var user model.User
	err := s.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}
