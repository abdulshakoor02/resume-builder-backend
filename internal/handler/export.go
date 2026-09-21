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
	photoStore  *store.PhotoStore
}

func NewExportHandler(resumeStore *store.ResumeStore, ncStore *store.NextcloudStore, photoStore *store.PhotoStore) *ExportHandler {
	return &ExportHandler{resumeStore: resumeStore, ncStore: ncStore, photoStore: photoStore}
}

func (h *ExportHandler) Download(c fiber.Ctx) error {
	userID, err := userIDFromLocals(c)
	if err != nil {
		return err
	}
	resumeID := c.Params("id")

	// Ownership FIRST: the in-memory cache is keyed by resume ID alone, so serving
	// it before this check would hand over another user's resume to anyone who
	// knew (or was shown) an ID.
	resume, err := h.resumeStore.FindByResumeIDString(resumeID)
	if err != nil || resume == nil || resume.UserID != userID {
		return fiber.NewError(fiber.StatusNotFound, "resume not found")
	}

	// 1. Fast path: in-memory cache (while the server is running)
	if data, ok := store.GetAnyCacheByResumeID(resumeID); ok {
		return serveResume(c, data)
	}

	// 2. Persisted HTML in MongoDB (survives restarts — primary retention path)
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

	return fiber.NewError(fiber.StatusNotFound, "resume not yet generated")
}

func serveResume(c fiber.Ctx, data []byte) error {
	contentType := "text/html"
	if len(data) > 10 && string(data[:5]) == "%PDF-" {
		contentType = "application/pdf"
	}
	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", "inline; filename=\"resume.html\"")
	// The dashboard re-reads this after every revision. Without an explicit
	// directive the browser caches it heuristically and keeps rendering the
	// previous document, so a change the user just made looks like it did
	// nothing.
	c.Set("Cache-Control", "no-store")
	return c.Send(data)
}

func (h *ExportHandler) Photo(c fiber.Ctx) error {
	userID, err := userIDFromLocals(c)
	if err != nil {
		return err
	}
	resumeID := c.Params("id")

	// Owner-scoped, and before the cache: the photo cache is keyed by resume ID.
	resume, err := h.resumeStore.FindByResumeIDString(resumeID)
	if err != nil || resume == nil || resume.UserID != userID {
		return fiber.NewError(fiber.StatusNotFound, "photo not found")
	}

	// Try in-memory cache first
	if data, ok := store.GetPhoto(resumeID); ok {
		contentType := http.DetectContentType(data)
		c.Set("Content-Type", contentType)
		c.Set("Cache-Control", "private, max-age=0, must-revalidate")
		return c.Send(data)
	}

	// Durable copy next: the object store mirror is unreliable, and the cache
	// above is empty after a restart.
	if h.photoStore != nil {
		if p, pErr := h.photoStore.Get(c.Context(), resume.ID, userID); pErr == nil && p != nil && len(p.Data) > 0 {
			store.PutPhoto(resumeID, p.Data)
			c.Set("Content-Type", http.DetectContentType(p.Data))
			c.Set("Cache-Control", "private, max-age=0, must-revalidate")
			return c.Send(p.Data)
		}
	}

	// If photo_path is stored in DB, try to fetch from Nextcloud
	if resume.PhotoPath != "" && h.ncStore != nil {
		if data, dErr := h.ncStore.DownloadFile(resume.PhotoPath); dErr == nil {
			store.PutPhoto(resumeID, data)
			contentType := http.DetectContentType(data)
			c.Set("Content-Type", contentType)
			c.Set("Cache-Control", "private, max-age=0, must-revalidate")
			return c.Send(data)
		}
	}
	// Fallback: check if there's any uploaded file cache for this resume
	if data, ok := store.GetUploadedFile(resumeID); ok {
		contentType := http.DetectContentType(data)
		c.Set("Content-Type", contentType)
		c.Set("Cache-Control", "private, max-age=0, must-revalidate")
		return c.Send(data)
	}

	return fiber.NewError(fiber.StatusNotFound, "photo not found")
}
