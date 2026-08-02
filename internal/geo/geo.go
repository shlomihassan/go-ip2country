package geo

import (
	"errors"
	"fmt"
	"net"

	"go-ip2country/internal/config"
)

type Location struct {
	Country string
	City    string
}

var ErrNotFound = errors.New("location not found")

type Locator interface {
	Lookup(ip net.IP) (Location, error)
}

type Factory func(cfg config.Config) (Locator, error)

var registry = map[string]Factory{}

func Register(name string, f Factory) {
	registry[name] = f
}

func New(name string, cfg config.Config) (Locator, error) {
	f, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown datastore type %q", name)
	}
	return f(cfg)
}
