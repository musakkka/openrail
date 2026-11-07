// SPDX-License-Identifier: AGPL-3.0-only

// Package assets embeds the files the agent mounts into app containers.
// Today that is cluster.cjs, the Node cluster wrapper behind "Use all cores"
// (docs/SPEC.md 8.1, 9.7). cluster.cjs itself is MIT licensed (see LICENSE
// in this directory); this Go file is AGPL-3.0-only like the rest of the
// agent.
package assets

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// CACertsName is the asset holding the host's CA bundle.
//
// Database images (postgres, redis) ship without ca-certificates, so WAL-G
// and restic inside them could not verify any HTTPS endpoint: every backup to
// a real S3 service (R2, AWS, B2) failed its TLS handshake and the SDK
// retried forever. Only plain-HTTP buckets ever worked. The control plane
// mounts this asset at CACertsTarget and points SSL_CERT_FILE at it.
const CACertsName = "ca-certificates.crt"

// CACertsTarget is where the CA bundle is mounted read-only in a container.
const CACertsTarget = "/opt/5050/ca-certificates.crt"

// hostCABundles are the system bundles tried in order (Debian/Ubuntu,
// RHEL/Fedora, Alpine/macOS-style).
var hostCABundles = []string{
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/pki/tls/certs/ca-bundle.crt",
	"/etc/ssl/cert.pem",
}

// hostCACerts returns the host's CA bundle. A host without one gets an empty
// file rather than none, so the mount still resolves; TLS then fails exactly
// as it did before this asset existed, never worse.
func hostCACerts() []byte {
	for _, p := range hostCABundles {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return b
		}
	}
	return []byte{}
}

// ClusterName is the asset name used in proto.Mount{Kind: "asset"}.
const ClusterName = "cluster.cjs"

// ClusterTarget is where cluster.cjs is mounted read-only in a container.
const ClusterTarget = "/opt/5050/cluster.cjs"

//go:embed cluster.cjs
var clusterCJS []byte

// Cluster returns the embedded cluster.cjs.
func Cluster() []byte { return bytes.Clone(clusterCJS) }

// Get returns an embedded asset by name.
func Get(name string) ([]byte, bool) {
	switch name {
	case ClusterName:
		return Cluster(), true
	case CACertsName:
		return hostCACerts(), true
	}
	return nil, false
}

// Names lists every asset.
func Names() []string { return []string{ClusterName, CACertsName} }

// WriteAll writes every asset into dir (normally <DataDir>/assets) and
// returns the path of each by name. A file is replaced only when its content
// changed, and always through a new file plus rename, so a running
// container that bind-mounted the old file keeps the copy it started with.
func WriteAll(dir string) (map[string]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("could not create the assets directory %s: %w", dir, err)
	}
	out := make(map[string]string, len(Names()))
	for _, name := range Names() {
		data, _ := Get(name)
		p := filepath.Join(dir, name)
		out[name] = p
		if cur, err := os.ReadFile(p); err == nil && bytes.Equal(cur, data) {
			continue
		}
		tmp, err := os.CreateTemp(dir, "."+name+".*")
		if err != nil {
			return nil, fmt.Errorf("could not write asset %s: %w", name, err)
		}
		_, werr := tmp.Write(data)
		cerr := tmp.Close()
		if werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = os.Chmod(tmp.Name(), 0o644)
		}
		if werr == nil {
			werr = os.Rename(tmp.Name(), p)
		}
		if werr != nil {
			_ = os.Remove(tmp.Name())
			return nil, fmt.Errorf("could not write asset %s: %w", name, werr)
		}
	}
	return out, nil
}
