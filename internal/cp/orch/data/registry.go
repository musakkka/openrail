// SPDX-License-Identifier: AGPL-3.0-only

package data

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/musakkka/openrail/internal/cp/cpapi"
	"github.com/musakkka/openrail/pkg/api"
)

// Registry retention and garbage collection (docs/SPEC.md 9.6, 6.6, 9.19).
//
// Retention keeps the newest Install.ImageRetention images per service
// (default 2; 0 keeps every image; at most 3 while the registry is above its
// size cap unless every image is kept) and never removes an image a live or
// in-flight deployment uses. It removes tags; garbage collection then deletes
// the manifests and blobs nothing refers to, including old build cache
// manifests (the cache tag is overwritten on every build).
//
// Garbage collection runs daily at 04:00 (control host time), as soon as the
// retention setting changes (including the first start of a version that
// reads it), and whenever the registry passes its cap: pause the build queue and wait for
// pushes to end, switch the registry to read-only, run
// `registry garbage-collect --delete-untagged`, switch it back, resume.
// Pulls keep working throughout.
//
// Runner.RegistryJob convention (see README.md): a call with a command execs
// it in the registry container; a call with no command and an env map
// restarts the registry with those extra variables (an empty map restores
// the defaults).

const (
	keepImagesOverCap = 3
	registryRepos     = "/var/lib/registry/docker/registry/v2/repositories"
	registryData      = "/var/lib/registry"
	// registryConfig is where registry 3.x reads its config. The 2.x path
	// (/etc/docker/registry/config.yml) does not exist in the pinned
	// registry:3.0.0 image, so garbage-collect exited at once with its usage
	// text and no blob was ever deleted. It must match the agent's
	// registryConfigPath (internal/agent/core/registry.go).
	registryConfig = "/etc/distribution/config.yml"
	readOnlyVar    = "REGISTRY_STORAGE_MAINTENANCE_READONLY"
	defaultCapPct  = 30
	// exitNotFilesystem is what the untag script returns when the registry
	// stores images in the bucket.
	exitNotFilesystem = 3
)

// liveStatuses are deployment statuses whose image must stay.
var liveStatuses = []string{"QUEUED", "WAITING", "BUILDING", "DEPLOYING", "SUCCESS", "CRASHED", "COMPLETED", "REMOVING"}

var registryName = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)

// registryKeep is the install's retention setting: images kept per service,
// 0 for every image.
func (d *Data) registryKeep(ctx context.Context) int {
	inst, err := d.cfg.Install(ctx)
	if err != nil || inst.ImageRetention < 0 {
		return defaultKeep
	}
	return inst.ImageRetention
}

// defaultKeep applies when the install settings cannot be read. It matches
// core.DefaultImageRetention.
const defaultKeep = 2

// keepRetryEvery spaces retries of a collection after a retention change.
const keepRetryEvery = 10 * time.Minute

// keepRetryDue reports whether a collection for retention keepKey may be
// attempted now. Only retries of the same value are spaced: a newly changed
// setting is always tried at once.
func (d *Data) keepRetryDue(ctx context.Context, keepKey string) bool {
	last, _ := d.st.stateGet(ctx, "registry_gc_keep_tried")
	value, at, ok := strings.Cut(last, "@")
	if !ok || value != keepKey {
		return true
	}
	ms, err := strconv.ParseInt(at, 10, 64)
	return err != nil || d.now().Sub(time.UnixMilli(ms)) >= keepRetryEvery
}

// overCapKeep is the retention used while the registry is above its cap. An
// owner who chose to keep every image is obeyed; the cap then only triggers
// collection of untagged data.
func overCapKeep(keep int) int {
	if keep == 0 || keep <= keepImagesOverCap {
		return keep
	}
	return keepImagesOverCap
}

