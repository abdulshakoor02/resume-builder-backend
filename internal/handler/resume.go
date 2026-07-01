package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/resume-builder/backend/internal/agent"
	"github.com/resume-builder/backend/internal/model"
	"github.com/resume-builder/backend/internal/parser"
	"github.com/resume-builder/backend/internal/store"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ResumeHandler struct {
	resumeStore *store.ResumeStore
	uploadStore *store.UploadStore
	ncStore     *store.NextcloudStore
	resumeAgent *agent.ResumeAgent
}

func NewResumeHandler(
	resumeStore *store.ResumeStore,
	uploadStore *store.UploadStore,
	ncStore *store.NextcloudStore,
	resumeAgent *agent.ResumeAgent,
) *ResumeHandler {
	return &ResumeHandler{
		resumeStore: resumeStore,
		uploadStore: uploadStore,
		ncStore:     ncStore,
		resumeAgent: resumeAgent,
	}
}

func (h *ResumeHandler) Create(c fiber.Ctx) error {
	userIDStr, ok := c.Locals("user_id").(string)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "authentication required")
	}
	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid user id")
	}

	// Parse files early so form is available in both branches
	form, err := c.MultipartForm()
	hasFiles := form != nil && form.File != nil && len(form.File["files"]) > 0
	
	log.Printf("MultipartForm: err=%v hasFiles=%v formKeys=%v", err, hasFiles, func() []string {
		if form == nil || form.File == nil {
			return nil
		}
		keys := make([]string, 0, len(form.File))
		for k := range form.File {
			keys = append(keys, k)
		}
		return keys
	}())

	prompt := c.FormValue("prompt", "")
	title := c.FormValue("title", prompt)

	if prompt == "" {
		if hasFiles {
			prompt = "Create a professional resume based on the uploaded file(s). Improve formatting, phrasing, and design."
			if title == "" {
				title = "Uploaded Resume"
			}
		} else {
			return fiber.NewError(fiber.StatusBadRequest, "prompt is required")
		}
	}
	if len(title) > 100 {
		title = title[:100]
	}

	// ---- Profile photo (optional) ----
	var photoDataURI string
	var photoPath string
	var photoBytes []byte
	if form != nil && form.File != nil {
		if photoHeaders, ok := form.File["photo"]; ok && len(photoHeaders) > 0 {
			ph := photoHeaders[0]
			if ph.Size > 2*1024*1024 {
				return fiber.NewError(fiber.StatusBadRequest, "photo must be under 2MB")
			}
			photoExt := strings.ToLower(filepath.Ext(ph.Filename))
			if photoExt == "" {
				photoExt = ".jpg"
			}
			mimeType := ph.Header.Get("Content-Type")
			if mimeType == "" {
				switch photoExt {
				case ".jpg", ".jpeg":
					mimeType = "image/jpeg"
				case ".png":
					mimeType = "image/png"
				case ".webp":
					mimeType = "image/webp"
				}
			}
			if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
				return fiber.NewError(fiber.StatusBadRequest, "photo must be JPEG, PNG, or WebP")
			}
			pf, err := ph.Open()
			if err != nil {
				log.Printf("photo: failed to open: %v", err)
			} else {
				pb, err := io.ReadAll(pf)
				pf.Close()
				if err != nil {
					log.Printf("photo: failed to read: %v", err)
				} else {
					contentType := http.DetectContentType(pb)
					if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
						return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("photo appears to be %s, not a valid image", contentType))
					}
					log.Printf("photo: received %s, size=%d bytes", ph.Filename, len(pb))
					photoDataURI = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(pb)
					photoPath = "photos/" + userIDStr + "/{{RESUME_ID}}" + photoExt // placeholder, replaced after resume creation
					photoBytes = pb
				}
			}
		}
	}

	resume := &model.Resume{
		ID:     primitive.NewObjectID(),
		UserID: userID,
		Title:  title,
		Status: model.StatusGenerating,
	}

	var extractedText string

	if hasFiles {
		log.Printf("processing %d uploaded files", len(form.File["files"]))
		files := form.File["files"]
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				continue
			}
			fileBytes, err := io.ReadAll(file)
			file.Close()
			if err != nil {
				log.Printf("failed to read uploaded file %s: %v", fileHeader.Filename, err)
				continue
			}

			log.Printf("file received: %s, size=%d bytes", fileHeader.Filename, len(fileBytes))

			upload := model.Upload{
				ID:        primitive.NewObjectID(),
				UserID:    userID,
				FileName:  fileHeader.Filename,
				MimeType:  fileHeader.Header.Get("Content-Type"),
				CreatedAt: time.Now(),
			}

			ext := fileExt(fileHeader.Filename)
			upload.NextcloudPath = "uploads/" + userIDStr + "/" + upload.ID.Hex() + ext
			if err := h.ncStore.UploadFile(upload.NextcloudPath, fileBytes); err != nil {
				log.Printf("nextcloud upload failed for %s (continuing with local extraction): %v", fileHeader.Filename, err)
			}

			switch ext {
			case ".docx":
				text, err := parser.ExtractDocxText(fileBytes)
				if err != nil {
					log.Printf("docx extraction failed for %s: %v", fileHeader.Filename, err)
				} else {
					upload.ExtractedText = text
				}
		case ".pdf":
				// Try PDF text-layer parser first (works well for text-based PDFs).
				text, err := parser.ExtractPDFText(fileBytes)
				if err != nil {
					log.Printf("pdf text extraction failed for %s: %v", fileHeader.Filename, err)
				} else {
					upload.ExtractedText = text
					log.Printf("pdf text parser produced %d chars for %s", len(text), fileHeader.Filename)
				}
			}

			ctx := context.Background()
			if err := h.uploadStore.Create(ctx, &upload); err != nil {
				continue
			}

			if upload.ExtractedText != "" {
				log.Printf("extracted text from %s: %d chars, preview: %.200s", fileHeader.Filename, len(upload.ExtractedText), upload.ExtractedText)
				if extractedText != "" {
					extractedText += "\n\n---\n\n"
				}
				extractedText += upload.ExtractedText
			} else {
				log.Printf("no text layer in %s, trying vision-based extraction...", fileHeader.Filename)
				visionText, visionErr := parser.ExtractPDFTextWithVision(fileBytes)
				if visionErr != nil {
					log.Printf("vision extraction also failed for %s: %v", fileHeader.Filename, visionErr)
				} else if visionText != "" {
					log.Printf("vision extraction succeeded for %s: %d chars", fileHeader.Filename, len(visionText))
					upload.ExtractedText = visionText
					if extractedText != "" {
						extractedText += "\n\n---\n\n"
					}
					extractedText += visionText
				}
			}
		}
	}

	if err := h.resumeStore.Create(context.Background(), resume); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create resume")
	}

	// Finalize photo path now that we have the resume ID, and upload to Nextcloud.
	if photoBytes != nil {
		photoPath = strings.Replace(photoPath, "{{RESUME_ID}}", resume.ID.Hex(), 1)
		if ncErr := h.ncStore.UploadFile(photoPath, photoBytes); ncErr != nil {
			log.Printf("photo: nextcloud upload failed (non-fatal): %v", ncErr)
		}
		store.PutPhoto(resume.ID.Hex(), photoBytes)
	}

	log.Printf("resume created: id=%s title=%s status=generating prompt_len=%d extracted_text_len=%d", 
		resume.ID.Hex(), title, len(prompt), len(extractedText))

	// Check if agent is configured
	if h.resumeAgent == nil {
		log.Printf("resumeAgent is nil - LLM not configured")
		h.resumeStore.SetStatus(context.Background(), resume.ID, model.StatusFailed)
		return c.Status(fiber.StatusCreated).JSON(model.CreateResumeResponse{
			ResumeID: resume.ID.Hex(),
		})
	}

	go func() {
		ctx := context.Background()
		resumeID := resume.ID.Hex()
		log.Printf("agent started for resume %s", resumeID)
		NotifyStatusChanged(resumeID, "generating", "Agent is analyzing your resume...", "")
		result, err := h.resumeAgent.GenerateResume(
			ctx,
			userIDStr,
			resumeID,
			extractedText,
			prompt,
			nil,            // conversationHistory
			photoDataURI,   // profile photo (base64 data URI or empty)
		)
		if err != nil {
			log.Printf("agent failed for resume %s: %v", resumeID, err)
			h.resumeStore.SetStatus(ctx, resume.ID, model.StatusFailed)
			NotifyStatusChanged(resumeID, "failed", err.Error(), "")
			return
		}

		log.Printf("agent completed for resume %s, html_path=%s", resumeID, result.HTMLPath)

		revision := model.Revision{
			Prompt:       prompt,
			PDFPath:      result.HTMLPath,
			AgentContext: result.ResumeData,
			CreatedAt:    time.Now(),
		}

		h.resumeStore.PushRevision(ctx, resume.ID, revision)

		updateFields := bson.M{
			"status":          model.StatusCompleted,
			"structured_data": result.ResumeData,
		}
		// Persist the generated HTML so it survives restarts and is always retrievable
		if htmlData, ok := store.GetHTML(resumeID); ok {
			updateFields["html_content"] = string(htmlData)
		}
		if photoPath != "" {
			updateFields["photo_path"] = photoPath
		}
		h.resumeStore.Update(ctx, resume.ID, updateFields)
		// Broadcast AFTER MongoDB writes are done, so the frontend sees updated data
		NotifyStatusChanged(resumeID, "completed", "Resume design is ready", result.HTMLPath)
	}()

	return c.Status(fiber.StatusCreated).JSON(model.CreateResumeResponse{
		ResumeID: resume.ID.Hex(),
	})
}

