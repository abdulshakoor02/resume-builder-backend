package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/resume-builder/backend/internal/store"
)

type ExportHandler struct {
	resumeStore *store.ResumeStore
	ncStore     *store.NextcloudStore
}

func NewExportHandler(resumeStore *store.ResumeStore, ncStore *store.NextcloudStore) *ExportHandler {
	return &ExportHandler{resumeStore: resumeStore, ncStore: ncStore}
}

func (h *ExportHandler) Download(c fiber.Ctx) error {
	resumeID := c.Params("id")

	// Try in-memory cache by resume ID
	if data, ok := store.GetAnyCacheByResumeID(resumeID); ok {
		contentType := "text/html"
		if len(data) > 10 && string(data[:5]) == "%PDF-" {
			contentType = "application/pdf"
		}
		c.Set("Content-Type", contentType)
		c.Set("Content-Disposition", "inline; filename=\"resume.html\"")
		return c.Send(data)
	}

	// Fallback: try MongoDB
	resume, err := h.resumeStore.FindByResumeIDString(resumeID)
	if err == nil && resume != nil && resume.CurrentPDFPath != "" {
		if data, ok := store.GetHTML(resume.CurrentPDFPath); ok {
			c.Set("Content-Type", "text/html")
			c.Set("Content-Disposition", "inline; filename=\"resume.html\"")
			return c.Send(data)
		}
	}

	return fiber.NewError(fiber.StatusNotFound, "resume not yet generated")
}
