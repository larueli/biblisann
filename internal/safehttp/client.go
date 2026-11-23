package safehttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// Defaults applied when an Option is left at its zero value.
const (
	DefaultTimeout      = 15 * time.Second
	DefaultMaxBodyBytes = 5 << 20
	DefaultMaxRedirects = 3
)

// ErrTooLarge is returned when a response body exceeds the configured limit.
var ErrTooLarge = errors.New("safehttp: response body too large")

// Options configure a Client. Zero values select the safe defaults.
type Options struct {
	// Timeout bounds the whole request, including redirects and reading the body.
	Timeout time.Duration
	// MaxBodyBytes bounds the response body read through Response.Body.
	MaxBodyBytes int64
	// MaxRedirects bounds the redirect chain. Each hop is validated again.
	MaxRedirects int
	// UserAgent is sent on every request.
	UserAgent string
}

// Client performs outbound HTTP requests with SSRF protection.
type Client struct {
	http         *http.Client
	maxBodyBytes int64
	userAgent    string
}

// New returns a Client. Only http and https URLs to public addresses are allowed.
func New(opts Options) *Client {
	return newClient(opts, isPublic)
}

// newClient lets tests allow loopback destinations; production code always uses isPublic.
func newClient(opts Options, allowed func(netip.Addr) bool) *Client {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if opts.MaxRedirects <= 0 {
		opts.MaxRedirects = DefaultMaxRedirects
	}
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		// Control runs after DNS resolution, right before connect, on the real destination.
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !allowed(ap.Addr()) {
				return ErrBlockedAddress
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                  nil, // never honor HTTP(S)_PROXY: it would bypass the dial check
		DialContext:            dialer.DialContext,
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  opts.Timeout,
		MaxIdleConns:           20,
		IdleConnTimeout:        30 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	}
	maxRedirects := opts.MaxRedirects
	return &Client{
		http: &http.Client{
			Transport: transport,
			Timeout:   opts.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > maxRedirects {
					return fmt.Errorf("safehttp: stopped after %d redirects", maxRedirects)
				}
				return checkURL(req.URL)
			},
		},
		maxBodyBytes: opts.MaxBodyBytes,
		userAgent:    opts.UserAgent,
	}
}

func checkURL(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("safehttp: scheme %q is not allowed", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("safehttp: URL has no host")
	}
	if u.User != nil {
		return errors.New("safehttp: credentials in URL are not allowed")
	}
	return nil
}

// Std returns an *http.Client with the same protections (address check at connect time, redirect
// validation, timeout) for libraries that insist on a standard client, such as Web Push. Unlike
// Do it does not bound the response body.
func (c *Client) Std() *http.Client { return c.http }

// Response is an HTTP response whose body is size-limited. Close it when done.
type Response struct {
	Status int
	Header http.Header
	Body   io.ReadCloser
}

// Do sends req. The caller must close Response.Body. Reading past the body limit returns
// ErrTooLarge.
func (c *Client) Do(req *http.Request) (*Response, error) {
	if err := checkURL(req.URL); err != nil {
		return nil, err
	}
	if c.userAgent != "" && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	resp, err := c.http.Do(req) //nolint:bodyclose // closed by the caller through Response.Body
	if err != nil {
		return nil, fmt.Errorf("safehttp: %s %s: %w", req.Method, safeURL(req.URL), unwrapURLError(err))
	}
	return &Response{
		Status: resp.StatusCode,
		Header: resp.Header,
		Body:   &limitedBody{rc: resp.Body, remaining: c.maxBodyBytes},
	}, nil
}

// Get is a convenience for a GET request.
func (c *Client) Get(ctx context.Context, rawURL string) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("safehttp: invalid URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("safehttp: %w", err)
	}
	return c.Do(req)
}

// safeURL drops credentials, query and fragment: query strings often carry API keys.
func safeURL(u *url.URL) string {
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}

// unwrapURLError drops the *url.Error wrapper, which repeats the URL (possibly with a token).
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

type limitedBody struct {
	rc        io.ReadCloser
	remaining int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		// Distinguish "exactly at the limit" from "more data than allowed".
		var one [1]byte
		n, err := b.rc.Read(one[:])
		if n > 0 {
			return 0, ErrTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.rc.Read(p)
	b.remaining -= int64(n)
	return n, err
}

func (b *limitedBody) Close() error { return b.rc.Close() }

// GetWithAuth is Get with an "Authorization: Bearer" header.
func (c *Client) GetWithAuth(ctx context.Context, rawURL, bearer string) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("safehttp: invalid URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("safehttp: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	return c.Do(req)
}
