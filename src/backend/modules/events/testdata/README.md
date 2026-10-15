# Synthetic IP lookup fixtures

`country.mmdb` and `asn.mmdb` contain synthetic records, generated with
github.com/maxmind/mmdbwriter v1.2.0. They are test inputs, not production IP data.

Networks: `8.8.8.0/24`, `2001:4860::/32`, `10.0.0.0/8`, `100.64.0.0/10`,
and `192.0.2.0/24`. Every record has `country.iso_code = US` in the country
file and `autonomous_system_number = 15169` in the ASN file. Reserved networks
intentionally contain records to verify that lookups reject them regardless
of database contents. Every other address is absent.
