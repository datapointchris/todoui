package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	gosync "sync"
	"time"
)

// tokenCommandTimeout bounds one run of sync.token_command. A command that
// refreshes its own login reaches an identity provider, which answers well
// inside this when it answers at all.
const tokenCommandTimeout = 10 * time.Second

// TokenCommandError is sync.token_command failing to print a token. Reason is
// the first line the command wrote to stderr where it wrote one, because that is
// where a CLI says its login has lapsed and what to run.
type TokenCommandError struct {
	Command string
	Reason  string
}

func (e *TokenCommandError) Error() string {
	return fmt.Sprintf("sync.token_command %q: %s", e.Command, e.Reason)
}

// tokenTransport sends the bearer token sync.token_command prints.
//
// The command runs on the first request and again only after the API refuses
// the token held, so a pull of many requests costs one run. A refused token is
// replaced once and the request sent once more; a second refusal is the API's
// answer, and so is a refusal of a token printed for this request.
type tokenTransport struct {
	argv []string
	base http.RoundTripper

	mu    gosync.Mutex
	token string
}

func newTokenTransport(argv []string, base http.RoundTripper) *tokenTransport {
	return &tokenTransport{argv: argv, base: base}
}

func (t *tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, printedNow, err := t.held(req.Context())
	if err != nil {
		closeBody(req)
		return nil, err
	}

	resp, err := t.send(req, req.Body, token)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || printedNow {
		return resp, err
	}
	// A body cannot be sent twice unless the request can produce it again, which
	// http.NewRequest arranges for every in-memory body.
	if req.Body != nil && req.GetBody == nil {
		return resp, nil
	}

	replacement, err := t.replace(req.Context(), token)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	var body io.ReadCloser
	if req.GetBody != nil {
		if body, err = req.GetBody(); err != nil {
			return resp, nil
		}
	}
	_ = resp.Body.Close()
	return t.send(req, body, replacement)
}

func (t *tokenTransport) send(req *http.Request, body io.ReadCloser, token string) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.Body = body
	out.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(out)
}

// held returns the token to send, running the command when none is held.
// printedNow reports that the command printed it for this request.
func (t *tokenTransport) held(ctx context.Context) (token string, printedNow bool, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != "" {
		return t.token, false, nil
	}
	if t.token, err = runTokenCommand(ctx, t.argv); err != nil {
		return "", false, err
	}
	return t.token, true, nil
}

// replace runs the command again for a refused token. Requests in flight
// together are refused together, so the first to arrive replaces it and the
// rest take its replacement rather than each running the command.
func (t *tokenTransport) replace(ctx context.Context, refused string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != "" && t.token != refused {
		return t.token, nil
	}
	token, err := runTokenCommand(ctx, t.argv)
	t.token = token
	return token, err
}

// runTokenCommand runs argv without a shell and with no stdin, so a command
// that would prompt fails rather than waiting on a terminal the TUI owns.
func runTokenCommand(ctx context.Context, argv []string) (string, error) {
	command := strings.Join(argv, " ")
	ctx, cancel := context.WithTimeout(ctx, tokenCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", &TokenCommandError{Command: command, Reason: failureReason(ctx, err, stderr.String())}
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", &TokenCommandError{Command: command, Reason: "printed no token"}
	}
	return token, nil
}

func failureReason(ctx context.Context, err error, stderr string) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("did not finish within %s", tokenCommandTimeout)
	}
	if line, _, _ := strings.Cut(strings.TrimSpace(stderr), "\n"); line != "" {
		return line
	}
	return err.Error()
}

func closeBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}
