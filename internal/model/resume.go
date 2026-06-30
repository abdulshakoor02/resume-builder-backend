package model

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ResumeStatus string

const (
	StatusDraft      ResumeStatus = "draft"
	StatusGenerating ResumeStatus = "generating"
	StatusCompleted  ResumeStatus = "completed"
	StatusFailed     ResumeStatus = "failed"
)

type Resume struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID         primitive.ObjectID `bson:"user_id" json:"user_id"`
	Title          string             `bson:"title" json:"title"`
	Status         ResumeStatus       `bson:"status" json:"status"`
	CurrentPDFPath string             `bson:"current_pdf_path" json:"current_pdf_path"`
	CurrentPDFURL  string             `bson:"current_pdf_url" json:"current_pdf_url"`
	HTMLContent    string             `bson:"html_content,omitempty" json:"html_content,omitempty"`
	StructuredData interface{}        `bson:"structured_data,omitempty" json:"structured_data,omitempty"`
	Revisions      []Revision         `bson:"revisions,omitempty" json:"revisions,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updated_at"`
}

type Revision struct {
	Prompt       string      `bson:"prompt" json:"prompt"`
	PDFPath      string      `bson:"pdf_path" json:"pdf_path"`
	PDFURL       string      `bson:"pdf_url" json:"pdf_url"`
	AgentContext interface{} `bson:"agent_context,omitempty" json:"agent_context,omitempty"`
	CreatedAt    time.Time   `bson:"created_at" json:"created_at"`
}
