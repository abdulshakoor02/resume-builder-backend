package server

import (
	"github.com/gofiber/fiber/v3"
	"github.com/resume-builder/backend/internal/handler"
)

func RegisterRoutes(
	app *fiber.App,
	authH *handler.AuthHandler,
	resumeH *handler.ResumeHandler,
	uploadH *handler.UploadHandler,
	exportH *handler.ExportHandler,
	jwtSecret string,
) {
	api := app.Group("/api")

	api.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	api.Get("/sse", handler.HandleSSE())

	auth := api.Group("/auth")
	auth.Post("/signup", authH.Signup)
	auth.Post("/login", authH.Login)

	upload := api.Group("/upload")
	upload.Post("/", uploadH.Upload)

	// Public photo endpoint (no auth) — MUST be registered before
	// the auth-protected resumes group, otherwise Fiber matches
	// api/resumes/:id first and applies the auth middleware.
	api.Get("/resumes/:id/photo", exportH.Photo)

	resumes := api.Group("/resumes", AuthRequiredMiddleware(jwtSecret))
	resumes.Post("/", resumeH.Create)
	resumes.Get("/", resumeH.List)
	resumes.Get("/:id", resumeH.Get)
	resumes.Post("/:id/refine", resumeH.Refine)
	resumes.Get("/:id/pdf", exportH.Download)
}
