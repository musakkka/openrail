// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "testing"

// The API's "0 keeps every image" must not reach agents as 0, which on the
// wire means "use your default".
func TestHostImageRetention(t *testing.T) {
	for _, c := range []struct{ setting, wire int }{{0, -1}, {1, 1}, {2, 2}, {50, 50}} {
		if got := hostImageRetention(c.setting); got != c.wire {
			t.Errorf("hostImageRetention(%d) = %d, want %d", c.setting, got, c.wire)
		}
	}
}
