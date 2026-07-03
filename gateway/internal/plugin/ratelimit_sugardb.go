package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/echovault/sugardb/sugardb"
)

// sugarDBStore backs rate_limiting's "distributed" mode with an embedded
// SugarDB instance (github.com/echovault/sugardb), shared across gateway
// instances configured into the same cluster - see the README for the
// DEALER_RATELIMIT_CLUSTER_* environment variables and their current
// limitations. Each key is a simple INCR-then-EXPIRE fixed 1-second
// window counter: unlike memoryStore's continuous token bucket, this
// allows up to burst requests per whole second rather than smoothly
// replenishing requestsPerSecond tokens - a deliberate simplification, since
// a true distributed token bucket needs atomic read-modify-write scripting
// that SugarDB's embedded command API doesn't expose.
type sugarDBStore struct {
	db *sugardb.SugarDB
}

func (s *sugarDBStore) allow(key string, requestsPerSecond, burst float64, now time.Time) bool {
	limit := int(burst)
	if limit < 1 {
		limit = 1
	}

	count, err := s.db.Incr(key)
	if err != nil {
		return false
	}
	if count == 1 {
		if _, err := s.db.Expire(key, 1); err != nil {
			return false
		}
	}
	return count <= limit
}

// sugarDBNodeConfig is the subset of SugarDB's configuration Dealer exposes
// for embedding, whether for a standalone instance or one node of a real
// RAFT/memberlist cluster.
type sugarDBNodeConfig struct {
	bindAddr      string
	joinAddr      string
	bootstrap     bool
	serverID      string
	dataDir       string
	discoveryPort uint16
}

// newSugarDBInstance starts one embedded SugarDB node from nc. Kept
// separate from getSharedSugarDBStore (which reads its configuration from
// the environment) so cluster-joining behavior can be exercised directly
// in tests without needing separate OS processes/env vars per node.
func newSugarDBInstance(nc sugarDBNodeConfig) (*sugarDBStore, error) {
	cfg := sugardb.DefaultConfig()
	if nc.bindAddr != "" {
		cfg.BindAddr = nc.bindAddr
	}
	if nc.joinAddr != "" {
		cfg.JoinAddr = nc.joinAddr
	}
	cfg.BootstrapCluster = nc.bootstrap
	if nc.serverID != "" {
		cfg.ServerID = nc.serverID
	}
	if nc.discoveryPort != 0 {
		cfg.DiscoveryPort = nc.discoveryPort
	}

	dataDir := nc.dataDir
	if dataDir == "" {
		dir, err := os.MkdirTemp("", "dealer-ratelimit-")
		if err != nil {
			return nil, fmt.Errorf("create data dir: %w", err)
		}
		dataDir = dir
	}
	cfg.DataDir = dataDir

	db, err := sugardb.NewSugarDB(sugardb.WithConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("start embedded SugarDB: %w", err)
	}
	return &sugarDBStore{db: db}, nil
}

var (
	sharedSugarDBOnce  sync.Once
	sharedSugarDB      *sugarDBStore
	sharedSugarDBSetup error
)

// getSharedSugarDBStore lazily starts one process-wide embedded SugarDB
// instance on first use, shared by every rate_limiting plugin instance
// configured with mode: distributed. plugin.Build has no mechanism to
// inject shared infrastructure into a Factory, so this reads its
// configuration directly from the environment rather than threading a new
// field through every plugin's Build signature for a feature most
// installs won't use.
func getSharedSugarDBStore() (*sugarDBStore, error) {
	sharedSugarDBOnce.Do(func() {
		nc := sugarDBNodeConfig{
			bindAddr: os.Getenv("DEALER_RATELIMIT_CLUSTER_BIND_ADDR"),
			joinAddr: os.Getenv("DEALER_RATELIMIT_CLUSTER_JOIN_ADDR"),
			serverID: os.Getenv("DEALER_RATELIMIT_CLUSTER_SERVER_ID"),
			dataDir:  os.Getenv("DEALER_RATELIMIT_CLUSTER_DATA_DIR"),
		}
		if v := os.Getenv("DEALER_RATELIMIT_CLUSTER_BOOTSTRAP"); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				sharedSugarDBSetup = fmt.Errorf("invalid DEALER_RATELIMIT_CLUSTER_BOOTSTRAP %q: %w", v, err)
				return
			}
			nc.bootstrap = b
		}
		if v := os.Getenv("DEALER_RATELIMIT_CLUSTER_DISCOVERY_PORT"); v != "" {
			p, err := strconv.ParseUint(v, 10, 16)
			if err != nil {
				sharedSugarDBSetup = fmt.Errorf("invalid DEALER_RATELIMIT_CLUSTER_DISCOVERY_PORT %q: %w", v, err)
				return
			}
			nc.discoveryPort = uint16(p)
		}

		store, err := newSugarDBInstance(nc)
		if err != nil {
			sharedSugarDBSetup = err
			return
		}
		sharedSugarDB = store
	})
	return sharedSugarDB, sharedSugarDBSetup
}

// randomID returns a short random hex identifier used to namespace one
// rate_limiting plugin instance's keys in the shared distributed store.
func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
