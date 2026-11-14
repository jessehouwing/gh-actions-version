package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimitKind classifies a rate limit response.
type RateLimitKind string

const (
	RateLimitPrimary   RateLimitKind = "primary"
	RateLimitSecondary RateLimitKind = "secondary"
	RateLimitGraphQL   RateLimitKind = "graphql"
)

// ErrRateLimited matches every *RateLimitError via errors.Is.
var ErrRateLimited = errors.New("rate limited")

// RateLimitError is returned when a rate limit cannot be waited out (wait above MaxWait or
// attempts exhausted). Callers should map it to exit code 2.
type RateLimitError struct {
	Kind      RateLimitKind
	Resource  string
	Limit     int
	Remaining int
	ResetAt   time.Time     // zero when unknown
	Wait      time.Duration // wait that would have been required
	MaxWait   time.Duration
	Anonymous bool
	URL       string
}

func (e *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

func (e *RateLimitError) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "GitHub %s rate limit hit", e.Kind)
	if e.Resource != "" {
		fmt.Fprintf(&sb, " (resource %s", e.Resource)
		if e.Limit > 0 {
			fmt.Fprintf(&sb, ", limit %d", e.Limit)
		}
		sb.WriteString(")")
	}
	if !e.ResetAt.IsZero() {
		fmt.Fprintf(&sb, "; resets at %s", e.ResetAt.UTC().Format(time.RFC3339))
	}
	if e.Wait > 0 {
		fmt.Fprintf(&sb, "; would need to wait %s", e.Wait.Round(time.Second))
		if e.MaxWait > 0 {
			fmt.Fprintf(&sb, " (max wait %s, see --max-wait)", e.MaxWait)
		}
	}
	if e.Anonymous {
		sb.WriteString("; the request was unauthenticated (60 requests/hour) - run 'gh auth login' or set GH_TOKEN")
	}
	return sb.String()
}

// TransportOptions configure the rate limit aware transport. Zero values select defaults.
type TransportOptions struct {
	Base        http.RoundTripper
	MaxWait     time.Duration // longest single wait tolerated; default 60s
	MaxAttempts int           // attempts per request; default 3
	// WritePacing is the minimum delay between mutating requests; default 1s. Use a negative
	// value to disable.
	WritePacing time.Duration
	// SecondaryBackoff is the first wait for a secondary limit without Retry-After; default 60s.
	SecondaryBackoff time.Duration
	// TransientBackoff is the first wait before retrying an idempotent request after a 5xx or
	// network error; default 1s (doubles).
	TransientBackoff time.Duration
	Now              func() time.Time
	Sleep            func(ctx context.Context, d time.Duration) error
	Jitter           func(max time.Duration) time.Duration // default random up to max
	Warn             func(msg string)                      // low-quota and wait notices
	OnWait           func(kind RateLimitKind, d time.Duration)
}

// Transport is an http.RoundTripper adding rate limit handling, ETag revalidation and write
// pacing around Base.
type Transport struct {
	opts TransportOptions

	writeMu   sync.Mutex
	lastWrite time.Time

	mu     sync.Mutex
	etags  map[string]*cacheEntry
	warned map[string]bool
	Stats  TransportStats
}

// TransportStats counts what the transport did (for tests and --verbose output).
type TransportStats struct {
	Requests, Retries, NotModified, Waits int
	WaitedTotal                           time.Duration
}

type cacheEntry struct {
	etag   string
	body   []byte
	header http.Header
	status int
}

