package entry_test

import (
	"testing"
)

// TestFind_EchoAndGin proves every framework adapter's registration is detected with
// its method and route.
func TestFind_EchoAndGin(t *testing.T) {
	cases := []struct {
		fixture   string
		framework string
	}{
		{"echo_app", "github.com/labstack/echo/v4"},
		{"echo5_app", "github.com/labstack/echo/v5"},
		{"gin_app", "github.com/gin-gonic/gin"},
	}
	for _, tc := range cases {
		points := loadFixtures(t, tc.fixture)
		if len(points) != 2 {
			t.Fatalf("%s: found %d points, want 2: %+v", tc.fixture, len(points), points)
		}
		routes := map[string]string{}
		for _, point := range points {
			if point.Framework != tc.framework {
				t.Errorf("%s: framework = %q, want %q", tc.fixture, point.Framework, tc.framework)
			}
			if point.Function != "main" {
				t.Errorf("%s: function = %q, want main for a literal", tc.fixture, point.Function)
			}
			routes[point.Route] = point.Method
		}
		if routes["/orders/:id"] != "GET" || routes["/pay/:id"] != "POST" {
			t.Errorf("%s: routes = %v, want GET /orders/:id and POST /pay/:id", tc.fixture, routes)
		}
	}
}
