// SPDX-License-Identifier: AGPL-3.0-only

package core

import "testing"

func TestInternalHost(t *testing.T) {
	for v, want := range map[string]string{
		"http://oracle-v2.internal:8080":   "oracle-v2",
		"postgres://u:p@pg.internal/db":    "pg",
		"redis.internal":                   "redis",
		"https://a.internal.example.com/":  "",
		"https://internal.example.com:443": "",
	} {
		got := ""
		if m := internalHost.FindStringSubmatch(v); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", v, got, want)
		}
	}
}
