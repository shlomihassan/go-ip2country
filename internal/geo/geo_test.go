package geo_test

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ip2country/internal/config"
	"go-ip2country/internal/geo"
)

type fakeLocator struct{}

func (fakeLocator) Lookup(net.IP) (geo.Location, error) {
	return geo.Location{Country: "USA", City: "Testville"}, nil
}

func TestRegisterAndNew(t *testing.T) {
	geo.Register("geo-test-fake", func(cfg config.Config) (geo.Locator, error) {
		return fakeLocator{}, nil
	})

	locator, err := geo.New("geo-test-fake", config.Config{})
	require.NoError(t, err)

	loc, err := locator.Lookup(net.ParseIP("1.2.3.4"))
	require.NoError(t, err)
	assert.Equal(t, geo.Location{Country: "USA", City: "Testville"}, loc)
}

func TestNew_UnknownDatastoreType(t *testing.T) {
	_, err := geo.New("does-not-exist", config.Config{})
	assert.Error(t, err)
}
