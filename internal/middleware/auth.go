package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Use the same secret key you used when generating tokens during login
var jwtSecret = []byte("your-super-secret-jwt-key-change-in-production")

// AuthMiddleware acts as the security bouncer
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Check if the Authorization header exists
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing authorization token", http.StatusUnauthorized)
			return
		}

		// 2. Expecting format: "Bearer <token_string>"
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Invalid authorization format. Use 'Bearer <token>'", http.StatusUnauthorized)
			return
		}

		tokenStr := parts[1]
		claims := jwt.MapClaims{}

		// 3. Parse and validate the JWT token
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		userID, ok := claims["user_id"]
		if !ok {
			http.Error(w, "Invalid token claims", http.StatusUnauthorized)
			return
		}

		// Attach userID and email to the request's context
		ctx := context.WithValue(r.Context(), "userID", userID)
		ctx = context.WithValue(ctx, "userEmail", claims["email"])
		
		// Pass the request down with the new context attached
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func OptionalAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			// No token provided? Proceed anonymously (user_id will be nil)
			next.ServeHTTP(w, r)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			// Bad format, but since auth is optional, we can choose to fail or pass.
			// Failing with 401 is safer if they tried to send a malformed token.
			http.Error(w, "Invalid authorization format", http.StatusUnauthorized)
			return
		}

		tokenStr := parts[1]
		claims := jwt.MapClaims{}

		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtSecret, nil
		})

		if err == nil && token.Valid {
			// If token is valid, inject the userID into the context
			if userID, ok := claims["user_id"]; ok {
				ctx := context.WithValue(r.Context(), "userID", userID)
				r = r.WithContext(ctx)
			}
		}

		// Proceed to the handler whether authenticated or not
		next.ServeHTTP(w, r)
	}
}