// maybeRegistryMaintenance runs the daily garbage collection, a collection
// whenever the retention setting changes, and once an hour the size cap check.
func (d *Data) maybeRegistryMaintenance(ctx context.Context) error {
	keep := d.registryKeep(ctx)

	// A changed setting applies now rather than at the next 04:00, so lowering
	// it frees disk straight away. The value is recorded only once a
	// collection succeeds: the first attempt runs as the control plane starts,
	// often while its agent is still reconnecting, and recording it up front
	// left that failure in place until the next day. Retries are spaced by
	// keepRetryEvery so a broken registry does not pause builds every minute.
	keepKey := strconv.Itoa(keep)
	if last, _ := d.st.stateGet(ctx, "registry_gc_keep"); last != keepKey && d.keepRetryDue(ctx, keepKey) {
		if err := d.st.stateSet(ctx, "registry_gc_keep_tried", keepKey+"@"+strconv.FormatInt(d.now().UnixMilli(), 10)); err != nil {
			return err
		}
		d.background("registry gc (retention changed)", func(bg context.Context) {
			if err := d.RunRegistryGC(bg, keep); err != nil {
				d.log.Warn("registry garbage collection after a retention change; will retry", "err", err, "in", keepRetryEvery)
				return
			}
			if err := d.st.stateSet(bg, "registry_gc_keep", keepKey); err != nil {
				d.log.Warn("could not record the applied image retention", "err", err)
			}
		})
		return nil
	}

	now := d.now().Local()
	if now.Hour() == 4 {
		key := now.Format("2006-01-02")
		if last, _ := d.st.stateGet(ctx, "registry_gc_day"); last != key {
			if err := d.st.stateSet(ctx, "registry_gc_day", key); err != nil {
				return err
			}
			d.background("registry gc", func(bg context.Context) {
				if err := d.RunRegistryGC(bg, keep); err != nil {
					d.log.Warn("daily registry garbage collection", "err", err)
				}
			})
			return nil
		}
	}
	if now.Minute() != 30 {
		return nil
	}
	key := now.UTC().Format("2006-01-02T15")
	if last, _ := d.st.stateGet(ctx, "registry_cap_hour"); last == key {
		return nil
	}
	if err := d.st.stateSet(ctx, "registry_cap_hour", key); err != nil {
		return err
	}
	over, used, limit, err := d.registryOverCap(ctx)
	if err != nil || !over {
		return err
	}
	capKeep := overCapKeep(keep)
	if capKeep == 0 {
		d.log.Warn("registry above its size cap, but every image is kept by setting; only untagged data can be collected",
			"usedBytes", used, "capBytes", limit)
	} else {
		d.log.Warn("registry above its size cap", "usedBytes", used, "capBytes", limit, "keep", capKeep)
	}
	d.background("registry gc (cap)", func(bg context.Context) {
		if err := d.RunRegistryGC(bg, capKeep); err != nil {
			d.log.Warn("registry garbage collection above the cap", "err", err)
		}
	})
	return nil
}

// registryOverCap measures the registry against its cap (a share of the
// control host filesystem, Settings > General).
func (d *Data) registryOverCap(ctx context.Context) (bool, int64, int64, error) {
	pct := defaultCapPct
	if inst, err := d.cfg.Install(ctx); err == nil && inst.RegistryCapPct > 0 {
		pct = inst.RegistryCapPct
	}
	res, err := d.run.RegistryJob(ctx, []string{"sh", "-c", "du -sk " + registryData + " | cut -f1; df -kP " + registryData + " | tail -n 1"}, nil)
	if err != nil {
		return false, 0, 0, err
	}
	if res.ExitCode != 0 {
		return false, 0, 0, fmt.Errorf("measuring the registry failed with exit code %d", res.ExitCode)
	}
	used, total, err := parseDuDf(res.Output)
	if err != nil {
		return false, 0, 0, err
	}
	limit := total * int64(pct) / 100
	return used > limit, used, limit, nil
}

// parseDuDf reads "du -sk" then a "df -kP" line, returning bytes.
func parseDuDf(out string) (used, total int64, err error) {
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 2 {
		return 0, 0, errors.New("the registry size could not be read")
	}
	u, err1 := strconv.ParseInt(strings.Fields(lines[0])[0], 10, 64)
	f := strings.Fields(lines[len(lines)-1])
	if err1 != nil || len(f) < 2 {
		return 0, 0, errors.New("the registry size could not be read")
	}
	t, err2 := strconv.ParseInt(f[1], 10, 64)
	if err2 != nil {
		return 0, 0, errors.New("the registry filesystem size could not be read")
	}
	return u * 1024, t * 1024, nil
}

// imageTag is one image in the internal registry.
type imageTag struct {
	deploymentID, serviceID, repo, tag string
}

