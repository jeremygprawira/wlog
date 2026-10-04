// Command app is an init fixture with a Fiber v3 router.
package main

import "github.com/gofiber/fiber/v3"

func main() {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error { return c.SendStatus(200) })
	_ = app.Listen(":8080")
}
