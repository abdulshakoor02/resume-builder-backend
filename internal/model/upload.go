package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Upload struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID         primitive.ObjectID `bson:"user_id" json:"user_id"`
	ResumeID       primitive.ObjectID `bson:"resume_id,omitempty" json:"resume_id,omitempty"`
	FileName       string             `bson:"filename" json:"filename"`
	NextcloudPath  string             `bson:"nextcloud_path" json:"nextcloud_path"`
	MimeType       string             `bson:"mime_type" json:"mime_type"`
	ExtractedText  string             `bson:"extracted_text" json:"extracted_text"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
}
