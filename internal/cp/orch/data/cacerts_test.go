// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"testing"

	"github.com/musakkka/openrail/internal/agent/assets"
	"github.com/musakkka/openrail/pkg/api"
	"github.com/musakkka/openrail/pkg/proto"
)

// The control plane names an asset the agent provides. If the two drift, the
// mount fails on the agent and the database container cannot start.
func TestCACertsNamesMatchTheAgent(t *testing.T) {
	if caCertsAsset != assets.CACertsName {
		t.Fatalf("control plane mounts asset %q, the agent provides %q", caCertsAsset, assets.CACertsName)
	}
	if caCertsTarget != assets.CACertsTarget {
		t.Fatalf("control plane mounts at %q, the agent documents %q", caCertsTarget, assets.CACertsTarget)
	}
}

// Database images ship without ca-certificates. WAL-G and restic must be told
// where the mounted bundle is, or no HTTPS bucket (R2, S3, B2) is reachable:
// the TLS handshake fails and the SDK retries forever, which is what happened
// on a real install the moment it moved off a plain-HTTP bucket.
func TestBackupEnvPointsTLSAtTheMountedBundle(t *testing.T) {
	env := backupEnv(api.BackupSettings{Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "b", Prefix: "p"},
		backupKeys{walgHex: "00", restic: "x"}, "svc_x", false)
	if got := env["SSL_CERT_FILE"]; got != caCertsTarget {
		t.Fatalf("SSL_CERT_FILE = %q, want %q", got, caCertsTarget)
	}
}

func TestCAMountIsReadOnlyAsset(t *testing.T) {
	m := caMount()
	if m.Kind != "asset" || m.Source != caCertsAsset || m.Target != caCertsTarget || !m.ReadOnly {
		t.Fatalf("unexpected CA mount %+v", m)
	}
}

// Every service that carries backup tools also carries the CA bundle, so the
// tools inside it (archive-wal, backup-push, restic) can reach an HTTPS
// bucket. Rendered for every template, the way TestRenderEveryTemplateAtHostSizes does.
func TestRenderedBackupServicesMountTheCABundle(t *testing.T) {
	e := newEnv(t)
	e.setBucket()
	ids, err := e.d.cat.TemplateIDs()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, id := range ids {
		tm, err := e.d.cat.Template(id)
		if err != nil {
			t.Fatal(err)
		}
		svcs := map[string]string{}
		for _, s := range tm.Services {
			svcs[s.Name] = e.addTemplateService(id, s.Name, false)
		}
		for _, s := range tm.Services {
			snap, err := e.cfg.ServiceInstance(e.ctx(), e.envID, svcs[s.Name])
			if err != nil {
				t.Fatal(err)
			}
			r, err := e.d.Render(e.ctx(), snap, proto.Facts{Arch: "amd64", CPUs: 4, MemBytes: 4 << 30, DataDir: "/var/lib/5050"})
			if err != nil {
				t.Fatalf("%s/%s: %v", id, s.Name, err)
			}
			if len(r.Tools) == 0 {
				continue
			}
			checked++
			found := false
			for _, m := range r.Mounts {
				if m == caMount() {
					found = true
				}
			}
			if !found {
				t.Errorf("%s/%s carries backup tools %v but no CA bundle", id, s.Name, r.Tools)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no template rendered backup tools; the test checked nothing")
	}
}
