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
	"github.com/resume-builder/backend/internal/converter"
	"github.com/resume-builder/backend/internal/model"
	"github.com/resume-builder/backend/internal/store"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type ResumeHandler struct {
	resumeStore       *store.ResumeStore
	uploadStore       *store.UploadStore
	ncStore           *store.NextcloudStore
	designRefStore    *store.DesignRefStore
	photoStore        *store.PhotoStore
	resumeAgent       *agent.ResumeAgent
	anydoc            *converter.Client
	freeResumeLimit   int
	freeRevisionLimit int
}

// maxDesignRefBytes caps a design reference image. Screenshots of full resume
// pages are commonly a few hundred KB; 5MB leaves room without letting a request
// push tens of megabytes into the model call.
const maxDesignRefBytes = 5 * 1024 * 1024

func NewResumeHandler(
	resumeStore *store.ResumeStore,
	uploadStore *store.UploadStore,
	ncStore *store.NextcloudStore,
	designRefStore *store.DesignRefStore,
	photoStore *store.PhotoStore,
	resumeAgent *agent.ResumeAgent,
	anydoc *converter.Client,
	freeResumeLimit int,
	freeRevisionLimit int,
) *ResumeHandler {
	return &ResumeHandler{
		resumeStore:       resumeStore,
		uploadStore:       uploadStore,
		ncStore:           ncStore,
		designRefStore:    designRefStore,
		photoStore:        photoStore,
		resumeAgent:       resumeAgent,
		anydoc:            anydoc,
		freeResumeLimit:   freeResumeLimit,
		freeRevisionLimit: freeRevisionLimit,
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

	// Check free tier resume limit
	completed, err := h.resumeStore.CountCompletedResumes(context.Background(), userID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to check usage")
	}
	if completed >= h.freeResumeLimit {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"error":           "free_limit_reached",
			"message":         "You have reached the free resume limit. Please upgrade to create more resumes.",
			"resumes_created": completed,
			"limit":           h.freeResumeLimit,
		})
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

	// ---- Design reference image (optional) ----
	// A picture of a resume design the user wants reproduced. It is sent to the
	// model as a multimodal content part and contributes *appearance only* — the
	// instructions forbid copying any text out of it (see DesignRefInstructions).
	var designRefDataURI string
	var designRefMime string
	var designRefBytes []byte
	if form != nil && form.File != nil {
		if refHeaders, ok := form.File["design_ref"]; ok && len(refHeaders) > 0 {
			rh := refHeaders[0]
			if rh.Size > maxDesignRefBytes {
				return fiber.NewError(fiber.StatusBadRequest, "design reference image must be under 5MB")
			}
			rf, err := rh.Open()
			if err != nil {
				log.Printf("design_ref: failed to open: %v", err)
			} else {
				rb, rErr := io.ReadAll(rf)
				rf.Close()
				if rErr != nil {
					log.Printf("design_ref: failed to read: %v", rErr)
				} else {
					contentType := http.DetectContentType(rb)
					if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
						return fiber.NewError(fiber.StatusBadRequest,
							fmt.Sprintf("design reference must be a JPEG, PNG or WebP image (got %s)", contentType))
					}
					log.Printf("design_ref: received %s, size=%d bytes, type=%s", rh.Filename, len(rb), contentType)
					designRefMime = contentType
					designRefBytes = rb
					designRefDataURI = "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(rb)
				}
			}
		}
	}

	resume := &model.Resume{
		ID:            primitive.NewObjectID(),
		UserID:        userID,
		Title:         title,
		Status:        model.StatusGenerating,
		DesignRefMime: designRefMime,
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
				ID:       primitive.NewObjectID(),
				UserID:   userID,
				ResumeID: resume.ID, // resume.ID exists before the doc is inserted, so link up front
				FileName: fileHeader.Filename,
				MimeType: fileHeader.Header.Get("Content-Type"),
				CreatedAt: time.Now(),
			}

			ext := fileExt(fileHeader.Filename)
			upload.NextcloudPath = "uploads/" + userIDStr + "/" + upload.ID.Hex() + ext
			if err := h.ncStore.UploadFile(upload.NextcloudPath, fileBytes); err != nil {
				log.Printf("nextcloud upload failed for %s (continuing with local extraction): %v", fileHeader.Filename, err)
			}

			if h.anydoc == nil {
				log.Printf("anydoc client is not configured")
			} else if text, conversionErr := h.anydoc.ConvertWithFallback(context.Background(), fileBytes, fileHeader.Filename); conversionErr != nil {
				log.Printf("document extraction failed for %s: %v", fileHeader.Filename, conversionErr)
			} else {
				upload.ExtractedText = text
			}

			ctx := context.Background()
			if err := h.uploadStore.Create(ctx, &upload); err != nil {
				continue
			}

			if upload.ExtractedText != "" {
				log.Printf("extracted text from %s: %d chars", fileHeader.Filename, len(upload.ExtractedText))
				if extractedText != "" {
					extractedText += "\n\n---\n\n"
				}
				extractedText += upload.ExtractedText
			}
		}
	}

	// Refuse to invent a resume when the upload produced no text.
	//
	// With extractedText == "" the fast path degrades to "create a resume from
	// this sentence", and the model happily returns a fully fabricated CV
	// (placeholder name, fake employers) that the UI reports as "completed".
	// Silent fabrication is worse than a visible failure: fail loudly and let
	// the caller fix the input (text-based PDF / DOCX) or paste the content.
	if hasFiles && extractedText == "" {
		log.Printf("extraction produced no text from %d uploaded file(s) - refusing to generate from the prompt alone (prompt_len=%d)", len(form.File["files"]), len(prompt))
		return fiber.NewError(fiber.StatusUnprocessableEntity,
			"Couldn't read any text from your uploaded file, so there is nothing to redesign. "+
				"The file may be a scanned or image-only PDF. Please upload a text-based PDF or DOCX, "+
				"or paste your resume content into the instructions and we'll build from that.")
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
		// Durable copy: the object store is broken and the cache dies with the
		// process, so /photo and later refinements need these bytes in Mongo.
		if h.photoStore != nil {
			if pErr := h.photoStore.Put(context.Background(), resume.ID, userID, http.DetectContentType(photoBytes), photoBytes); pErr != nil {
				log.Printf("photo: persist failed, /photo may 404 after a restart: %v", pErr)
			}
		}
	}

	// Persist the design reference so later refinements keep the same look. The
	// bytes go in their own collection rather than on the resume document: the
	// list endpoint returns whole resume documents and a base64 blob on each one
	// would bloat every dashboard load.
	if designRefBytes != nil && h.designRefStore != nil {
		if refErr := h.designRefStore.Put(context.Background(), resume.ID, userID, designRefMime, designRefBytes); refErr != nil {
			log.Printf("design_ref: persist failed, generation continues but refinements won't reuse it: %v", refErr)
		}
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
			nil,          // conversationHistory
			photoDataURI, // profile photo (base64 data URI or empty)
			designRefDataURI, // design reference image (base64 data URI or empty)
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
	userID, err := userIDFromLocals(c)
	if err != nil {
		return err
	}
	id, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid resume id")
	}

	resume, err := h.resumeStore.FindByIDForUser(context.Background(), id, userID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "resume not found")
	}

	return c.JSON(resume)
}

