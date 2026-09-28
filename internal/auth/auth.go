// Package auth implements header-based role authorization.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// Role is a caller role taken from the X-User-Role header.
type Role string

// Known roles. Anything else (or a missing header) is a read-only viewer.
const (
	RoleAdmin       Role = "admin"
	RoleContributor Role = "contributor"
	RoleViewer      Role = "viewer"
)

// Action is a permission checked by Require.
type Action string

// Actions.
const (
	ActionRead   Action = "read"
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

var permissions = map[Role]map[Action]bool{
	RoleAdmin:       {ActionRead: true, ActionCreate: true, ActionUpdate: true, ActionDelete: true},
	RoleContributor: {ActionRead: true, ActionCreate: true, ActionUpdate: true},
	RoleViewer:      {ActionRead: true},
}

type ctxKey int

const (
	roleKey ctxKey = iota
	userKey
)

// Middleware parses X-User-Role / X-User-ID and stores them in the context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := parseRole(r.Header.Get("X-User-Role"))
		user := strings.TrimSpace(r.Header.Get("X-User-ID"))
		if len(user) > 100 {
			user = user[:100]
		}
		if user == "" {
			user = "anonymous"
			if role != RoleViewer {
				user = string(role)
			}
		}
		ctx := context.WithValue(r.Context(), roleKey, role)
		ctx = context.WithValue(ctx, userKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Require rejects requests whose role lacks the permission with 403.
func Require(action Action) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !permissions[RoleFrom(r.Context())][action] {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "forbidden: your role is not permitted to " + string(action) + " disasters",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RoleFrom returns the caller's role (viewer if unset).
func RoleFrom(ctx context.Context) Role {
	if r, ok := ctx.Value(roleKey).(Role); ok {
		return r
	}
	return RoleViewer
}

// UserID returns the caller's identifier.
func UserID(ctx context.Context) string {
	if u, ok := ctx.Value(userKey).(string); ok {
		return u
	}
	return "anonymous"
}

func parseRole(h string) Role {
	switch Role(strings.ToLower(strings.TrimSpace(h))) {
	case RoleAdmin:
		return RoleAdmin
	case RoleContributor:
		return RoleContributor
	default:
		return RoleViewer
	}
}
