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

	// 1. Try in-memory cache by resume ID (no MongoDB read needed, works instantly)
	if pdfBytes, ok := store.GetPDFByResumeID(resumeID); ok {
		c.Set("Content-Type", "application/pdf")
		c.Set("Content-Disposition", "inline; filename=\"resume.pdf\"")
		return c.Send(pdfBytes)
	}

	// 2. Fallback: query MongoDB then cache
	resume, err := h.resumeStore.FindByResumeIDString(resumeID)
	if err == nil && resume != nil && resume.CurrentPDFPath != "" {
		if pdfBytes, ok := store.GetPDF(resume.CurrentPDFPath); ok {
			c.Set("Content-Type", "application/pdf")
			c.Set("Content-Disposition", "inline; filename=\"resume.pdf\"")
			return c.Send(pdfBytes)
		}
	}

	return fiber.NewError(fiber.StatusNotFound, "pdf not yet generated")
}
