// Copyright 2022 Stock Parfait

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package fetch implements retriable remote HTTP requests.
package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/stockparfait/errors"
)

type clientKeyType string

const clientKey = clientKeyType("client")

// UseClient adds the HTTP client to the context to be used instead of the
// default. This is used primarily in tests.
func UseClient(ctx context.Context, client *http.Client) context.Context {
	return context.WithValue(ctx, clientKey, client)
}

// GetClient retrieves an HTTP client from the context. Returns nil if not present.
func GetClient(ctx context.Context) *http.Client {
	client, ok := ctx.Value(clientKey).(*http.Client)
	if client == nil || !ok {
		return nil
	}
	return client
}

// RetriableError signals a transient failure that can be retried.
type RetriableError struct {
	Err error
}

func (e *RetriableError) Error() string {
	return e.Err.Error()
}

// NewRetriableError makes the given error retriable.
func NewRetriableError(e error) *RetriableError {
	return &RetriableError{e}
}

// Params defines the retry policy.
type Params struct {
	NumRetries   int
	RetryMinWait time.Duration    // time to wait before the first retry
	RetryMaxWait time.Duration    // exponential backoff caps at this value
	IsRetriable  func(error) bool // only retry when fn(err) == true
}

// NewParams creates the default value of Params.
func NewParams() *Params {
	return &Params{
		NumRetries:   3, // number of retries; when 0, `fn` is called only once
		RetryMinWait: time.Second,
		RetryMaxWait: time.Minute,
		IsRetriable: func(e error) bool {
			_, ok := e.(*RetriableError)
			return ok
		},
	}
}

// Retries sets `NumRetries` parameter.
func (p *Params) Retries(r int) *Params {
	p.NumRetries = r
	return p
}

// MinWait sets `RetryMinWait` parameter.
func (p *Params) MinWait(d time.Duration) *Params {
	p.RetryMinWait = d
	return p
}

// MaxWait sets `RetryMaxWait` parameter.
func (p *Params) MaxWait(d time.Duration) *Params {
	p.RetryMaxWait = d
	return p
}

// IsRetriableFn sets `IsRetriable` parameter.
func (p *Params) IsRetriableFn(f func(e error) bool) *Params {
	p.IsRetriable = f
	return p
}

// Retriable is a callback function which, if fails with a RetriableError, can
// be retried.
type Retriable func(attempt int) error

// Retry calls `fn` and retries it if it returns a retriable error, and returns
// the last error from `fn`, or from the context if it closes. Retry blocks
// until all the retries finish. This method is context-aware; it will stop
// retrying when the context is canceled. In particular, if it is called with a
// canceled context, it will not run `fn` at all.
func Retry(ctx context.Context, params *Params, fn Retriable) error {
	wait := params.RetryMinWait
	var err error
	for i := 0; i <= params.NumRetries; i++ {
		select {
		case <-ctx.Done():
			return errors.Annotate(ctx.Err(), "context is canceled")
		default:
		}
		err = fn(i)
		if err == nil {
			return nil
		}
		if !params.IsRetriable(err) {
			return errors.Annotate(err, "error cannot be retried")
		}
		time.Sleep(wait)
		wait = 2 * wait
		if wait > params.RetryMaxWait {
			wait = params.RetryMaxWait
		}
	}
	return errors.Annotate(err, "exhausted %d retries", params.NumRetries)
}

// ResponseOK returns true if the response is successful (code 2xx).
func ResponseOK(r *http.Response) bool {
	return 200 <= r.StatusCode && r.StatusCode <= 299
}

// ResponseRetriable checks if an unsuccessful response can be
// retried. Normally, these are 5xx codes.
func ResponseRetriable(r *http.Response) bool {
	return 500 <= r.StatusCode && r.StatusCode <= 599
}

// Get sends a "GET" request using the `uri` with optional query parameters. It
// returns a non-nil error if the request completes with a code outside of 2xx.
// For retriable status codes (5xx) the error is RetriableError, making it
// compatible with `Retry()`.
func Get(ctx context.Context, uri string, query url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", uri, nil)
	if err != nil {
		return nil, errors.Annotate(err, "failed to create HTTP request")
	}
	// Tests will supply an httptest client in ctx.
	client := http.DefaultClient
	if c := GetClient(ctx); c != nil {
		client = c
	}
	if query != nil {
		req.URL.RawQuery = query.Encode()
	}
	resp, err := client.Get(req.URL.String())
	if err != nil {
		return resp, errors.Annotate(err, "failed to GET URL")
	}
	if ResponseOK(resp) {
		return resp, nil
	}
	if ResponseRetriable(resp) {
		return resp, NewRetriableError(err)
	}
	// The body of the response may have additional info, add it to the error.
	body := bytes.NewBuffer(nil)
	body.ReadFrom(resp.Body)
	return resp, errors.Reason(
		"url: %s, response code %s, body: %s", uri, resp.Status, body.String())
}

// GetRetry is like Get that retries transient failures.
func GetRetry(ctx context.Context, uri string, query url.Values, params *Params) (resp *http.Response, err error) {
	if params == nil {
		params = NewParams()
	}
	err = Retry(ctx, params, func(i int) (err error) {
		resp, err = Get(ctx, uri, query)
		return
	})
	return
}

// FetchJSON fetches a JSON blob from uri using GetRetry and unpacks it into
// result.
func FetchJSON(ctx context.Context, uri string, result interface{}, query url.Values, params *Params) error {
	resp, err := GetRetry(ctx, uri, query, params)
	if err != nil {
		return errors.Annotate(err, "failed to get JSON data")
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Annotate(err, "failed to read response body")
	}
	if err := json.Unmarshal(data, result); err != nil {
		return errors.Annotate(err, "failed to unmarshal JSON")
	}
	return nil
}
