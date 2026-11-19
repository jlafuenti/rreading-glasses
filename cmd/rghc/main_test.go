package main

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseServe(t *testing.T, env map[string]string, args ...string) (*cli, error) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	var c cli
	parser, err := kong.New(&c)
	require.NoError(t, err)
	_, err = parser.Parse(append([]string{"serve", "--hardcover-auth=Bearer x"}, args...))
	return &c, err
}

// Hardcover rejects a request with more than 10 top-level queries
// ("request_exceeds_capacity"), so the default batch must fit under that cap.
func TestHardcoverBatchSizeDefaultsToTen(t *testing.T) {
	c, err := parseServe(t, nil)
	require.NoError(t, err)
	assert.Equal(t, 10, c.Serve.HardcoverBatchSize)
}

func TestHardcoverBatchSizeFromEnv(t *testing.T) {
	c, err := parseServe(t, map[string]string{"HARDCOVER_BATCH_SIZE": "5"})
	require.NoError(t, err)
	assert.Equal(t, 5, c.Serve.HardcoverBatchSize)
}

func TestHardcoverBatchSizeFromFlag(t *testing.T) {
	c, err := parseServe(t, nil, "--hardcover-batch-size=7")
	require.NoError(t, err)
	assert.Equal(t, 7, c.Serve.HardcoverBatchSize)
}

func TestHardcoverBatchSizeMustBePositive(t *testing.T) {
	_, err := parseServe(t, nil, "--hardcover-batch-size=0")
	assert.ErrorContains(t, err, "hardcover-batch-size")
}
