package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/middleware/timeout"
)

func TestTimeoutReturnsStructuredError(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: ErrorHandler,
	})

	app.Use(requestid.New())

	slowHandler := func(c *fiber.Ctx) error {
		select {
		case <-time.After(100 * time.Millisecond):
			return c.SendStatus(fiber.StatusOK)

		case <-c.UserContext().Done():
			return c.UserContext().Err()
		}
	}

	app.Get(
		"/timeout",
		timeout.NewWithContext(slowHandler, 10*time.Millisecond),
	)

	request := httptest.NewRequest(
		fiber.MethodGet,
		"/timeout",
		nil,
	)

	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("app.Test returned an error: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != fiber.StatusRequestTimeout {
		t.Errorf(
			"expected status %d, got %d",
			fiber.StatusRequestTimeout,
			response.StatusCode,
		)
	}

	var responseBody ChatResponse

	if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}

	if responseBody.RequestID == "" {
		t.Error("expected request_id to be present")
	}

	if responseBody.Error == nil {
		t.Fatal("expected an error response")
	}

	if responseBody.Error.Code != "REQUEST_TIMEOUT" {
		t.Errorf(
			"expected REQUEST_TIMEOUT, got %s",
			responseBody.Error.Code,
		)
	}
}