// NewTransport builds a Transport.
func NewTransport(o TransportOptions) *Transport {
	if o.Base == nil {
		o.Base = http.DefaultTransport
	}
	if o.MaxWait == 0 {
		o.MaxWait = 60 * time.Second
	}
	if o.MaxAttempts == 0 {
		o.MaxAttempts = 3
	}
	if o.WritePacing == 0 {
		o.WritePacing = time.Second
	}
	if o.SecondaryBackoff == 0 {
		o.SecondaryBackoff = 60 * time.Second
	}
	if o.TransientBackoff == 0 {
		o.TransientBackoff = time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	if o.Jitter == nil {
		o.Jitter = func(max time.Duration) time.Duration {
			if max <= 0 {
				return 0
			}
			return time.Duration(rand.Int63n(int64(max)))
		}
	}
	return &Transport{opts: o, etags: map[string]*cacheEntry{}, warned: map[string]bool{}}
}

func isWrite(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return !isGraphQLQuery(req)
}

func isGraphQLPath(req *http.Request) bool {
	return req.Method == http.MethodPost && strings.HasSuffix(strings.TrimRight(req.URL.Path, "/"), "/graphql")
}

// isGraphQLQuery is true for GraphQL requests that do not contain a mutation.
func isGraphQLQuery(req *http.Request) bool {
	if !isGraphQLPath(req) || req.GetBody == nil {
		return false
	}
	rc, err := req.GetBody()
	if err != nil {
		return false
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return !bytes.Contains(bytes.ToLower(b), []byte("mutation"))
}

// idempotent requests may be retried after ambiguous failures (5xx, network errors).
func idempotent(req *http.Request) bool { return !isWrite(req) }

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	write := isWrite(req)
	if write {
		t.writeMu.Lock()
		defer t.writeMu.Unlock()
		if err := t.pace(ctx); err != nil {
			return nil, err
		}
		defer func() { t.lastWrite = t.opts.Now() }()
	}

	for attempt := 1; ; attempt++ {
		r, err := cloneRequest(req)
		if err != nil {
			return nil, err
		}
		t.mu.Lock()
		t.Stats.Requests++
		if attempt > 1 {
			t.Stats.Retries++
		}
		var cached *cacheEntry
		if req.Method == http.MethodGet {
			cached = t.etags[req.URL.String()]
		}
		t.mu.Unlock()
		if cached != nil && r.Header.Get("If-None-Match") == "" {
			r.Header.Set("If-None-Match", cached.etag)
		}

		resp, err := t.opts.Base.RoundTrip(r)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !idempotent(req) || attempt >= t.opts.MaxAttempts {
				return nil, err
			}
			if serr := t.wait(ctx, RateLimitKind(""), t.backoff(attempt, t.opts.TransientBackoff), false); serr != nil {
				return nil, serr
			}
			continue
		}
		t.observe(req, resp)

		var body []byte
		needBody := resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests ||
			(isGraphQLPath(req) && resp.StatusCode == http.StatusOK)
		if needBody {
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return nil, err
			}
			resp.Body = io.NopCloser(bytes.NewReader(body))
		}

		kind, wait, rle := t.classify(req, resp, body, attempt)
		switch {
		case kind != "":
			if rle != nil || attempt >= t.opts.MaxAttempts {
				if rle == nil {
					rle = t.limitError(req, resp, kind, wait)
				}
				resp.Body.Close()
				return nil, rle
			}
			resp.Body.Close()
			if err := t.wait(ctx, kind, wait, true); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode >= 500 && idempotent(req) && attempt < t.opts.MaxAttempts:
			resp.Body.Close()
			if err := t.wait(ctx, "", t.backoff(attempt, t.opts.TransientBackoff), false); err != nil {
				return nil, err
			}
			continue
		}

		return t.cache(req, resp, cached)
	}
}

func (t *Transport) pace(ctx context.Context) error {
	if t.opts.WritePacing < 0 || t.lastWrite.IsZero() {
		return nil
	}
	if d := t.opts.WritePacing - t.opts.Now().Sub(t.lastWrite); d > 0 {
		return t.opts.Sleep(ctx, d)
	}
	return nil
}

func (t *Transport) backoff(attempt int, base time.Duration) time.Duration {
	d := base
	for i := 1; i < attempt; i++ {
		d *= 2
	}
	return d
}

func (t *Transport) wait(ctx context.Context, kind RateLimitKind, d time.Duration, limit bool) error {
	if d < 0 {
		d = 0
	}
	if limit && t.opts.Warn != nil {
		t.opts.Warn(fmt.Sprintf("GitHub %s rate limit hit; waiting %s", kind, d.Round(time.Second)))
	}
	if t.opts.OnWait != nil {
		t.opts.OnWait(kind, d)
	}
	t.mu.Lock()
	t.Stats.Waits++
	t.Stats.WaitedTotal += d
	t.mu.Unlock()
	return t.opts.Sleep(ctx, d)
}

// classify decides whether resp is a rate limit. It returns the kind and the wait needed, and a
// non-nil error when the wait is not acceptable.
func (t *Transport) classify(req *http.Request, resp *http.Response, body []byte, attempt int) (RateLimitKind, time.Duration, *RateLimitError) {
	now := t.opts.Now()
	h := resp.Header
	remaining, hasRemaining := headerInt(h, "X-RateLimit-Remaining")
	reset := time.Time{}
	if v, ok := headerInt(h, "X-RateLimit-Reset"); ok {
		reset = time.Unix(int64(v), 0)
	}
	retryAfter := time.Duration(0)
	hasRetryAfter := false
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			retryAfter, hasRetryAfter = time.Duration(secs)*time.Second, true
		} else if when, err := http.ParseTime(v); err == nil {
			retryAfter, hasRetryAfter = when.Sub(now), true
		}
	}

	var kind RateLimitKind
	switch {
	case isGraphQLPath(req) && resp.StatusCode == http.StatusOK && graphqlRateLimited(body):
		kind = RateLimitGraphQL
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden:
		msg := strings.ToLower(string(body))
		switch {
		case hasRemaining && remaining == 0:
			kind = RateLimitPrimary
		case hasRetryAfter, strings.Contains(msg, "secondary rate limit"), strings.Contains(msg, "abuse detection"):
			kind = RateLimitSecondary
		case strings.Contains(msg, "api rate limit exceeded"):
			kind = RateLimitPrimary
		case resp.StatusCode == http.StatusTooManyRequests:
			kind = RateLimitSecondary
		default:
			return "", 0, nil // plain permission failure
		}
	default:
		return "", 0, nil
	}

	var wait time.Duration
	switch {
	case hasRetryAfter:
		wait = retryAfter
	case kind == RateLimitPrimary || kind == RateLimitGraphQL:
		if !reset.IsZero() {
			wait = reset.Sub(now) + t.opts.Jitter(time.Second)
		} else if kind == RateLimitGraphQL {
			wait = t.backoff(attempt, t.opts.SecondaryBackoff)
		} else {
			wait = t.opts.SecondaryBackoff
		}
	default:
		wait = t.backoff(attempt, t.opts.SecondaryBackoff)
	}
	if wait < 0 {
		wait = 0
	}
	if wait > t.opts.MaxWait {
		return kind, wait, t.limitError(req, resp, kind, wait)
	}
	return kind, wait, nil
}

