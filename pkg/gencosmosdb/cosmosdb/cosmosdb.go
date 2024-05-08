package cosmosdb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strconv"
	"time"

	"github.com/ugorji/go/codec"
)

// Options represents API options
type Options struct {
	NoETag              bool
	PreTriggers         []string
	PostTriggers        []string
	PartitionKeyRangeID string
	Continuation        string
}

// Error represents an error
type Error struct {
	StatusCode int
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d %s: %s", e.StatusCode, e.Code, e.Message)
}

// IsErrorStatusCode returns true if err is of type Error and its StatusCode
// matches statusCode
func IsErrorStatusCode(err error, statusCode int) bool {
	if err, ok := err.(*Error); ok {
		return err.StatusCode == statusCode
	}
	return false
}

// ErrETagRequired is the error returned if the ETag field is not populate on a
// PUT or DELETE operation
var ErrETagRequired = fmt.Errorf("ETag is required")

// ErrNotImplemented is the error returned if a fake function is not implemented
var ErrNotImplemented = fmt.Errorf("not implemented")

// RetryOnPreconditionFailed retries a function if it fails due to
// PreconditionFailed
func RetryOnPreconditionFailed(f func() error) (err error) {
	for i := 0; i < 5; i++ {
		err = f()
		if !IsErrorStatusCode(err, http.StatusPreconditionFailed) {
			return
		}
		time.Sleep(time.Duration(100*i) * time.Millisecond)
	}
	return
}

func (c *databaseClient) do(ctx context.Context, method, path, resourceType, resourceLink string, expectedStatusCode int, in, out interface{}, headers http.Header) error {
	var resp *http.Response
	var err error

	for retry := 0; retry < c.maxRetries; retry++ {
		resp, err = c._do(ctx, method, path, resourceType, resourceLink, expectedStatusCode, in, out, headers)
		if !IsErrorStatusCode(err, http.StatusTooManyRequests) {
			break
		}

		c.log.Warnf("%s %s: attempt %d: %s", method, path, retry, err)

		ms, err2 := strconv.ParseInt(resp.Header.Get("x-ms-retry-after-ms"), 10, 0)
		if err2 != nil {
			return err2
		}

		time.Sleep(time.Duration(ms) * time.Millisecond)
	}

	if resp != nil && headers != nil {
		for k := range headers {
			delete(headers, k)
		}
		for k, v := range resp.Header {
			headers[k] = v
		}
	}

	return err
}

func (c *databaseClient) _do(ctx context.Context, method, path, resourceType, resourceLink string, expectedStatusCode int, in, out interface{}, headers http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, "https://"+c.databaseHostname+"/"+path, nil)
	if err != nil {
		return nil, err
	}

	if in != nil {
		buf := &bytes.Buffer{}
		err := codec.NewEncoder(buf, c.jsonHandle).Encode(in)
		if err != nil {
			return nil, err
		}
		req.Body = io.NopCloser(buf)
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range headers {
		req.Header[textproto.CanonicalMIMEHeaderKey(k)] = v
	}

	req.Header.Set("x-ms-version", "2018-12-31")

	if c.authorizer != nil {
		err := c.authorizer.Authorize(req, resourceType, resourceLink)
		if err != nil {
			return nil, err
		}
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		resp.Body.Read(nil)
		resp.Body.Close()
	}()

	d := codec.NewDecoder(resp.Body, c.jsonHandle)

	if resp.StatusCode != expectedStatusCode {
		err := &Error{}
		if resp.Header.Get("Content-Type") == "application/json" {
			d.Decode(&err)
		}
		err.StatusCode = resp.StatusCode
		return resp, err
	}

	if out != nil && resp.Header.Get("Content-Type") == "application/json" {
		return resp, d.Decode(&out)
	}

	return resp, nil
}
