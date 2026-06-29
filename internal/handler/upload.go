package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/resume-builder/backend/internal/model"
	"github.com/resume-builder/backend/internal/parser"
	"github.com/resume-builder/backend/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type UploadHandler struct {
	ncStore    *store.NextcloudStore
	uploadStore *store.UploadStore
}

func NewUploadHandler(ncStore *store.NextcloudStore, uploadStore *store.UploadStore) *UploadHandler {
	return &UploadHandler{ncStore: ncStore, uploadStore: uploadStore}
}

func (h *UploadHandler) Upload(c fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "missing file field")
	}

	userIDStr, ok := c.Locals("user_id").(string)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "authentication required")
	}
	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid user id")
	}

	data, err := file.Open()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to open file")
	}
	defer data.Close()

	fileBytes := make([]byte, file.Size)
	if _, err := data.Read(fileBytes); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to read file")
	}

	fileID := uuid.New().String()
	ext := getExtension(file.Filename)
	remotePath := "uploads/" + userIDStr + "/" + fileID + ext

	if err := h.ncStore.UploadFile(remotePath, fileBytes); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to upload to Nextcloud: "+err.Error())
	}

	var extractedText string
	switch ext {
	case ".docx":
		extractedText, err = parser.ExtractDocxText(fileBytes)
	case ".pdf":
		extractedText, err = parser.ExtractPDFText(fileBytes)
	default:
		return fiber.NewError(fiber.StatusBadRequest, "unsupported file type: "+ext)
	}
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to parse file: "+err.Error())
	}

	upload := model.Upload{
		ID:             primitive.NewObjectID(),
		UserID:         userID,
		FileName:       file.Filename,
		NextcloudPath:  remotePath,
		MimeType:       getMimeType(ext),
		ExtractedText:  extractedText,
		CreatedAt:      time.Now(),
	}

	if err := h.uploadStore.Create(context.Background(), &upload); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to save upload record")
	}

	return c.JSON(model.UploadFileResponse{
		FileID:        upload.ID.Hex(),
		FileName:      file.Filename,
		NextcloudPath: remotePath,
		ExtractedText: extractedText,
	})
}

func getExtension(filename string) string {
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			return filename[i:]
		}
	}
	return ""
}

func getMimeType(ext string) string {
	switch ext {
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pdf":
		return "application/pdf"
	default:
		return "application/octet-stream"
	}
}