func (t *Transport) limitError(req *http.Request, resp *http.Response, kind RateLimitKind, wait time.Duration) *RateLimitError {
	h := resp.Header
	e := &RateLimitError{Kind: kind, Resource: h.Get("X-RateLimit-Resource"), Wait: wait, MaxWait: t.opts.MaxWait, URL: req.URL.String()}
	e.Limit, _ = headerInt(h, "X-RateLimit-Limit")
	e.Remaining, _ = headerInt(h, "X-RateLimit-Remaining")
	if v, ok := headerInt(h, "X-RateLimit-Reset"); ok {
		e.ResetAt = time.Unix(int64(v), 0)
	}
	e.Anonymous = req.Header.Get("Authorization") == "" && e.Limit > 0 && e.Limit <= 60
	return e
}

// observe warns once per resource when less than 10% of the quota is left.
func (t *Transport) observe(req *http.Request, resp *http.Response) {
	if t.opts.Warn == nil {
		return
	}
	limit, ok1 := headerInt(resp.Header, "X-RateLimit-Limit")
	rem, ok2 := headerInt(resp.Header, "X-RateLimit-Remaining")
	if !ok1 || !ok2 || limit <= 0 || rem*10 >= limit {
		return
	}
	res := resp.Header.Get("X-RateLimit-Resource")
	t.mu.Lock()
	seen := t.warned[res]
	t.warned[res] = true
	t.mu.Unlock()
	if seen {
		return
	}
	msg := fmt.Sprintf("GitHub API rate limit low: %d of %d requests left for %q", rem, limit, res)
	if v, ok := headerInt(resp.Header, "X-RateLimit-Reset"); ok {
		msg += ", resets at " + time.Unix(int64(v), 0).UTC().Format(time.RFC3339)
	}
	if limit <= 60 {
		msg += " (unauthenticated: 60 requests/hour)"
	}
	t.opts.Warn(msg)
}

func (t *Transport) cache(req *http.Request, resp *http.Response, cached *cacheEntry) (*http.Response, error) {
	if req.Method != http.MethodGet {
		return resp, nil
	}
	key := req.URL.String()
	if resp.StatusCode == http.StatusNotModified && cached != nil {
		resp.Body.Close()
		t.mu.Lock()
		t.Stats.NotModified++
		t.mu.Unlock()
		hdr := cached.header.Clone()
		for _, k := range []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "X-RateLimit-Used", "X-RateLimit-Resource"} {
			if v := resp.Header.Get(k); v != "" {
				hdr.Set(k, v)
			}
		}
		hdr.Set("X-Cache", "etag")
		return &http.Response{
			Status: "200 OK", StatusCode: cached.status, Proto: resp.Proto, ProtoMajor: resp.ProtoMajor, ProtoMinor: resp.ProtoMinor,
			Header: hdr, Body: io.NopCloser(bytes.NewReader(cached.body)), ContentLength: int64(len(cached.body)), Request: req,
		}, nil
	}
	etag := resp.Header.Get("ETag")
	if resp.StatusCode == http.StatusOK && etag != "" {
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(b))
		t.mu.Lock()
		t.etags[key] = &cacheEntry{etag: etag, body: b, header: resp.Header.Clone(), status: resp.StatusCode}
		t.mu.Unlock()
	}
	return resp, nil
}

func cloneRequest(req *http.Request) (*http.Request, error) {
	r := req.Clone(req.Context())
	if req.Body != nil && req.GetBody != nil {
		b, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		r.Body = b
	}
	return r, nil
}

func headerInt(h http.Header, k string) (int, bool) {
	v := h.Get(k)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

func graphqlRateLimited(body []byte) bool {
	if !bytes.Contains(body, []byte("RATE_LIMIT")) {
		return false
	}
	var r struct {
		Errors []struct {
			Type string `json:"type"`
			Code string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &r) != nil {
		return false
	}
	for _, e := range r.Errors {
		if e.Type == "RATE_LIMITED" || e.Code == "RATE_LIMITED" {
			return true
		}
	}
	return false
}
