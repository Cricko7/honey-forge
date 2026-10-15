package events

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestIPDatabasesLookup(t *testing.T) {
	db, err := OpenIPDatabases("testdata/country.mmdb", "testdata/asn.mmdb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, tc := range []struct {
		name, ip, country string
		asn               uint32
	}{
		{"IPv4", "8.8.8.8", "US", 15169},
		{"mapped IPv4", "::ffff:8.8.8.8", "US", 15169},
		{"IPv6", "2001:4860:4860::8888", "US", 15169},
		{"unknown public", "9.9.9.9", "", 0},
		{"private", "10.0.0.1", "", 0},
		{"loopback", "127.0.0.1", "", 0},
		{"IPv6 loopback", "::1", "", 0},
		{"link local", "169.254.1.1", "", 0},
		{"multicast", "224.0.0.1", "", 0},
		{"unspecified", "0.0.0.0", "", 0},
		{"private IPv6", "fd00::1", "", 0},
		{"documentation", "192.0.2.1", "", 0},
		{"shared space", "100.64.0.1", "", 0},
		{"invalid", "attacker.example", "", 0},
		{"zone", "fe80::1%eth0", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := db.Lookup(tc.ip)
			if err != nil {
				t.Fatal(err)
			}
			if tc.country == "" {
				if got.CountryCode != nil || got.ASN != nil {
					t.Fatalf("unexpected enrichment: %+v", got)
				}
			} else if got.CountryCode == nil || *got.CountryCode != tc.country || got.ASN == nil || *got.ASN != tc.asn {
				t.Fatalf("enrichment: %+v", got)
			}
		})
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			got, err := db.Lookup("8.8.8.8")
			if err != nil || got.ASN == nil || *got.ASN != 15169 {
				t.Errorf("concurrent lookup: %+v %v", got, err)
			}
		})
	}
	wg.Wait()
}

func TestIPDatabasesConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, country, asn            string
		wantCountry, wantASN, wantErr bool
	}{
		{"disabled", "", "", false, false, false},
		{"country only", "testdata/country.mmdb", "", true, false, false},
		{"ASN only", "", "testdata/asn.mmdb", false, true, false},
		{"missing country", "testdata/missing.mmdb", "", false, false, true},
		{"missing ASN after country", "testdata/country.mmdb", "testdata/missing.mmdb", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := OpenIPDatabases(tc.country, tc.asn)
			if (err != nil) != tc.wantErr {
				t.Fatalf("open: %v", err)
			}
			if err != nil {
				return
			}
			defer func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			}()
			got, err := db.Lookup("8.8.8.8")
			if err != nil || (got.CountryCode != nil) != tc.wantCountry || (got.ASN != nil) != tc.wantASN {
				t.Fatalf("lookup: %+v %v", got, err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "corrupt.mmdb")
	if err := os.WriteFile(path, []byte("not an MMDB"), 0600); err != nil {
		t.Fatal(err)
	}
	if db, err := OpenIPDatabases(path, ""); err == nil {
		db.Close()
		t.Fatal("accepted corrupt database")
	}
}