// retentionCandidates lists images beyond the newest keep per service that
// no live or in-flight deployment uses. It reads WP2a's o_images and
// o_deployments.
func (d *Data) retentionCandidates(ctx context.Context, keep int) ([]imageTag, error) {
	if !d.st.tableExists(ctx, "o_images") {
		return nil, nil
	}
	protected := map[string]bool{}
	if d.st.tableExists(ctx, "o_deployments") {
		q := `SELECT id FROM o_deployments WHERE status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(liveStatuses)), ",") + `)`
		args := make([]any, len(liveStatuses))
		for i, s := range liveStatuses {
			args[i] = s
		}
		rows, err := d.st.db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				protected[id] = true
			}
		}
		rows.Close()
	}
	rows, err := d.st.db.QueryContext(ctx, `SELECT deployment_id, service_id, ref FROM o_images
		WHERE deleted_at = 0 ORDER BY service_id, created_at DESC, deployment_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []imageTag
	seen := map[string]int{}
	for rows.Next() {
		var depID, svcID, ref string
		if err := rows.Scan(&depID, &svcID, &ref); err != nil {
			return nil, err
		}
		repo, tag, ok := strings.Cut(ref, ":")
		if !ok || !strings.HasPrefix(repo, "svc_") || !registryName.MatchString(repo) || !registryName.MatchString(tag) {
			continue // not an image of the internal registry
		}
		seen[svcID]++
		if seen[svcID] <= keep || protected[depID] {
			continue
		}
		out = append(out, imageTag{deploymentID: depID, serviceID: svcID, repo: repo, tag: tag})
	}
	return out, rows.Err()
}

// untagScript removes tag directories from the registry's filesystem.
func untagScript(tags []imageTag) string {
	var b strings.Builder
	b.WriteString("set -eu\nR=" + registryRepos + "\n")
	fmt.Fprintf(&b, "if [ ! -d \"$R\" ]; then echo 'The registry does not keep images on its filesystem, so old images are not removed.' >&2; exit %d; fi\n", exitNotFilesystem)
	for _, t := range tags {
		fmt.Fprintf(&b, "rm -rf \"$R/%s/_manifests/tags/%s\"\n", t.repo, t.tag)
	}
	b.WriteString("echo '{\"ok\":true}'\n")
	return b.String()
}

// RunRegistryGC applies registry retention with keep images per service and
// runs garbage collection with the build queue paused and the registry
// read-only. keep 0 removes no image and only collects untagged data. Only
// one runs at a time.
func (d *Data) RunRegistryGC(ctx context.Context, keep int) error {
	if !d.gcMu.TryLock() {
		return errors.New("A registry garbage collection is already running.")
	}
	defer d.gcMu.Unlock()
	var tags []imageTag
	if keep > 0 {
		var err error
		if tags, err = d.retentionCandidates(ctx, keep); err != nil {
			return fmt.Errorf("The images to remove could not be listed: %w", err)
		}
	}

	resume, err := d.run.PauseBuilds(ctx)
	if err != nil {
		return fmt.Errorf("The build queue could not be paused for registry garbage collection: %w", err)
	}
	defer resume()

	if len(tags) > 0 {
		res, err := d.run.RegistryJob(ctx, []string{"sh", "-c", untagScript(tags)}, nil)
		switch {
		case err != nil:
			return fmt.Errorf("Old images could not be removed from the registry: %w", err)
		case res.ExitCode == exitNotFilesystem:
			d.log.Info("registry retention skipped: images are stored in the bucket")
		case res.ExitCode != 0:
			return errors.New(jobError("Removing old images from the registry", JobOutcome{Result: res}))
		default:
			now := d.now().UnixMilli()
			for _, t := range tags {
				_, _ = d.st.exec(ctx, `UPDATE o_images SET deleted_at = ? WHERE deployment_id = ? AND deleted_at = 0`, now, t.deploymentID)
			}
		}
	}

	if _, err := d.run.RegistryJob(ctx, nil, map[string]string{readOnlyVar: `{"enabled":true}`}); err != nil {
		return fmt.Errorf("The registry could not be switched to read-only for garbage collection: %w", err)
	}
	gcRes, gcErr := d.run.RegistryJob(ctx, []string{"registry", "garbage-collect", "--delete-untagged", registryConfig}, nil)
	// Always switch back, even when the collection failed.
	_, backErr := d.run.RegistryJob(context.WithoutCancel(ctx), nil, map[string]string{})
	switch {
	case gcErr != nil:
		return fmt.Errorf("Registry garbage collection could not run: %w", gcErr)
	case gcRes.ExitCode != 0 || gcRes.Error != "":
		return errors.New(jobError("Registry garbage collection", JobOutcome{Result: gcRes}))
	case backErr != nil:
		return fmt.Errorf("Garbage collection finished, but the registry could not be switched back to read-write: %w", backErr)
	}
	_ = d.st.stateSet(ctx, "registry_gc_at", strconv.FormatInt(d.now().UnixMilli(), 10))
	return nil
}

