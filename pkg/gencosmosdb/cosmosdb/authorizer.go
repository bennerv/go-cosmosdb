package cosmosdb

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Authorizer interface {
	Authorize(context.Context, *http.Request, string, string) error
}

type masterKeyAuthorizer struct {
	masterKey []byte
}

func (a *masterKeyAuthorizer) Authorize(ctx context.Context, req *http.Request, resourceType, resourceLink string) error {
	date := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")

	h := hmac.New(sha256.New, a.masterKey)
	fmt.Fprintf(h, "%s\n%s\n%s\n%s\n\n", strings.ToLower(req.Method), resourceType, resourceLink, strings.ToLower(date))

	req.Header.Set("Authorization", url.QueryEscape(fmt.Sprintf("type=master&ver=1.0&sig=%s", base64.StdEncoding.EncodeToString(h.Sum(nil)))))
	req.Header.Set("x-ms-date", date)

	return nil
}

func NewMasterKeyAuthorizer(masterKey string) (Authorizer, error) {
	b, err := base64.StdEncoding.DecodeString(masterKey)
	if err != nil {
		return nil, err
	}

	return &masterKeyAuthorizer{masterKey: b}, nil
}

type tokenAuthorizer struct {
	token       string
	expiration  time.Time
	cond        *sync.Cond
	acquiring   bool
	lastAttempt time.Time
	getToken    func(context.Context) (token string, newExpiration time.Time, err error)
}

func (a *tokenAuthorizer) Authorize(ctx context.Context, req *http.Request, resourceType, resourceLink string) error {
	token, err := a.acquireToken(ctx)
	if err != nil {
		return err
	}
	authorizationHeader := fmt.Sprintf("type=aad&ver=1.0&sig=%s", token)
	currentTime := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
	req.Header.Set("Authorization", authorizationHeader)
	req.Header.Set("x-ms-date", currentTime)

	return nil
}

func NewTokenAuthorizer(token string, expiration time.Time, getToken func(context.Context) (token string, newExpiration time.Time, err error)) Authorizer {
	return &tokenAuthorizer{token: token, expiration: expiration, getToken: getToken, cond: sync.NewCond(&sync.Mutex{})}
}

// Get returns the underlying resource.
// If the resource is fresh, no refresh is performed.
func (a *tokenAuthorizer) acquireToken(ctx context.Context) (string, error) {
	// If the resource is expiring within this time window, update it eagerly.
	// This allows other goroutines to keep running by using the not-yet-expired
	// resource value while one goroutine updates the resource.
	const window = 5 * time.Minute   // To update the token if 5 mins left to expire
	const backoff = 30 * time.Second // Minimum wait time between attempts
	now, acquire, expired := time.Now(), false, false

	// acquire exclusive lock
	a.cond.L.Lock()
	token := a.token

	for {
		expired = a.expiration.IsZero() || a.expiration.Before(now)
		if expired {
			if !a.acquiring {
				// This goroutine will acquire the token.
				a.acquiring, acquire = true, true
				break
			}
			// Getting here means that this goroutine will wait for the updated token
		} else if a.expiration.Add(-window).Before(now) {
			// The token is valid but is expiring within the time window(5 mins)
			if !a.acquiring && a.lastAttempt.Add(backoff).Before(now) {
				// If another goroutine is not acquiring/renewing the token, and none has attempted
				// to do so within the last 30 seconds, this goroutine will do it
				a.acquiring, acquire = true, true
				break
			}
			// This goroutine will use the existing token value while another updates it
			token = a.token
			break
		} else {
			// The token is not close to expiring, this so using its current value
			token = a.token
			break
		}
		// If we get here, wait for the new token value to be acquired/updated
		a.cond.Wait()
	}
	a.cond.L.Unlock() // Release the lock so no goroutines are blocked

	var err error
	if acquire {
		// This goroutine has been selected to acquire/update the token
		var expiration time.Time
		var newValue string
		a.lastAttempt = now
		newValue, expiration, err = a.getToken(ctx)

		// Atomically, update the shared token's new value & expiration.
		a.cond.L.Lock()
		if err == nil {
			// Update token & expiration, return the new value
			token = newValue
			a.token, a.expiration = token, expiration
		} else if !expired {
			// An eager update failed. Discard the error and return the current--still valid--token value
			err = nil
		}
		a.acquiring = false
		a.cond.L.Unlock()
		a.cond.Broadcast()
	}
	return token, err
}
