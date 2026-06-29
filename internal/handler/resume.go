package handler

import (
	"context"
	"io"
	"log"
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
				text, err := parser.ExtractPDFText(fileBytes)
				if err != nil {
					log.Printf("pdf extraction failed for %s: %v", fileHeader.Filename, err)
				} else {
					upload.ExtractedText = text
				}
			}

			ctx := context.Background()
			if err := h.uploadStore.Create(ctx, &upload); err != nil {
				continue
			}

			if upload.ExtractedText != "" {
				if extractedText != "" {
					extractedText += "\n\n---\n\n"
				}
				extractedText += upload.ExtractedText
			}
		}
	}

	resume := &model.Resume{
		ID:     primitive.NewObjectID(),
		UserID: userID,
		Title:  title,
		Status: model.StatusGenerating,
	}

	if err := h.resumeStore.Create(context.Background(), resume); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create resume")
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
			nil,
		)
		if err != nil {
			log.Printf("agent failed for resume %s: %v", resumeID, err)
			h.resumeStore.SetStatus(ctx, resume.ID, model.StatusFailed)
			NotifyStatusChanged(resumeID, "failed", err.Error(), "")
			return
		}

		log.Printf("agent completed for resume %s, pdf_path=%s", resumeID, result.PDFPath)

		revision := model.Revision{
			Prompt:       prompt,
			PDFPath:      result.PDFPath,
			AgentContext: result.ResumeData,
			CreatedAt:    time.Now(),
		}

		h.resumeStore.PushRevision(ctx, resume.ID, revision)
		h.resumeStore.Update(ctx, resume.ID, bson.M{
			"status":          model.StatusCompleted,
			"structured_data": result.ResumeData,
		})
		// Broadcast AFTER MongoDB writes are done, so the frontend sees updated data
		NotifyStatusChanged(resumeID, "completed", "Resume PDF is ready", result.PDFPath)
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

	var req model.RefineResumeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if req.Prompt == "" {
		return fiber.NewError(fiber.StatusBadRequest, "prompt is required")
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

	h.resumeStore.SetStatus(context.Background(), resumeID, model.StatusGenerating)

	go func() {
		ctx := context.Background()
		resumeIDStr := resumeID.Hex()
		NotifyStatusChanged(resumeIDStr, "generating", "Agent is refining your resume...", "")
		result, err := h.resumeAgent.GenerateResume(
			ctx,
			userIDStr,
			resumeIDStr,
			"",
			req.Prompt,
			history,
		)
		if err != nil {
			h.resumeStore.SetStatus(ctx, resumeID, model.StatusFailed)
			NotifyStatusChanged(resumeIDStr, "failed", err.Error(), "")
			return
		}

		revision := model.Revision{
			Prompt:       req.Prompt,
			PDFPath:      result.PDFPath,
			AgentContext: result.ResumeData,
			CreatedAt:    time.Now(),
		}

		h.resumeStore.PushRevision(ctx, resumeID, revision)
		h.resumeStore.Update(ctx, resumeID, bson.M{
			"status":          model.StatusCompleted,
			"structured_data": result.ResumeData,
		})
		NotifyStatusChanged(resumeIDStr, "completed", "Resume PDF updated", result.PDFPath)
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
