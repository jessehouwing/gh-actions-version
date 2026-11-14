package version

import (
	"strings"
	"testing"
)

func TestConvertTo(t *testing.T) {
	t.Run("3-part version number support", func(t *testing.T) {
		for in, want := range map[string]string{
			"1": "1.0.0", "2": "2.0.0", "10": "10.0.0", "1.0": "1.0.0", "1.1": "1.1.0", "2.5": "2.5.0",
			"1.0.0": "1.0.0", "1.2.3": "1.2.3", "10.20.30": "10.20.30", "0.0.1": "0.0.1",
		} {
			t.Run("Should convert '"+in+"' to version "+want, func(t *testing.T) {
				v, err := ConvertTo(in)
				if err != nil || v.String() != want {
					t.Fatalf("got %v, %v want %s", v, err, want)
				}
			})
		}
	})
	t.Run("Versions with more than 3 components are truncated", func(t *testing.T) {
		for in, want := range map[string]string{"1.2.3.4": "1.2.3", "1.0.0.0": "1.0.0", "10.20.30.40": "10.20.30"} {
			t.Run("Should truncate '"+in+"' to '"+want+"'", func(t *testing.T) {
				v, err := ConvertTo(in)
				if err != nil || v.String() != want {
					t.Fatalf("got %v, %v want %s", v, err, want)
				}
			})
		}
	})
	t.Run("Should throw on null or empty input", func(t *testing.T) {
		for _, in := range []string{"", "   "} {
			if _, err := ConvertTo(in); err == nil {
				t.Fatalf("expected error for %q", in)
			}
		}
	})
	t.Run("Version object properties", func(t *testing.T) {
		for in, want := range map[string]Version{"1.2.3": {1, 2, 3}, "5": {5, 0, 0}, "3.7": {3, 7, 0}} {
			if v, _ := ConvertTo(in); v != want {
				t.Errorf("%s: got %v want %v", in, v, want)
			}
		}
	})
	t.Run("Compare orders numerically", func(t *testing.T) {
		a, _ := ConvertTo("1.10.0")
		b, _ := ConvertTo("1.9.0")
		if a.Compare(b) != 1 || b.Compare(a) != -1 || a.Compare(a) != 0 {
			t.Fatal("bad compare")
		}
	})
}

func TestIsValidPattern(t *testing.T) {
	t.Run("Should accept valid patterns", func(t *testing.T) {
		for _, p := range []string{"v1", "v2", "v10", "v1.0", "v1.1", "v10.20", "v1.0.0", "v1.2.3", "v10.20.30", "v1.*", "v2.*", "v1.0.*", "v1.2.*"} {
			t.Run("Should accept '"+p+"' as valid", func(t *testing.T) {
				if !IsValidPattern(p) {
					t.Fatal("want valid")
				}
			})
		}
	})
	t.Run("Should reject invalid patterns (ReDoS prevention)", func(t *testing.T) {
		for _, p := range []string{"v", "v-1", "v1.*.0", "v*.0", "v1.0.0-beta", "v1.0.0+build", "invalid", "1.0.0", "", "   "} {
			t.Run("Should reject '"+p+"' as invalid", func(t *testing.T) {
				if IsValidPattern(p) {
					t.Fatal("want invalid")
				}
			})
		}
	})
	t.Run("Should reject patterns longer than 50 characters", func(t *testing.T) {
		if IsValidPattern("v" + strings.Repeat("1", 60)) {
			t.Fatal("want invalid")
		}
	})
	t.Run("Should accept patterns at the limit", func(t *testing.T) {
		if !IsValidPattern("v" + strings.Repeat("1", 10) + "." + strings.Repeat("2", 10) + "." + strings.Repeat("3", 10)) {
			t.Fatal("want valid")
		}
	})
}

func TestShouldBeIgnored(t *testing.T) {
	cases := []struct {
		v      string
		ignore []string
		want   bool
	}{
		{"v1.0.0", nil, false},
		{"v1.0.0", []string{"v1.*"}, true},
		{"v1.0", []string{"v1.*"}, true},
		{"v1", []string{"v1.*"}, false},
		{"v1.0.0", []string{"v1.0.*"}, true},
		{"v1.1.0", []string{"v1.0.*"}, false},
		{"v2.0.0", []string{"v1.0.0", "v1.1.0"}, false},
		{"v1.0.0", []string{"v1.0.0"}, true},
		{"V1.0.0", []string{"v1.0.0"}, true},
	}
	for _, c := range cases {
		t.Run(c.v+" vs "+strings.Join(c.ignore, ","), func(t *testing.T) {
			if got := ShouldBeIgnored(c.v, c.ignore); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
	t.Run("IsIgnored exact and wildcard", func(t *testing.T) {
		if !IsIgnored("v1.2.3", []string{"v1.*"}) || IsIgnored("v1", []string{"v1.*"}) || !IsIgnored("v1", []string{"v1"}) || IsIgnored("v2", nil) {
			t.Fatal("unexpected")
		}
	})
}