// userIDFromLocals returns the authenticated user id injected by
// AuthRequiredMiddleware. Handlers behind that middleware use it both to scope
// lookups and to answer 401 when the middleware was bypassed.
func userIDFromLocals(c fiber.Ctx) (primitive.ObjectID, error) {
	userIDStr, ok := c.Locals("user_id").(string)
	if !ok || userIDStr == "" {
		return primitive.NilObjectID, fiber.NewError(fiber.StatusUnauthorized, "authentication required")
	}
	userID, err := primitive.ObjectIDFromHex(userIDStr)
	if err != nil {
		return primitive.NilObjectID, fiber.NewError(fiber.StatusUnauthorized, "invalid user id")
	}
	return userID, nil
}

// Delete removes a resume and everything derived from it: the Mongo document,
// its source-upload rows, the stored HTML/PDF objects, the profile photo and the
// in-process caches. Object deletion is best-effort — the object store is
// periodically out of sync, and a missing object must not leave a resume the
// user can no longer delete — so partial failures are reported in the response
// instead of failing the request.
func (h *ResumeHandler) Delete(c fiber.Ctx) error {
	userID, err := userIDFromLocals(c)
	if err != nil {
		return err
	}
	resumeID, err := primitive.ObjectIDFromHex(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid resume id")
	}

	ctx := context.Background()
	resume, err := h.resumeStore.FindByIDForUser(ctx, resumeID, userID)
	if err != nil {
		// Missing and not-yours get the same answer, so probing IDs reveals nothing.
		return fiber.NewError(fiber.StatusNotFound, "resume not found")
	}
	resumeHex := resume.ID.Hex()

	// Every stored object this resume owns, de-duplicated.
	paths := map[string]struct{}{}
	addPath := func(p string) {
		if strings.TrimSpace(p) != "" {
			paths[p] = struct{}{}
		}
	}
	addPath(resume.CurrentPDFPath)
	addPath(resume.PhotoPath)
	for _, rev := range resume.Revisions {
		addPath(rev.PDFPath)
	}

	uploads, uErr := h.uploadStore.FindByResumeID(ctx, resume.ID, userID)
	if uErr != nil {
		log.Printf("delete resume %s: upload lookup failed: %v", resumeHex, uErr)
	}
	for _, u := range uploads {
		addPath(u.NextcloudPath)
	}

	resp := model.DeleteResumeResponse{Deleted: true, ResumeID: resumeHex}

	// 1. Stored objects (path by path, so one failure doesn't stop the rest).
	if h.ncStore != nil {
		for p := range paths {
			if err := h.ncStore.DeleteFile(p); err != nil {
				resp.FilesFailed++
				log.Printf("delete resume %s: object delete failed for %s: %v", resumeHex, p, err)
				if len(resp.Errors) < 5 {
					resp.Errors = append(resp.Errors, fmt.Sprintf("%s: %v", p, err))
				}
				continue
			}
			resp.FilesDeleted++
		}
	}

	// 2. Database rows — resume last, so a mid-way failure stays retryable.
	deletedUploads, err := h.uploadStore.DeleteByResumeID(ctx, resume.ID, userID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete resume uploads")
	}
	resp.UploadsDeleted = deletedUploads

	// Design reference bytes live in their own collection; drop them with the
	// resume so nothing of a deleted resume survives anywhere.
	if h.designRefStore != nil {
		if refsDeleted, refErr := h.designRefStore.DeleteByResumeID(ctx, resume.ID, userID); refErr != nil {
			log.Printf("delete resume %s: design reference delete failed: %v", resumeHex, refErr)
		} else {
			resp.DesignRefsDeleted = refsDeleted
		}
	}

	// Same for the profile photo bytes.
	if h.photoStore != nil {
		if photosDeleted, pErr := h.photoStore.DeleteByResumeID(ctx, resume.ID, userID); pErr != nil {
			log.Printf("delete resume %s: photo delete failed: %v", resumeHex, pErr)
		} else {
			resp.PhotosDeleted = photosDeleted
		}
	}

	deleted, err := h.resumeStore.DeleteByID(ctx, resume.ID, userID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete resume")
	}
	if deleted == 0 {
		return fiber.NewError(fiber.StatusNotFound, "resume not found")
	}

	// 3. In-process caches: revision HTML/PDF, photo, uploaded file bytes.
	resp.CachePurged = store.PurgeResumeCache(resumeHex) + store.PurgeResumeFiles(resumeHex)

	log.Printf("delete resume %s: objects=%d failed=%d uploads=%d cache_purged=%d",
		resumeHex, resp.FilesDeleted, resp.FilesFailed, resp.UploadsDeleted, resp.CachePurged)
	return c.JSON(resp)
}

