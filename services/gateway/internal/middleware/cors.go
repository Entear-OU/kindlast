package middleware

import (
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/cors"
)

// CORSConfig holds configuration for CORS middleware
type CORSConfig struct {
	Origins     []string
	Environment string
}

// CORS returns a middleware that handles CORS headers
// It validates origins and prevents wildcard (*) usage in production
func CORS(cfg CORSConfig) func(next http.Handler) http.Handler {
	origins := validateAndSanitizeOrigins(cfg.Origins, cfg.Environment)

	return cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	})
}

// validateAndSanitizeOrigins validates CORS origins and prevents insecure configurations
// in production environments
func validateAndSanitizeOrigins(origins []string, environment string) []string {
	isProduction := environment == "production"
	validOrigins := make([]string, 0, len(origins))

	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}

		// Block wildcard (*) in production - this is a security vulnerability
		if origin == "*" {
			if isProduction {
				log.Printf("WARNING: Wildcard CORS origin (*) is not allowed in production. Skipping.")
				continue
			}
			log.Printf("WARNING: Wildcard CORS origin (*) detected in %s environment. This is insecure.", environment)
		}

		// Validate origin format (must be a valid URL scheme)
		if !strings.HasPrefix(origin, "http://") && !strings.HasPrefix(origin, "https://") && origin != "*" {
			log.Printf("WARNING: Invalid CORS origin format: %s. Origins must start with http:// or https://", origin)
			continue
		}

		// Warn about HTTP origins in production
		if isProduction && strings.HasPrefix(origin, "http://") && !strings.Contains(origin, "localhost") {
			log.Printf("WARNING: Non-HTTPS CORS origin in production: %s. Consider using HTTPS.", origin)
		}

		validOrigins = append(validOrigins, origin)
	}

	// Ensure at least one origin is configured
	if len(validOrigins) == 0 {
		if isProduction {
			log.Printf("ERROR: No valid CORS origins configured for production. Defaulting to empty list (no cross-origin requests allowed).")
			return []string{}
		}
		log.Printf("WARNING: No valid CORS origins configured. Defaulting to http://localhost:3000 for development.")
		return []string{"http://localhost:3000"}
	}

	return validOrigins
}
