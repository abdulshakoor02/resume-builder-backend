package handler

import (
	"log"

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

	// 1. Fast path: in-memory cache (while the server is running)
	if data, ok := store.GetAnyCacheByResumeID(resumeID); ok {
		return serveResume(c, data)
	}

	// 2. Persisted HTML in MongoDB (survives restarts — primary retention path)
	resume, err := h.resumeStore.FindByResumeIDString(resumeID)
	if err == nil && resume != nil {
		if resume.HTMLContent != "" {
			data := []byte(resume.HTMLContent)
			store.PutHTML(resumeID, data) // warm the cache for subsequent requests
			return serveResume(c, data)
		}

		// 3. Fallback: Nextcloud file storage (for resumes without persisted HTML)
		if resume.CurrentPDFPath != "" && h.ncStore != nil {
			if data, dErr := h.ncStore.DownloadFile(resume.CurrentPDFPath); dErr == nil {
				store.PutHTML(resumeID, data)
				return serveResume(c, data)
			} else {
				log.Printf("export: nextcloud download failed for %s: %v", resume.CurrentPDFPath, dErr)
			}
		}
	}

	return fiber.NewError(fiber.StatusNotFound, "resume not yet generated")
}

func serveResume(c fiber.Ctx, data []byte) error {
	contentType := "text/html"
	if len(data) > 10 && string(data[:5]) == "%PDF-" {
		contentType = "application/pdf"
	}
	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", "inline; filename=\"resume.html\"")
	return c.Send(data)
}
