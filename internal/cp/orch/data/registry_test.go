// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/musakkka/openrail/pkg/proto"
)

// TestRegistryGCSequencing checks 9.6: pause the build queue, switch the
// registry read-only, collect, switch back, then resume, even on failure.
func TestRegistryGCSequencing(t *testing.T) {
	for _, fail := range []bool{false, true} {
		e := newEnv(t)
		e.run.OnRegistryJob = func(cmd []string, env map[string]string) (proto.JobResult, error) {
			if fail && len(cmd) > 1 && cmd[1] == "garbage-collect" {
				return proto.JobResult{ExitCode: 1}, nil
			}
			return proto.JobResult{}, nil
		}
		err := e.d.RunRegistryGC(e.ctx(), 3)
		if fail != (err != nil) {
			t.Fatalf("fail=%v: err=%v", fail, err)
		}
		var seq []string
		for _, c := range e.run.Calls() {
			if strings.HasPrefix(c, "PauseBuilds") || strings.HasPrefix(c, "ResumeBuilds") || strings.HasPrefix(c, "Registry") {
				seq = append(seq, c)
			}
		}
		want := []string{"PauseBuilds", `RegistryRestart REGISTRY_STORAGE_MAINTENANCE_READONLY={"enabled":true}`,
			"RegistryJob registry garbage-collect --delete-untagged", "RegistryRestart ", "ResumeBuilds"}
		if len(seq) != len(want) {
			t.Fatalf("fail=%v: sequence %q, want %q", fail, seq, want)
		}
		for i := range want {
			if !strings.HasPrefix(seq[i], want[i]) {
				t.Fatalf("fail=%v: step %d is %q, want %q (sequence %q)", fail, i, seq[i], want[i], seq)
			}
		}
	}
}