// loadStoredPhoto returns the resume's existing profile photo as a data URI,
// looking in the in-process cache, then the object store, then the durable Mongo
// copy. Empty when the resume has no photo anywhere (or the stored bytes are not
// a usable image).
func (h *ResumeHandler) loadStoredPhoto(resumeID, userID primitive.ObjectID, photoPath string) string {
	hex := resumeID.Hex()
	if data, ok := store.GetPhoto(hex); ok && len(data) > 0 {
		return photoDataURIFor(data, "cache")
	}
	if photoPath != "" && h.ncStore != nil {
		if data, err := h.ncStore.DownloadFile(photoPath); err == nil && len(data) > 0 {
			store.PutPhoto(hex, data)
			return photoDataURIFor(data, "object store")
		}
	}
	if h.photoStore != nil {
		if p, err := h.photoStore.Get(context.Background(), resumeID, userID); err == nil && p != nil && len(p.Data) > 0 {
			store.PutPhoto(hex, p.Data)
			return photoDataURIFor(p.Data, "mongodb")
		}
	}
	return ""
}

// photoDataURIFor validates stored photo bytes and encodes them, naming the
// source in the log so a missing avatar is diagnosable after the fact.
func photoDataURIFor(data []byte, source string) string {
	ct := http.DetectContentType(data)
	if ct != "image/jpeg" && ct != "image/png" && ct != "image/webp" {
		log.Printf("photo: stored bytes are %s, not an image (source=%s) - ignoring", ct, source)
		return ""
	}
	log.Printf("photo: reusing stored photo (%d bytes, source=%s)", len(data), source)
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (h *ResumeHandler) Refine(c fiber.Ctx) error {
	userID, err := userIDFromLocals(c)
	if err != nil {
		return err
	}
	userIDStr := userID.Hex()

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

	resume, err := h.resumeStore.FindByIDForUser(context.Background(), resumeID, userID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "resume not found")
	}

	// Reuse the design reference the resume was created with, so a refinement
	// keeps the look the user asked for instead of drifting to a new design.
	var designRefDataURI string
	if h.designRefStore != nil {
		if ref, refErr := h.designRefStore.Get(context.Background(), resumeID, userID); refErr == nil && ref != nil && len(ref.Data) > 0 {
			designRefDataURI = "data:" + ref.MimeType + ";base64," + base64.StdEncoding.EncodeToString(ref.Data)
			log.Printf("refine: reusing stored design reference (%d bytes)", len(ref.Data))
		} else if refErr != nil && refErr != mongo.ErrNoDocuments {
			log.Printf("refine: design reference lookup failed (continuing without it): %v", refErr)
		}
	}

	// Check free tier revision limit
	revisionCount, err := h.resumeStore.CountTotalRevisions(context.Background(), resume.UserID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to check usage")
	}
	if revisionCount >= h.freeRevisionLimit {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"error":           "free_limit_reached",
			"message":         "You have reached the free revision limit. Please upgrade for unlimited revisions.",
			"total_revisions": revisionCount,
			"limit":           h.freeRevisionLimit,
		})
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

	// Profile photo: a newly attached one is stored; otherwise reuse whatever the
	// resume already has, so the avatar survives a refinement that doesn't
	// re-attach it (and so it can be re-inlined after generation).
	if photoBytes != nil {
		if ncErr := h.ncStore.UploadFile(photoPath, photoBytes); ncErr != nil {
			log.Printf("photo: nextcloud upload failed (non-fatal): %v", ncErr)
		}
		store.PutPhoto(resumeID.Hex(), photoBytes)
		if h.photoStore != nil {
			if pErr := h.photoStore.Put(context.Background(), resumeID, userID, http.DetectContentType(photoBytes), photoBytes); pErr != nil {
				log.Printf("photo: persist failed, later refinements may lose the avatar: %v", pErr)
			}
		}
	} else {
		photoDataURI = h.loadStoredPhoto(resumeID, userID, resume.PhotoPath)
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
			designRefDataURI, // design reference reused from creation (empty if none)
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