func (h *ResumeHandler) List(c fiber.Ctx) error {
	userIDStr, _ := c.Locals("user_id").(string)
	userID, _ := primitive.ObjectIDFromHex(userIDStr)

	resumes, err := h.resumeStore.FindByUserID(context.Background(), userID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to fetch resumes")
	}

	return c.JSON(fiber.Map{
		"resumes": resumes,
	})
}

func (h *ResumeHandler) Get(c fiber.Ctx) error {
	id, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid resume id")
	}

	resume, err := h.resumeStore.FindByID(context.Background(), id)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "resume not found")
	}

	return c.JSON(resume)
}

func (h *ResumeHandler) Refine(c fiber.Ctx) error {
	userIDStr, _ := c.Locals("user_id").(string)

	resumeID, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid resume id")
	}

	// Try JSON first, then multipart form (for photo upload)
	var req model.RefineResumeRequest
	var photoDataURI string
	var photoBytes []byte
	var photoPath string

	contentType := c.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		form, err := c.MultipartForm()
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid multipart form")
		}
		prompt := form.Value["prompt"]
		if len(prompt) == 0 || prompt[0] == "" {
			return fiber.NewError(fiber.StatusBadRequest, "prompt is required")
		}
		req.Prompt = prompt[0]

		// Handle photo upload
		if form.File != nil {
			if photoHeaders, ok := form.File["photo"]; ok && len(photoHeaders) > 0 {
				ph := photoHeaders[0]
				if ph.Size > 2*1024*1024 {
					return fiber.NewError(fiber.StatusBadRequest, "photo must be under 2MB")
				}
				photoExt := strings.ToLower(filepath.Ext(ph.Filename))
				if photoExt == "" {
					photoExt = ".jpg"
				}
				mimeType := ph.Header.Get("Content-Type")
				if mimeType == "" {
					switch photoExt {
					case ".jpg", ".jpeg":
						mimeType = "image/jpeg"
					case ".png":
						mimeType = "image/png"
					case ".webp":
						mimeType = "image/webp"
					}
				}
				if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
					return fiber.NewError(fiber.StatusBadRequest, "photo must be JPEG, PNG, or WebP")
				}
				pf, err := ph.Open()
				if err != nil {
					log.Printf("photo: failed to open: %v", err)
				} else {
					pb, err := io.ReadAll(pf)
					pf.Close()
					if err != nil {
						log.Printf("photo: failed to read: %v", err)
					} else {
						ct := http.DetectContentType(pb)
						if ct != "image/jpeg" && ct != "image/png" && ct != "image/webp" {
							return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("photo appears to be %s, not a valid image", ct))
						}
						log.Printf("photo: received %s, size=%d bytes", ph.Filename, len(pb))
						photoDataURI = "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(pb)
						photoBytes = pb
						photoPath = "photos/" + userIDStr + "/" + resumeID.Hex() + photoExt
					}
				}
			}
		}
	} else {
		if err := c.Bind().JSON(&req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
		}
		if req.Prompt == "" {
			return fiber.NewError(fiber.StatusBadRequest, "prompt is required")
		}
	}

	resume, err := h.resumeStore.FindByID(context.Background(), resumeID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "resume not found")
	}

	var history []map[string]string
	for _, rev := range resume.Revisions {
		history = append(history, map[string]string{
			"prompt": rev.Prompt,
		})
	}

	// Include existing structured data so the agent has full context of what it's modifying
	if resume.StructuredData != nil {
		if dataBytes, err := json.Marshal(resume.StructuredData); err == nil {
			history = append(history, map[string]string{
				"context": string(dataBytes),
			})
		}
	}

	h.resumeStore.SetStatus(context.Background(), resumeID, model.StatusGenerating)

	// Upload photo to Nextcloud and cache if present
	if photoBytes != nil {
		if ncErr := h.ncStore.UploadFile(photoPath, photoBytes); ncErr != nil {
			log.Printf("photo: nextcloud upload failed (non-fatal): %v", ncErr)
		}
		store.PutPhoto(resumeID.Hex(), photoBytes)
	}

	go func() {
		ctx := context.Background()
		resumeIDStr := resumeID.Hex()
		NotifyStatusChanged(resumeIDStr, "generating", "Agent is refining your resume...", "")

		// Reload resume from DB in the goroutine so we always have
		// fresh StructuredData + HTMLContent for context.
		freshResume, loadErr := h.resumeStore.FindByID(context.Background(), resumeID)
		if loadErr == nil && freshResume != nil {
			// Rebuild history with fresh data
			history = nil
			for _, rev := range freshResume.Revisions {
				history = append(history, map[string]string{
					"prompt": rev.Prompt,
				})
			}
			if freshResume.StructuredData != nil {
				if dataBytes, err := json.Marshal(freshResume.StructuredData); err == nil {
					history = append(history, map[string]string{
						"context": string(dataBytes),
					})
				}
			}
			if freshResume.HTMLContent != "" {
				history = append(history, map[string]string{
					"html": freshResume.HTMLContent,
				})
			}
		}

		result, err := h.resumeAgent.GenerateResume(
			ctx,
			userIDStr,
			resumeIDStr,
			"",
			req.Prompt,
			history,
			photoDataURI, // photoDataURI — empty if no photo uploaded
		)
		if err != nil {
			h.resumeStore.SetStatus(ctx, resumeID, model.StatusFailed)
			NotifyStatusChanged(resumeIDStr, "failed", err.Error(), "")
			return
		}

		revision := model.Revision{
			Prompt:       req.Prompt,
			PDFPath:      result.HTMLPath,
			AgentContext: result.ResumeData,
			CreatedAt:    time.Now(),
		}

		h.resumeStore.PushRevision(ctx, resumeID, revision)

		updateFields := bson.M{
			"status":          model.StatusCompleted,
			"structured_data": result.ResumeData,
		}
		if htmlData, ok := store.GetHTML(resumeIDStr); ok {
			updateFields["html_content"] = string(htmlData)
		}
		if photoPath != "" {
			updateFields["photo_path"] = photoPath
		}
		h.resumeStore.Update(ctx, resumeID, updateFields)
		NotifyStatusChanged(resumeIDStr, "completed", "Resume design updated", result.HTMLPath)
	}()

	return c.JSON(model.RefineResumeResponse{})
}

func fileExt(filename string) string {
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			return filename[i:]
		}
	}
	return ""
}
