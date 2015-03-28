package database

import (
	"fmt"
	"math/rand"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/aryann/difflib"
)

func cidr(in string) *net.IPNet {
	_, net, err := net.ParseCIDR(in)
	if err != nil {
		panic(err)
	}
	return net
}

func deleteNet(db *DB, net *net.IPNet) error {
	subnet := db.Subnet(net, true)
	if subnet == nil {
		return fmt.Errorf("Expected subnet for %s not in DB", net)
	}
	subnet.Delete(false)
	return nil
}

func asJSON(db *DB) string {
	ret, err := db.Bytes()
	if err != nil {
		panic(err)
	}
	return string(ret)
}

func fromJSON(raw string) *DB {
	ret, err := LoadBytes([]byte(raw))
	if err != nil {
		panic(err)
	}
	return ret
}

func DBDiff(a, b *DB) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	var ret string
	for _, record := range difflib.Diff(strings.Split(asJSON(a), "\n"), strings.Split(asJSON(b), "\n")) {
		ret += record.String() + "\n"
	}
	return ret
}

func TestBasicAllocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action   func(*DB) error
		expected string
	}{
		// Add a subnet
		{
			func(d *DB) error {
				_, err := d.AddSubnet("foo", cidr("192.168.144.0/22"), nil)
				return err
			},
			`{
  "Subnets": {
    "192.168.144.0/22": {
      "Net": "192.168.144.0/22",
      "Name": "foo"
    }
  }
}`,
		},

		// Add a child subnet
		{
			func(d *DB) error {
				_, err := d.AddSubnet("bar", cidr("192.168.144.16/29"), nil)
				return err
			},
			`{
  "Subnets": {
    "192.168.144.0/22": {
      "Net": "192.168.144.0/22",
      "Name": "foo",
      "Subnets": {
        "192.168.144.16/29": {
          "Net": "192.168.144.16/29",
          "Name": "bar"
        }
      }
    }
  }
}`,
		},

		// Non-recursively delete the parent subnet
		{
			func(d *DB) error {
				subnet := d.Subnet(cidr("192.168.144.0/22"), false)
				if subnet == nil {
					return fmt.Errorf("Subnet not found in DB")
				}
				subnet.Delete(false)
				return nil
			},
			`{
  "Subnets": {
    "192.168.144.16/29": {
      "Name": "bar",
      "Net": "192.168.144.16/29"
    }
  }
}`,
		},
	}

	db := New()
	for _, test := range tests {
		if err := test.action(db); err != nil {
			t.Fatalf("Error performing action: %s\nexpected state after action:\n%s", err, test.expected)
		}
		if d := DBDiff(fromJSON(test.expected), db); d != "" {
			t.Errorf("DB state differs from expected:\n%s", d)
			t.Errorf("If no diff is visible, it means internal structures don't match.")
			t.FailNow()
		}
	}
}

// Stress test of the subnetting and reparenting logic.
func TestAllocateDeallocate(t *testing.T) {
	t.Parallel()
	ranges := []string{
		"192.168.144.0/22",
		"192.168.144.0/26",
		"192.168.144.0/28",
		"192.168.144.16/29",
		"192.168.144.32/28",
		"192.168.144.56/29",
		"192.168.144.64/31",
		"192.168.144.66/31",
		"192.168.144.68/31",
		"192.168.144.64/27",
		"192.168.144.70/31",
		"192.168.144.72/31",
		"192.168.144.128/25",
		"192.168.144.128/27",
		"192.168.144.240/28",
	}

	for it := 0; it < 100; it++ {
		// We mutate golden during the deletion testing, so recreate
		// it each time.
		golden := New()
		for _, r := range ranges {
			if _, err := golden.AddSubnet(r, cidr(r), nil); err != nil {
				t.Fatalf("Adding %s to golden DB failed: %s", r, err)
			}
		}

		// Build a new DB with a randomized order
		order := rand.Perm(len(ranges))
		var rangesInOrder []string
		db := New()
		for _, i := range order {
			rangesInOrder = append(rangesInOrder, ranges[i])
			r := cidr(ranges[i])
			if _, err := db.AddSubnet(r.String(), r, nil); err != nil {
				t.Fatalf("Adding %s to DB failed: %s", r, err)
			}
		}

		if err := db.validate(); err != nil {
			t.Fatalf("Internal validation failure: %s", err)
		}

		if d := DBDiff(golden, db); d != "" {
			t.Errorf("DB state differs after addition sequence %#v", rangesInOrder)
			t.Errorf("%s", d)
			t.Errorf("If no diff is visible, it means internal structures don't match.")
			t.FailNow()
		}

		// Now delete in a random order, pulling from golden and db in
		// lockstep, checking for correctness at every step.
		order = rand.Perm(len(ranges))
		rangesInOrder = nil
		for _, i := range order {
			rangesInOrder = append(rangesInOrder, ranges[i])
			if err := deleteNet(golden, cidr(ranges[i])); err != nil {
				t.Errorf("Deleting %s from golden: %s", ranges[i], err)
				t.Errorf("Delete sequence: %#v", rangesInOrder)
				t.Errorf("Golden DB:\n%s", asJSON(golden))
				t.FailNow()
			}
			if err := deleteNet(db, cidr(ranges[i])); err != nil {
				t.Errorf("Deleting %s from db: %s", ranges[i], err)
				t.Errorf("Delete sequence: %#v", rangesInOrder)
				t.Errorf("DB:\n%s", asJSON(db))
				t.FailNow()
			}
			if err := db.validate(); err != nil {
				t.Fatalf("Internal validation failure: %s", err)
			}

			if d := DBDiff(golden, db); d != "" {
				t.Errorf("DB state differs after deletion sequence %#v", rangesInOrder)
				t.Errorf("%s", d)
				t.Errorf("If no diff is visible, it means internal structures don't match.")
				t.FailNow()
			}
		}
	}
}

