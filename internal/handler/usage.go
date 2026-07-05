package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/resume-builder/backend/internal/model"
	"github.com/resume-builder/backend/internal/store"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type UsageHandler struct {
	resumeStore       *store.ResumeStore
	freeResumeLimit   int
	freeRevisionLimit int
}

func NewUsageHandler(
	resumeStore *store.ResumeStore,
	freeResumeLimit int,
	freeRevisionLimit int,
) *UsageHandler {
	return &UsageHandler{
		resumeStore:       resumeStore,
		freeResumeLimit:   freeResumeLimit,
		freeRevisionLimit: freeRevisionLimit,
	}
}

func (h *UsageHandler) GetUsage(c fiber.Ctx) error {
	userIDStr, ok := c.Locals("user_id").(string)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "authentication required")
	}
	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid user id")
	}

	ctx := context.Background()

	completed, err := h.resumeStore.CountCompletedResumes(ctx, userID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to count resumes")
	}

	revisions, err := h.resumeStore.CountTotalRevisions(ctx, userID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to count revisions")
	}

	return c.JSON(model.UsageResponse{
		ResumesCreated:    completed,
		TotalRevisions:    revisions,
		FreeResumeLimit:   h.freeResumeLimit,
		FreeRevisionLimit: h.freeRevisionLimit,
		CanCreate:         completed < h.freeResumeLimit,
		CanRevise:         revisions < h.freeRevisionLimit,
	})
}
