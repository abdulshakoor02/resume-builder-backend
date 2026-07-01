package handler

import (
	"log"
	"net/http"

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

func (h *ExportHandler) Photo(c fiber.Ctx) error {
	resumeID := c.Params("id")

	// Try in-memory cache first
	if data, ok := store.GetPhoto(resumeID); ok {
		contentType := http.DetectContentType(data)
		c.Set("Content-Type", contentType)
		c.Set("Cache-Control", "public, max-age=31536000")
		return c.Send(data)
	}

	// Try database for photo_path, then Nextcloud
	resume, err := h.resumeStore.FindByResumeIDString(resumeID)
	if err == nil && resume != nil {
		// If photo_path is stored in DB, try to fetch from Nextcloud
		if resume.PhotoPath != "" && h.ncStore != nil {
			if data, dErr := h.ncStore.DownloadFile(resume.PhotoPath); dErr == nil {
				store.PutPhoto(resumeID, data)
				contentType := http.DetectContentType(data)
				c.Set("Content-Type", contentType)
				c.Set("Cache-Control", "public, max-age=31536000")
				return c.Send(data)
			}
		}
		// Fallback: check if there's any uploaded file cache for this resume
		if data, ok := store.GetUploadedFile(resumeID); ok {
			contentType := http.DetectContentType(data)
			c.Set("Content-Type", contentType)
			c.Set("Cache-Control", "public, max-age=31536000")
			return c.Send(data)
		}
	}

	return fiber.NewError(fiber.StatusNotFound, "photo not found")
}
