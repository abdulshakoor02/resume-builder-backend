package server

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/resume-builder/backend/internal/config"
)

func New(cfg *config.Config) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "Resume Builder API",
		ErrorHandler: errorHandler,
		// Fiber defaults to a 4MB body limit, which silently truncated the real
		// limits: the UI advertises PDFs up to 10MB and a design reference may be
		// up to 5MB, and anything over 4MB died as a reset connection instead of a
		// readable error. 16MB covers a 10MB document plus a 5MB image with room
		// for multipart overhead.
		BodyLimit: 16 * 1024 * 1024,
	})

	app.Use(logger.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: strings.Split(cfg.AllowedOrigins, ","),
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Authorization", "Content-Type"},
	}))

	return app
}

func errorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	// Both keys: the dashboard/client reads `message`, older callers read `error`.
	// Without `message` a 4xx/5xx reaches the UI as a bare status text.
	return c.Status(code).JSON(fiber.Map{
		"error":   err.Error(),
		"message": err.Error(),
	})
}
