package db

import (
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"time"
)

type Domain struct {
	db    *sql.DB
	realm string

	Name   string
	SOA    DomainSOA
	Serial DomainSerial
}

func (d *Domain) validate() error {
	soa := &d.SOA

	if _, _, err := net.ParseCIDR(d.Name); err == nil {
		if soa.PrimaryNS == "" {
			return fmt.Errorf("Must explicitly specify the primary NS for ARPA domain %s", d.Name)
		}
		if soa.Email == "" {
			return fmt.Errorf("Must explicitly specify the email for ARPA domain %s", d.Name)
		}
	}

	if soa.PrimaryNS == "" {
		soa.PrimaryNS = "ns1." + d.Name
	}
	if soa.Email == "" {
		soa.Email = "hostmaster." + d.Name
	}
	if soa.SlaveRefresh == 0 {
		soa.SlaveRefresh = time.Hour
	}
	if soa.SlaveRetry == 0 {
		soa.SlaveRetry = 15 * time.Minute
	}
	if soa.SlaveExpiry == 0 {
		soa.SlaveExpiry = 21 * 24 * time.Hour // 3 weeks
	}
	if soa.NXDomainTTL == 0 {
		soa.NXDomainTTL = 10 * time.Minute
	}
	return nil
}

func (d *Domain) Create() error {
	if err := d.validate(); err != nil {
		return err
	}
	d.Serial.Inc()

	q := `
INSERT INTO domains (realm_id, name, primary_ns, email, slave_refresh, slave_retry, slave_expiry, nxdomain_ttl, serial)
VALUES ((SELECT realm_id FROM realms WHERE name = $1), $2, $3, $4, $5, $6, $7, $8, $9)
`
	_, err := d.db.Exec(q, d.realm, d.Name, d.SOA.PrimaryNS, d.SOA.Email, d.SOA.SlaveRefresh, d.SOA.SlaveRetry, d.SOA.SlaveExpiry, d.SOA.NXDomainTTL, d.Serial.String())
	if err != nil && errIsAlreadyExists(err) {
		return ErrAlreadyExists
	}
	return err
}

func (d *Domain) Save() error {
	if err := d.validate(); err != nil {
		return err
	}
	d.Serial.Inc()

	q := `
UPDATE domains
SET primary_ns=$1, email=$2, slave_refresh=$3, slave_retry=$4, slave_expiry=$5, nxdomain_ttl=$6, serial=$7
WHERE realm_id=(SELECT realm_id FROM realms WHERE name=$8) AND name=$9
`
	res, err := d.db.Exec(q, d.SOA.PrimaryNS, d.SOA.Email, d.SOA.SlaveRefresh, d.SOA.SlaveRetry, d.SOA.SlaveExpiry, d.SOA.NXDomainTTL, d.Serial.String(), d.realm, d.Name)
	if err != nil {
		return err
	}
	return mustHaveChanged(res)
}

func (d *Domain) Delete() error {
	q := `
DELETE FROM domains
WHERE realm_id=(SELECT realm_id FROM realms WHERE name=$1) AND name=$2
`
	if _, err := d.db.Exec(q, d.realm, d.Name); err != nil {
		return err
	}
	return nil
}

func (d *Domain) Get() error {
	q := `
SELECT primary_ns, email, slave_refresh, slave_retry, slave_expiry, nxdomain_ttl, serial
FROM domains INNER JOIN realms USING (realm_id)
WHERE realms.name=$1 AND domains.name=$2
`
	var refresh, retry, expiry, ttl int64
	if err := d.db.QueryRow(q, d.realm, d.Name).Scan(&d.SOA.PrimaryNS, &d.SOA.Email, &refresh, &retry, &expiry, &ttl, &d.Serial); err != nil {
		if err == sql.ErrNoRows {
			return ErrNotFound
		}
		return err
	}
	d.SOA.SlaveRefresh = time.Duration(refresh)
	d.SOA.SlaveRetry = time.Duration(retry)
	d.SOA.SlaveExpiry = time.Duration(expiry)
	d.SOA.NXDomainTTL = time.Duration(ttl)
	return nil
}

func (d *Domain) AddRecord(record string) error {
	q := `
INSERT INTO domain_records (domain_id, record)
VALUES (
  (
    SELECT domain_id
    FROM domains INNER JOIN realms USING (realm_id)
    WHERE realms.name=$1 AND domains.name=$2
  ), $3)
`
	_, err := d.db.Exec(q, d.realm, d.Name, record)
	if err != nil && errIsAlreadyExists(err) {
		return ErrAlreadyExists
	}
	return err
}

func (d *Domain) DeleteRecord(record string) error {
	q := `
DELETE FROM domain_records
WHERE domain_id=(
  SELECT domain_id
  FROM domains INNER JOIN realms USING (realm_id)
  WHERE realms.name=$1 AND domains.name=$2)
AND record=$3
`
	if _, err := d.db.Exec(q, d.realm, d.Name, record); err != nil {
		return err
	}
	return nil
}

func (d *Domain) Records() ([]string, error) {
	q := `
SELECT record
FROM domain_records INNER JOIN domains USING (domain_id) INNER JOIN realms USING (realm_id)
WHERE realms.name=$1 AND domains.name=$2
`
	rows, err := d.db.Query(q, d.realm, d.Name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ret []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			return nil, err
		}
		ret = append(ret, s)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return ret, nil
}

type DomainSOA struct {
	PrimaryNS    string
	Email        string
	SlaveRefresh time.Duration
	SlaveRetry   time.Duration
	SlaveExpiry  time.Duration
	NXDomainTTL  time.Duration
}

type DomainSerial struct {
	date time.Time
	inc  int
}

func (ds *DomainSerial) Scan(v interface{}) error {
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case []byte:
		s = string(t)
	default:
		return fmt.Errorf("Non-string %q (%T) cannot be domain serial", v, v)
	}

	if s == "0" {
		ds.date = time.Time{}
		ds.inc = 0
		return nil
	}
	if len(s) != 10 {
		return fmt.Errorf("invalid domain serial %q", s)
	}
	date, err := time.Parse("20060102", s[:8])
	if err != nil {
		return fmt.Errorf("Invalid date section of zone serial %s", s[:8])
	}
	inc, err := strconv.Atoi(s[8:])
	if err != nil {
		return fmt.Errorf("Invalid counter section of zone serial %s", s[8:])
	}
	ds.date = date
	ds.inc = inc
	return nil
}

// Inc increments z, following the date-as-zone conventions. For
// example, 2014042915 might increment to 2014042916 or 2014043001.
func (ds *DomainSerial) Inc() {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	y, m, d := ds.date.Date()
	y2, m2, d2 := now.Date()
	if y == y2 && m == m2 && d == d2 {
		if ds.inc == 99 {
			panic("Zone serial overflow")
		}
		ds.inc++
	} else {
		ds.date = now
		ds.inc = 0
	}
}

// Before returns true if z describes an older zone than oz.
func (ds DomainSerial) Before(ods DomainSerial) bool {
	if ds.date.Before(ods.date) {
		return true
	}
	return ds.inc < ods.inc
}

// String returns the zone serial in the YYYYMMDDxx format.
func (ds DomainSerial) String() string {
	return fmt.Sprintf("%s%02d", ds.date.Format("20060102"), ds.inc)
}
