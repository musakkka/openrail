// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"

	"github.com/musakkka/openrail/pkg/api"
)

// Images kept per service (9.19): default 2 on an install that never set it,
// 0 keeps every image, and it survives a read back.
func TestImageRetentionSetting(t *testing.T) {
	e := newEnv(t)
	owner, _, _ := secOwner(t, e)

	var in api.Install
	owner.do("GET", "/api/v1/install", nil, &in, 200)
	if in.ImageRetention != 2 {
		t.Fatalf("default imageRetention = %d, want 2", in.ImageRetention)
	}

	for _, bad := range []int{-1, 101} {
		owner.do("PATCH", "/api/v1/install", api.UpdateInstallRequest{ImageRetention: ptr(bad)}, nil, 422)
	}

	owner.do("PATCH", "/api/v1/install", api.UpdateInstallRequest{ImageRetention: ptr(0)}, &in, 200)
	if in.ImageRetention != 0 {
		t.Fatalf("keep-all came back as %d, want 0", in.ImageRetention)
	}
	owner.do("GET", "/api/v1/install", nil, &in, 200)
	if in.ImageRetention != 0 {
		t.Fatalf("keep-all was not stored: read back %d", in.ImageRetention)
	}

	owner.do("PATCH", "/api/v1/install", api.UpdateInstallRequest{ImageRetention: ptr(5)}, &in, 200)
	if in.ImageRetention != 5 {
		t.Fatalf("imageRetention = %d after setting 5", in.ImageRetention)
	}

	// An unrelated change must not reset it to the default.
	owner.do("PATCH", "/api/v1/install", api.UpdateInstallRequest{BuildTimeoutMin: ptr(45)}, &in, 200)
	if in.ImageRetention != 5 {
		t.Fatalf("an unrelated PATCH reset imageRetention to %d", in.ImageRetention)
	}
}
