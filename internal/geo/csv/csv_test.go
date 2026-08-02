package csv_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ip2country/internal/config"
	"go-ip2country/internal/geo"
	geocsv "go-ip2country/internal/geo/csv"
)

func TestNew_LoadsAndLooksUpEntries(t *testing.T) {
	locator, err := geocsv.New(config.Config{DatastoreCSVPath: "testdata/valid.csv"})
	require.NoError(t, err)

	loc, err := locator.Lookup(net.ParseIP("8.8.8.8"))
	require.NoError(t, err)
	assert.Equal(t, geo.Location{Country: "USA", City: "Mountain View"}, loc)
}

func TestLookup_NotFound(t *testing.T) {
	locator, err := geocsv.New(config.Config{DatastoreCSVPath: "testdata/valid.csv"})
	require.NoError(t, err)

	_, err = locator.Lookup(net.ParseIP("9.9.9.9"))
	assert.ErrorIs(t, err, geo.ErrNotFound)
}

func TestNew_MalformedCSVFailsToLoad(t *testing.T) {
	_, err := geocsv.New(config.Config{DatastoreCSVPath: "testdata/malformed.csv"})
	assert.Error(t, err)
}

func TestNew_MissingFileFailsToLoad(t *testing.T) {
	_, err := geocsv.New(config.Config{DatastoreCSVPath: "testdata/does-not-exist.csv"})
	assert.Error(t, err)
}
