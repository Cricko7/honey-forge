package events

import (
	"errors"
	"fmt"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
)

// IPDatabases is an immutable local snapshot. Close it only after requests stop.
type IPDatabases struct {
	country *maxminddb.Reader
	asn     *maxminddb.Reader
}

// Empty paths disable the corresponding lookup; configured files must be valid.
func OpenIPDatabases(countryPath, asnPath string) (*IPDatabases, error) {
	db := &IPDatabases{}
	var err error
	db.country, err = openIPDatabase(countryPath)
	if err != nil {
		return nil, fmt.Errorf("open country database: %w", err)
	}
	db.asn, err = openIPDatabase(asnPath)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open ASN database: %w", err), db.Close())
	}
	return db, nil
}

func openIPDatabase(path string) (*maxminddb.Reader, error) {
	if path == "" {
		return nil, nil
	}
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	if err := db.Verify(); err != nil {
		return nil, errors.Join(fmt.Errorf("verify IP database: %w", err), db.Close())
	}
	return db, nil
}

func (db *IPDatabases) Close() error {
	var errs []error
	if db.country != nil {
		errs = append(errs, db.country.Close())
	}
	if db.asn != nil {
		errs = append(errs, db.asn.Close())
	}
	return errors.Join(errs...)
}

var specialSourceNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// Lookup never performs network I/O and never infers locations for local IPs.
func (db *IPDatabases) Lookup(source string) (*Enrichment, error) {
	result := &Enrichment{}
	ip, err := netip.ParseAddr(source)
	if err != nil || ip.Zone() != "" {
		return result, nil
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return result, nil
	}
	for _, network := range specialSourceNetworks {
		if network.Contains(ip) {
			return result, nil
		}
	}
	var errs []error
	if db.country != nil {
		var record struct {
			Country struct {
				ISOCode string `maxminddb:"iso_code"`
			} `maxminddb:"country"`
		}
		if err := db.country.Lookup(ip).Decode(&record); err != nil {
			errs = append(errs, fmt.Errorf("lookup country: %w", err))
		} else if code := record.Country.ISOCode; len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' {
			result.CountryCode = &record.Country.ISOCode
		}
	}
	if db.asn != nil {
		var record struct {
			ASN uint32 `maxminddb:"autonomous_system_number"`
		}
		if err := db.asn.Lookup(ip).Decode(&record); err != nil {
			errs = append(errs, fmt.Errorf("lookup ASN: %w", err))
		} else if record.ASN > 0 {
			result.ASN = &record.ASN
		}
	}
	return result, errors.Join(errs...)
}