// ---------------------------------------------------------------- monthly restore test

// restoreTestHour is the hour (UTC) of the first Sunday the test starts at,
// after the daily 03:00 base backups.
const restoreTestHour = 5

// firstSunday reports whether t (UTC) falls on the first Sunday of its month.
func firstSunday(t time.Time) bool {
	return t.Weekday() == time.Sunday && t.Day() <= 7
}

func (d *Data) maybeRunRestoreTests(ctx context.Context) error {
	now := d.now().UTC()
	if !firstSunday(now) || now.Hour() < restoreTestHour {
		return nil
	}
	month := now.Format("2006-01")
	if last, _ := d.st.stateGet(ctx, "restore_test_month"); last == month {
		return nil
	}
	if _, configured, err := d.bucket(ctx); err != nil || !configured {
		return err
	}
	// Record the month first: a restart during the test never repeats it.
	if err := d.st.stateSet(ctx, "restore_test_month", month); err != nil {
		return err
	}
	d.background("restore test", func(bg context.Context) {
		if err := d.RunRestoreTests(bg); err != nil {
			d.log.Warn("monthly restore test", "err", err)
		}
	})
	return nil
}

// RunRestoreTests restores the newest backup of each database into a
// throwaway container on the database's host, runs the template's check,
// records verified_at and removes the container (docs/SPEC.md 9.13). It
// returns the failures joined; each one has also fired backup.failed.
func (d *Data) RunRestoreTests(ctx context.Context) error {
	b, configured, err := d.bucket(ctx)
	if err != nil {
		return err
	}
	if !configured {
		return errors.New("Set a backup bucket in Settings > Backups first.")
	}
	snaps, err := d.cfg.AllInstances(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, snap := range snaps {
		if ctx.Err() != nil {
			break
		}
		info := d.recipe(snap)
		if !info.supported() || info.data == "" || d.isUnpromotedCopy(ctx, snap.EnvID, snap.ServiceID) {
			continue
		}
		if err := d.restoreTest(ctx, snap, info, b); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", snap.Config.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (d *Data) restoreTest(ctx context.Context, snap cpapi.ServiceSnapshot, info recipeInfo, b api.BackupSettings) error {
	rows, err := d.listBackups(ctx, snap.EnvID, snap.ServiceID, 50)
	if err != nil {
		return err
	}
	var bk *backupRow
	for i := range rows {
		if rows[i].Status == "ok" && rows[i].StoreID != "" {
			bk = &rows[i]
			break
		}
	}
	if bk == nil {
		return nil // nothing to test yet; backup health reports it
	}
	host, _ := d.run.VolumeHost(ctx, snap.EnvID, snap.ServiceID)
	if host == "" {
		if host, err = d.run.PlaceJob(ctx, snap.EnvID, []string{"data"}); err != nil {
			return err
		}
	}
	// A throwaway volume named like a deleted one whose 48 hours are over,
	// so the agent's janitor removes it after the test.
	vol := fmt.Sprintf("5050-trash-restoretest%s-%d", strings.TrimPrefix(newID("x"), "x_"), d.now().Add(-49*time.Hour).Unix())
	out := d.restoreJob(ctx, restoreSpec{
		src: snap, info: info, bucket: b, backup: *bk, into: snap, host: host,
		volume: vol, createVolume: true, check: true, kind: "restore_test",
	})
	if !out.ok() {
		msg := jobError("The monthly restore test", out)
		d.notifyBackupFailed(ctx, snap, bk.ID, fmt.Sprintf("The monthly restore test failed for %s. %s", snap.Config.Name, msg))
		return errors.New(msg)
	}
	_, err = d.st.exec(ctx, `UPDATE o_backups SET verified_at = ? WHERE id = ?`, d.now().UnixMilli(), bk.ID)
	return err
}
