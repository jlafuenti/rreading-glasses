package internal

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Khan/genqlient/graphql"
	"github.com/blampe/rreading-glasses/hardcover"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _workAlias = regexp.MustCompile(`(\w+): books_by_pk`)

// statusThenOK answers the first `failures` requests with the given status
// and every later one with a work titled "ok".
func statusThenOK(t *testing.T, status int, failures int32, calls *atomic.Int32) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		n := calls.Add(1)
		if n <= failures {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"data":null}`))}, nil
		}
		var req struct{ Query string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return nil, err
		}
		m := _workAlias.FindStringSubmatch(req.Query)
		if m == nil {
			return nil, fmt.Errorf("no books_by_pk alias in %q", req.Query)
		}
		body := fmt.Sprintf(`{"data":{%q:{"id":1,"title":"ok"}}}`, m[1])
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

func fastRetries(t *testing.T, gql graphql.Client, max int) {
	t.Helper()
	c := gql.(*batchedgqlclient)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.retryBase = 10 * time.Millisecond
	c.maxRetries = max
}

// Hardcover answers 429 when we exceed its rate limit. The whole batch fails,
// so every query in it used to be lost; it should be retried instead.
func TestBatchingRetriesRateLimitedBatch(t *testing.T) {
	calls := atomic.Int32{}
	gql, err := NewBatchedGraphQLClient("https://foo.com", statusThenOK(t, http.StatusTooManyRequests, 2, &calls), 10*time.Millisecond, 10, nil)
	require.NoError(t, err)
	fastRetries(t, gql, 4)

	resp, err := hardcover.GetWork(t.Context(), gql, 1)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Books_by_pk.GetTitle())
	assert.Equal(t, int32(3), calls.Load())
}

func TestBatchingGivesUpAfterMaxRetries(t *testing.T) {
	calls := atomic.Int32{}
	gql, err := NewBatchedGraphQLClient("https://foo.com", statusThenOK(t, http.StatusTooManyRequests, 100, &calls), 10*time.Millisecond, 10, nil)
	require.NoError(t, err)
	fastRetries(t, gql, 2)

	_, err = hardcover.GetWork(t.Context(), gql, 1)

	var httpErr *graphql.HTTPError
	require.True(t, errors.As(err, &httpErr), "want a 429 HTTPError, got %v", err)
	assert.Equal(t, http.StatusTooManyRequests, httpErr.StatusCode)
	assert.Equal(t, int32(3), calls.Load()) // First try plus two retries.
}

func TestBatchingDoesNotRetryOtherErrors(t *testing.T) {
	calls := atomic.Int32{}
	gql, err := NewBatchedGraphQLClient("https://foo.com", statusThenOK(t, http.StatusForbidden, 1, &calls), 10*time.Millisecond, 10, nil)
	require.NoError(t, err)
	fastRetries(t, gql, 4)

	_, err = hardcover.GetWork(t.Context(), gql, 1)

	require.Error(t, err)
	time.Sleep(100 * time.Millisecond) // Room for a wrongful retry to show up.
	assert.Equal(t, int32(1), calls.Load())
}

// The default backoff must leave Hardcover's per-minute window time to
// recover, and the default must actually retry.
func TestBatchingRetryDefaults(t *testing.T) {
	gql, err := NewBatchedGraphQLClient("https://foo.com", http.DefaultClient, time.Hour, 10, nil)
	require.NoError(t, err)
	c := gql.(*batchedgqlclient)
	assert.Equal(t, 5*time.Second, c.retryBase)
	assert.Equal(t, 4, c.maxRetries)
}
