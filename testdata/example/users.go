package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// ErrNotFound is returned when a user does not exist.
var ErrNotFound = errors.New("user not found")

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email,omitempty"`
}

type Store interface {
	Find(ctx context.Context, id int64) (*User, error)
	List(ctx context.Context) ([]*User, error)
}

type Server struct {
	store Store
}

// Get returns the user with the given id.
func (s *Server) Get(ctx context.Context, id int64) (*User, error) {
	if id <= 0 {
		return nil, ErrNotFound
	}
	u, err := s.store.Find(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find user %d: %w", id, err)
	}
	return u, nil
}

func (s *Server) Emails(ctx context.Context) ([]string, error) {
	users, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, u := range users {
		if u.Email != "" {
			out = append(out, u.Email)
		}
	}
	return out, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	u, err := s.Get(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := json.NewEncoder(w).Encode(u); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (u *User) FullName() string {
	return u.FirstName + " " + u.LastName
}
