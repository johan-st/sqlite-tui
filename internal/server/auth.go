package server

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/charmbracelet/ssh"
	"github.com/johan-st/sqlite-tui/internal/access"
	"github.com/johan-st/sqlite-tui/internal/config"
	"github.com/johan-st/sqlite-tui/internal/history"
	gossh "golang.org/x/crypto/ssh"
)

// Authenticator handles SSH authentication.
type Authenticator struct {
	config       *config.Config
	historyStore *history.Store
}

// NewAuthenticator creates a new authenticator.
func NewAuthenticator(cfg *config.Config, historyStore *history.Store) *Authenticator {
	return &Authenticator{
		config:       cfg,
		historyStore: historyStore,
	}
}

// PublicKeyHandler returns a handler for public key authentication.
func (a *Authenticator) PublicKeyHandler() ssh.PublicKeyHandler {
	return func(ctx ssh.Context, key ssh.PublicKey) bool {
		fingerprint := FingerprintKey(key)
		user := a.findUserByKey(fingerprint, key)

		if user != nil {
			// Store user info in context
			ctx.SetValue("user", user)
			log.Printf("Authenticated user %s from %s", user.Name, ctx.RemoteAddr())
			return true
		}

		// Allow anonymous access if configured
		if a.config.AllowKeyless || a.config.AnonymousAccess != "none" {
			// Create anonymous user info
			anonName := a.historyStore.GenerateAnonymousName()
			anonUser := &access.UserInfo{
				IsAnonymous:   true,
				AnonymousName: anonName,
				PublicKeyFP:   fingerprint,
				RemoteAddr:    ctx.RemoteAddr().String(),
			}
			ctx.SetValue("user", anonUser)
			log.Printf("Anonymous access from %s as %s", ctx.RemoteAddr(), anonName)
			return true
		}

		log.Printf("Authentication failed for key %s from %s", fingerprint, ctx.RemoteAddr())
		return false
	}
}

// KeyboardInteractiveHandler returns a handler for keyboard-interactive auth.
func (a *Authenticator) KeyboardInteractiveHandler() ssh.KeyboardInteractiveHandler {
	if !a.config.AllowKeyless {
		return nil
	}

	return func(ctx ssh.Context, challenger gossh.KeyboardInteractiveChallenge) bool {
		// Allow anonymous access
		anonName := a.historyStore.GenerateAnonymousName()
		anonUser := &access.UserInfo{
			IsAnonymous:   true,
			AnonymousName: anonName,
			RemoteAddr:    ctx.RemoteAddr().String(),
		}
		ctx.SetValue("user", anonUser)
		log.Printf("Anonymous keyboard-interactive access from %s as %s", ctx.RemoteAddr(), anonName)
		return true
	}
}

// findUserByKey finds a user by their public key.
func (a *Authenticator) findUserByKey(fingerprint string, key ssh.PublicKey) *access.UserInfo {
	for _, user := range a.config.Users {
		for _, pubKeyStr := range user.PublicKeys {
			// Parse the authorized key
			parsedKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubKeyStr))
			if err != nil {
				// Try comparing as raw fingerprint
				if strings.Contains(pubKeyStr, fingerprint) {
					return &access.UserInfo{
						Name:        user.Name,
						IsAdmin:     user.Admin,
						PublicKeyFP: fingerprint,
					}
				}
				continue
			}

			// Compare keys
			if ssh.KeysEqual(parsedKey, key) {
				return &access.UserInfo{
					Name:        user.Name,
					IsAdmin:     user.Admin,
					PublicKeyFP: fingerprint,
				}
			}
		}
	}
	return nil
}

// GetUserFromContext retrieves user info from the SSH context.
func GetUserFromContext(ctx ssh.Context) *access.UserInfo {
	if user, ok := ctx.Value("user").(*access.UserInfo); ok {
		return user
	}
	return nil
}

// FingerprintKey returns the SHA256 fingerprint of a public key.
func FingerprintKey(key ssh.PublicKey) string {
	hash := sha256.Sum256(key.Marshal())
	return fmt.Sprintf("SHA256:%s", base64.StdEncoding.EncodeToString(hash[:]))
}

// FingerprintKeyShort returns a shortened fingerprint for display.
func FingerprintKeyShort(key ssh.PublicKey) string {
	fp := FingerprintKey(key)
	if len(fp) > 20 {
		return fp[:20] + "..."
	}
	return fp
}
