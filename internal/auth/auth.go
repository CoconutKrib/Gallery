package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const cookieName = "gallery_session"

// Store is a simple in-memory session store.
type Store struct {
	mu       sync.Mutex
	sessions map[string]time.Time // token → expiry
	ttl      time.Duration
}

// NewStore creates a session store with the given TTL.
func NewStore(ttlHours int) *Store {
	if ttlHours <= 0 {
		ttlHours = 24
	}
	return &Store{
		sessions: make(map[string]time.Time),
		ttl:      time.Duration(ttlHours) * time.Hour,
	}
}

// Login verifies the password against the bcrypt hash. On success it creates a
// session token, sets the cookie, and returns true.
func (s *Store) Login(w http.ResponseWriter, r *http.Request, passwordHash, submitted string) bool {
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(submitted)); err != nil {
		return false
	}
	token := randomToken()
	expiry := time.Now().Add(s.ttl)
	s.mu.Lock()
	s.sessions[token] = expiry
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return true
}

// Logout clears the session cookie and removes the token from the store.
func (s *Store) Logout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(cookieName)
	if err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// Valid returns true if the request carries a valid, unexpired session cookie.
func (s *Store) Valid(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	s.mu.Lock()
	expiry, ok := s.sessions[c.Value]
	s.mu.Unlock()
	return ok && time.Now().Before(expiry)
}

// HashPassword returns the bcrypt hash of the given password.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("auth: random token generation failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
