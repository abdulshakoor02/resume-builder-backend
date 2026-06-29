package store

import (
	"github.com/gofiber/fiber/v3"
)

func RequireAuth(c fiber.Ctx) error {
	_, ok := c.Locals("user_id").(string)
	if !ok {
		return fiber.NewError(fiber.StatusUnauthorized, "authentication required")
	}
	return nil
}