// This is used in the various tests that just need a reasonable
// network to play with.
const sampleNet = `{
  "Subnets": {
    "192.168.1.0/24": {
      "Name": "muz",
      "Net": "192.168.1.0/24",
      "Subnets": {
        "192.168.1.128/25": {
          "Name": "darf",
          "Net": "192.168.1.128/25"
        }
      }
    },
    "192.168.144.0/22": {
      "Name": "foo",
      "Net": "192.168.144.0/22",
      "Subnets": {
        "192.168.144.0/28": {
          "Name": "bar",
          "Net": "192.168.144.0/28",
          "Subnets": {
            "192.168.144.2/31": {
              "Name": "qux",
              "Net": "192.168.144.2/31"
            }
          }
        },
        "192.168.144.16/29": {
          "Name": "baz",
          "Net": "192.168.144.16/29"
        }
      }
    }
  }
}`

func TestHostsAddRm(t *testing.T) {
	t.Parallel()
	db := fromJSON(sampleNet)
	ip := net.ParseIP("192.168.144.1")

	h, err := db.AddHost("router", []net.IP{ip}, nil)
	if err != nil {
		t.Fatalf("Adding host failed: %s", err)
	}

	h2 := db.Host(ip)
	if h != h2 {
		t.Fatalf("Couldn't find host I just added to the DB")
	}

	h.Delete()
	if h2 = db.Host(ip); h2 != nil {
		t.Fatalf("Deleted host %s, but it's still in the DB", ip)
	}
}

func TestHostMultiAddr(t *testing.T) {
	t.Parallel()
	db := fromJSON(sampleNet)
	ip1 := net.ParseIP("192.168.144.1")
	ip2 := net.ParseIP("192.168.1.1")

	h, err := db.AddHost("router", []net.IP{ip1, ip2}, nil)
	if err != nil {
		t.Fatalf("Adding host failed: %s", err)
	}
	if h == nil {
		t.Fatalf("AddHost returned nil host w/out error")
	}

	h2 := db.Host(ip1)
	if h != h2 {
		t.Fatalf("Couldn't find host %s", ip1)
	}
	h2 = db.Host(ip2)
	if h != h2 {
		t.Fatalf("Couldn't find host %s", ip2)
	}

	h.Delete()
	if h = db.Host(ip1); h != nil {
		t.Fatalf("Deleted host %s, but it's still in the DB", ip1)
	}
	if h = db.Host(ip2); h != nil {
		t.Fatalf("Deleted host %s, but it's still in the DB", ip2)
	}
}

func TestNetContains(t *testing.T) {
	t.Parallel()
	type table struct {
		n1, n2  string
		outcome bool
	}
	tests := []table{
		{"192.168.208.0/22", "192.168.208.0/23", true},
		{"192.168.208.0/22", "192.168.208.0/24", true},
		{"192.168.208.0/22", "192.168.208.0/22", true},
		{"192.168.208.0/22", "192.168.209.0/24", true},
		{"192.168.208.0/23", "192.168.208.0/22", false},
	}

	for _, test := range tests {
		n1 := (*IPNet)(cidr(test.n1))
		n2 := (*IPNet)(cidr(test.n2))
		res := n1.ContainsIPNet(n2)
		if res != test.outcome {
			t.Errorf("ContainsIPNet(%#v, %#v) = %v, want %v", test.n1, test.n2, res, test.outcome)
		}
	}
}

func TestZoneSerial(t *testing.T) {
	t.Parallel()
	var zs ZoneSerial
	if zs.String() != "0001010100" {
		t.Fatalf("Bad zero ZoneSerial %s", zs)
	}

	var zs2 ZoneSerial
	zs2.Inc()
	if !zs.Before(zs2) {
		t.Fatalf("Time-travelling zone serial: %s < %s", zs2, zs)
	}

	if err := zs.UnmarshalJSON([]byte("\"2012030699\"")); err != nil {
		t.Fatalf("Unmarshalling valid ZoneSerial: %s", err)
	}

	if !zs.Before(zs2) {
		t.Fatalf("Time-travelling zone serial: %s < %s", zs2, zs)
	}
	if zs.date.Year() != 2012 || zs.date.Month() != 3 || zs.date.Day() != 6 {
		t.Fatalf("Unmarshalled ZoneSerial %s has wrong date, should be 20120306", zs)
	}
	if zs.inc != 99 {
		t.Fatalf("Unmarshalled ZoneSerial %s has wrong increment, should be 99", zs)
	}

	b, err := zs.MarshalJSON()
	if err != nil {
		t.Fatalf("Marshalling ZoneSerial %s: %s", zs, err)
	}
	if string(b) != "\"2012030699\"" {
		t.Fatalf("Marshaled ZoneSerial %s is wrong, should be \"2012030699\"", zs)
	}
}
