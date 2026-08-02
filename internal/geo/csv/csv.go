package csv

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"go-ip2country/internal/config"
	"go-ip2country/internal/geo"
)

func init() {
	geo.Register("csv", New)
}

type Locator struct {
	entries map[string]geo.Location
}

func New(cfg config.Config) (geo.Locator, error) {
	return load(cfg.DatastoreCSVPath)
}

func load(path string) (*Locator, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening csv datastore %q: %w", path, err)
	}
	defer f.Close()

	entries := make(map[string]geo.Location)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s:%d: expected 3 fields (ip,city,country), got %d", path, lineNum, len(fields))
		}
		ipStr := strings.TrimSpace(fields[0])
		city := strings.TrimSpace(fields[1])
		country := strings.TrimSpace(fields[2])

		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("%s:%d: invalid ip address %q", path, lineNum, ipStr)
		}
		entries[ip.String()] = geo.Location{City: city, Country: country}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading csv datastore %q: %w", path, err)
	}

	return &Locator{entries: entries}, nil
}

func (l *Locator) Lookup(ip net.IP) (geo.Location, error) {
	loc, ok := l.entries[ip.String()]
	if !ok {
		return geo.Location{}, geo.ErrNotFound
	}
	return loc, nil
}