// seedImages creates the WP2a tables retention reads and fills them. Each
// image is "svc:dep:status:createdAt".
func seedImages(t *testing.T, e *testEnv, rows ...[4]any) {
	t.Helper()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS o_deployments (id TEXT PRIMARY KEY, status TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS o_images (deployment_id TEXT, service_id TEXT, ref TEXT, created_at INTEGER, deleted_at INTEGER NOT NULL DEFAULT 0)`,
	} {
		if _, err := e.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range rows {
		svc, dep, status, at := r[0].(string), r[1].(string), r[2].(string), r[3].(int)
		if _, err := e.db.Exec(`INSERT INTO o_deployments (id, status) VALUES (?, ?)`, dep, status); err != nil {
			t.Fatal(err)
		}
		if _, err := e.db.Exec(`INSERT INTO o_images (deployment_id, service_id, ref, created_at) VALUES (?, ?, ?, ?)`,
			dep, svc, svc+":"+dep, at); err != nil {
			t.Fatal(err)
		}
	}
}

func candidateDeps(t *testing.T, e *testEnv, keep int) []string {
	t.Helper()
	got, err := e.d.retentionCandidates(e.ctx(), keep)
	if err != nil {
		t.Fatal(err)
	}
	var deps []string
	for _, c := range got {
		deps = append(deps, c.deploymentID)
	}
	return deps
}

// TestRegistryRetentionKeepsTheNewestPerService checks 9.19: the newest keep
// images of each service stay, and so does any image a live deployment uses,
// however old.
func TestRegistryRetentionKeepsTheNewestPerService(t *testing.T) {
	e := newEnv(t)
	seedImages(t, e,
		[4]any{"svc_a", "dep_a1", "SUCCESS", 1}, // oldest, but live: protected
		[4]any{"svc_a", "dep_a2", "REMOVED", 2},
		[4]any{"svc_a", "dep_a3", "REMOVED", 3},
		[4]any{"svc_a", "dep_a4", "REMOVED", 4},
		[4]any{"svc_b", "dep_b1", "REMOVED", 1},
	)
	if got := strings.Join(candidateDeps(t, e, 2), ","); got != "dep_a2" {
		t.Fatalf("keep 2 removes %q, want dep_a2 only (a4 and a3 are newest, a1 is live, b has one image)", got)
	}
	if got := candidateDeps(t, e, 1); strings.Join(got, ",") != "dep_a3,dep_a2" {
		t.Fatalf("keep 1 removes %q, want dep_a3,dep_a2", got)
	}
}

// TestRegistryKeepAllRemovesNoImage: an owner who keeps every image gets a
// collection of untagged data only, never a removed tag.
func TestRegistryKeepAllRemovesNoImage(t *testing.T) {
	e := newEnv(t)
	seedImages(t, e,
		[4]any{"svc_a", "dep_a1", "REMOVED", 1},
		[4]any{"svc_a", "dep_a2", "REMOVED", 2},
		[4]any{"svc_a", "dep_a3", "REMOVED", 3},
	)
	if err := e.d.RunRegistryGC(e.ctx(), 0); err != nil {
		t.Fatal(err)
	}
	collected := false
	for _, c := range e.run.Calls() {
		if strings.Contains(c, "_manifests/tags") {
			t.Fatalf("keep-all removed a tag: %q", c)
		}
		if strings.Contains(c, "garbage-collect") {
			collected = true
		}
	}
	if !collected {
		t.Fatal("keep-all should still collect untagged data")
	}
}

func TestOverCapKeep(t *testing.T) {
	for _, c := range []struct{ keep, want int }{
		{0, 0}, // keep-all is obeyed even over the cap
		{1, 1}, // already below the cap retention
		{2, 2}, // the default stays
		{3, 3},
		{10, 3}, // a generous setting drops to 3 while over the cap
	} {
		if got := overCapKeep(c.keep); got != c.want {
			t.Errorf("overCapKeep(%d) = %d, want %d", c.keep, got, c.want)
		}
	}
}

// TestRetentionChangeCollectsAtOnce: lowering the setting frees disk at the
// next maintenance tick instead of waiting for 04:00, and only once.
func TestRetentionChangeCollectsAtOnce(t *testing.T) {
	e := newEnv(t)
	gcRuns := func() int {
		n := 0
		for _, c := range e.run.Calls() {
			if strings.Contains(c, "garbage-collect") {
				n++
			}
		}
		return n
	}
	tick := func() {
		if err := e.d.maybeRegistryMaintenance(e.ctx()); err != nil {
			t.Fatal(err)
		}
		e.wait()
	}

	tick() // first start of this version: the setting has never been applied
	if gcRuns() != 1 {
		t.Fatalf("first tick ran %d collections, want 1", gcRuns())
	}
	tick()
	if gcRuns() != 1 {
		t.Fatalf("an unchanged setting collected again (%d runs)", gcRuns())
	}

	inst, _ := e.cfg.Install(e.ctx())
	inst.ImageRetention = 5
	e.cfg.SetInstall(inst)
	tick()
	if gcRuns() != 2 {
		t.Fatalf("a changed setting ran %d collections in total, want 2", gcRuns())
	}
}

// The first collection after a retention change runs as the control plane
// starts, often while its agent is still reconnecting (seen on a real host:
// "Host ... is offline"). A failure must be retried, spaced out, and the
// setting recorded as applied only once a collection succeeds.
func TestRetentionChangeRetriesAFailedCollection(t *testing.T) {
	e := newEnv(t)
	hostDown := true
	e.run.OnRegistryJob = func(cmd []string, env map[string]string) (proto.JobResult, error) {
		if hostDown {
			return proto.JobResult{}, errors.New("The job could not be sent to host depot-1: Host is offline.")
		}
		return proto.JobResult{}, nil
	}
	tick := func() {
		t.Helper()
		if err := e.d.maybeRegistryMaintenance(e.ctx()); err != nil {
			t.Fatal(err)
		}
		e.wait()
	}
	applied := func() string {
		v, _ := e.d.st.stateGet(e.ctx(), "registry_gc_keep")
		return v
	}
	attempts := func() int {
		n := 0
		for _, c := range e.run.Calls() {
			if strings.HasPrefix(c, "PauseBuilds") {
				n++
			}
		}
		return n
	}

	tick()
	if applied() != "" {
		t.Fatalf("a failed collection was recorded as applied (%q)", applied())
	}
	first := attempts()
	if first == 0 {
		t.Fatal("no collection was attempted")
	}

	e.clock.Add(time.Minute)
	tick()
	if attempts() != first {
		t.Fatal("a failed collection was retried after one minute; retries must be spaced")
	}

	hostDown = false
	e.clock.Add(keepRetryEvery)
	tick()
	if attempts() == first {
		t.Fatal("the failed collection was never retried")
	}
	if applied() != "2" {
		t.Fatalf("a successful retry was not recorded (applied %q)", applied())
	}

	// A new value is tried at once, without waiting for the retry spacing.
	before := attempts()
	inst, _ := e.cfg.Install(e.ctx())
	inst.ImageRetention = 4
	e.cfg.SetInstall(inst)
	tick()
	if attempts() == before {
		t.Fatal("a newly changed setting waited for the retry spacing")
	}
}

// garbage-collect must name the config of the pinned registry:3.0.0 image.
// With the 2.x path it exits with its usage text, and nothing on disk is ever
// freed (seen on a real host: a 68 GB registry after months of "weekly GC").
func TestRegistryGCUsesTheRegistry3ConfigPath(t *testing.T) {
	e := newEnv(t)
	if err := e.d.RunRegistryGC(e.ctx(), 2); err != nil {
		t.Fatal(err)
	}
	want := "garbage-collect --delete-untagged /etc/distribution/config.yml"
	for _, c := range e.run.Calls() {
		if strings.Contains(c, "garbage-collect") {
			if !strings.Contains(c, want) {
				t.Fatalf("garbage-collect ran as %q, want it to contain %q", c, want)
			}
			return
		}
	}
	t.Fatal("garbage-collect never ran")
}
