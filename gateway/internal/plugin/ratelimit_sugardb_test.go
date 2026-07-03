package plugin

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// freeDiscoveryPort grabs an OS-assigned free TCP port and immediately
// releases it, so a SugarDB node can bind its memberlist discovery
// listener there. Small TOCTOU race in principle; fine for a test.
func freeDiscoveryPort(t *testing.T) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	return uint16(ln.Addr().(*net.TCPAddr).Port)
}

func waitUntilSugarDB(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}

// TestSugarDBStore_TwoNodeClusterReplicatesCounters proves the actual
// recipe Dealer's DEALER_RATELIMIT_CLUSTER_* env vars produce - bootstrap
// one node, join a second to it via "<serverID>/<host>:<discoveryPort>" -
// really replicates a counter across two independent embedded SugarDB
// instances, not just that each one runs standalone.
func TestSugarDBStore_TwoNodeClusterReplicatesCounters(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real 2-node RAFT/memberlist cluster; skipped in -short mode")
	}
	if raceDetectorEnabled {
		// SugarDB's RAFT log store (hashicorp/raft-boltdb) uses the
		// old, unmaintained github.com/boltdb/bolt, whose unsafe
		// pointer conversions fail Go's -race checkptr validation with
		// a fatal (not recoverable) "converted pointer straddles
		// multiple allocations" - this only happens once RAFT actually
		// initializes (bootstrap/join), never in standalone mode.
		// Nothing in Dealer can fix a third-party library's pointer
		// handling; skip under -race rather than crash the whole test
		// binary.
		t.Skip("SugarDB's RAFT/boltdb path fails Go's -race checkptr check; see comment above")
	}

	portA := freeDiscoveryPort(t)

	nodeA, err := newSugarDBInstance(sugarDBNodeConfig{
		bindAddr:      "localhost",
		serverID:      "test-node-a",
		dataDir:       t.TempDir(),
		bootstrap:     true,
		discoveryPort: portA,
	})
	if err != nil {
		t.Fatalf("newSugarDBInstance(nodeA) error = %v", err)
	}

	nodeB, err := newSugarDBInstance(sugarDBNodeConfig{
		bindAddr:      "localhost",
		serverID:      "test-node-b",
		dataDir:       t.TempDir(),
		joinAddr:      fmt.Sprintf("test-node-a/localhost:%d", portA),
		discoveryPort: freeDiscoveryPort(t),
	})
	if err != nil {
		t.Fatalf("newSugarDBInstance(nodeB) error = %v", err)
	}

	// Bootstrapping a single-node cluster still goes through RAFT leader
	// election (~1s heartbeat timeout) before it can accept writes.
	waitUntilSugarDB(t, func() bool {
		return nodeA.db.GetServerInfo().Role == "master"
	})

	now := time.Now()
	if !nodeA.allow("shared-client", 1, 1, now) {
		t.Fatal("nodeA.allow() = false, want true for the first request against a fresh counter")
	}

	// Read (never Incr) from node B while waiting: allow() itself
	// increments, so polling with allow() would inflate the counter via
	// node B's own writes regardless of replication, masking the very
	// thing this test is supposed to prove.
	waitUntilSugarDB(t, func() bool {
		v, err := nodeB.db.Get("shared-client")
		return err == nil && v == "1"
	})

	// Only now does a further request against the replicated counter
	// correctly get denied by node B, without node B ever having
	// incremented it itself.
	if nodeB.allow("shared-client", 1, 1, now) {
		t.Fatal("nodeB.allow() = true, want false: the limit was already reached by node A and replicated to node B")
	}
}
