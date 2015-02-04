// Package dht implements a distributed hash table that satisfies the ipfs routing
// interface. This DHT is modeled after kademlia with Coral and S/Kademlia modifications.
package dht

import (
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	peer "github.com/jbenet/go-ipfs/p2p/peer"
	routing "github.com/jbenet/go-ipfs/routing"
	u "github.com/jbenet/go-ipfs/util"

	context "github.com/jbenet/go-ipfs/Godeps/_workspace/src/code.google.com/p/go.net/context"
	goprocess "github.com/jbenet/go-ipfs/Godeps/_workspace/src/github.com/jbenet/goprocess"
	periodicproc "github.com/jbenet/go-ipfs/Godeps/_workspace/src/github.com/jbenet/goprocess/periodic"
)

// BootstrapConfig specifies parameters used bootstrapping the DHT.
//
// Note there is a tradeoff between the bootstrap period and the
// number of queries. We could support a higher period with less
// queries.
type BootstrapConfig struct {
	Queries int           // how many queries to run per period
	Period  time.Duration // how often to run periodi cbootstrap.
	Timeout time.Duration // how long to wait for a bootstrao query to run
}

var DefaultBootstrapConfig = BootstrapConfig{
	// For now, this is set to 1 query.
	// We are currently more interested in ensuring we have a properly formed
	// DHT than making sure our dht minimizes traffic. Once we are more certain
	// of our implementation's robustness, we should lower this down to 8 or 4.
	Queries: 1,

	// For now, this is set to 10 seconds, which is an aggressive period. We are
	// We are currently more interested in ensuring we have a properly formed
	// DHT than making sure our dht minimizes traffic. Once we are more certain
	// implementation's robustness, we should lower this down to 30s or 1m.
	Period: time.Duration(20 * time.Second),

	Timeout: time.Duration(20 * time.Second),
}

func (dht *IpfsDHT) Bootstrap(context.Context) error {
	// Bootstrap satisfies the routing interface
	return errors.New("TODO: perform DHT bootstrap")
}

// Bootstrap ensures the dht routing table remains healthy as peers come and go.
// it builds up a list of peers by requesting random peer IDs. The Bootstrap
// process will run a number of queries each time, and run every time signal fires.
// These parameters are configurable.
//
// Bootstrap returns a process, so the user can stop it.
func (dht *IpfsDHT) BootstrapWithConfig(config BootstrapConfig) (goprocess.Process, error) {
	sig := time.Tick(config.Period)
	return dht.BootstrapOnSignal(config, sig)
}

// SignalBootstrap ensures the dht routing table remains healthy as peers come and go.
// it builds up a list of peers by requesting random peer IDs. The Bootstrap
// process will run a number of queries each time, and run every time signal fires.
// These parameters are configurable.
//
// SignalBootstrap returns a process, so the user can stop it.
func (dht *IpfsDHT) BootstrapOnSignal(cfg BootstrapConfig, signal <-chan time.Time) (goprocess.Process, error) {
	if cfg.Queries <= 0 {
		return nil, fmt.Errorf("invalid number of queries: %d", cfg.Queries)
	}

	if signal == nil {
		return nil, fmt.Errorf("invalid signal: %v", signal)
	}

	proc := periodicproc.Ticker(signal, func(worker goprocess.Process) {
		// it would be useful to be able to send out signals of when we bootstrap, too...
		// maybe this is a good case for whole module event pub/sub?

		ctx := dht.Context()
		if err := dht.runBootstrap(ctx, cfg); err != nil {
			log.Warning(err)
			// A bootstrapping error is important to notice but not fatal.
		}
	})

	return proc, nil
}

// runBootstrap builds up list of peers by requesting random peer IDs
func (dht *IpfsDHT) runBootstrap(ctx context.Context, cfg BootstrapConfig) error {
	bslog := func(msg string) {
		log.Debugf("DHT %s dhtRunBootstrap %s -- routing table size: %d", dht.self, msg, dht.routingTable.Size())
	}
	bslog("start")
	defer bslog("end")
	defer log.EventBegin(ctx, "dhtRunBootstrap").Done()

	var merr u.MultiErr

	randomID := func() peer.ID {
		// 16 random bytes is not a valid peer id. it may be fine becuase
		// the dht will rehash to its own keyspace anyway.
		id := make([]byte, 16)
		rand.Read(id)
		id = u.Hash(id)
		return peer.ID(id)
	}

	// bootstrap sequentially, as results will compound
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	runQuery := func(ctx context.Context, id peer.ID) {
		p, err := dht.FindPeer(ctx, id)
		if err == routing.ErrNotFound {
			// this isn't an error. this is precisely what we expect.
		} else if err != nil {
			merr = append(merr, err)
		} else {
			// woah, actually found a peer with that ID? this shouldn't happen normally
			// (as the ID we use is not a real ID). this is an odd error worth logging.
			err := fmt.Errorf("Bootstrap peer error: Actually FOUND peer. (%s, %s)", id, p)
			log.Warningf("%s", err)
			merr = append(merr, err)
		}
	}

	sequential := true
	if sequential {
		// these should be parallel normally. but can make them sequential for debugging.
		// note that the core/bootstrap context deadline should be extended too for that.
		for i := 0; i < cfg.Queries; i++ {
			id := randomID()
			log.Debugf("Bootstrapping query (%d/%d) to random ID: %s", i+1, cfg.Queries, id)
			runQuery(ctx, id)
		}

	} else {
		// note on parallelism here: the context is passed in to the queries, so they
		// **should** exit when it exceeds, making this function exit on ctx cancel.
		// normally, we should be selecting on ctx.Done() here too, but this gets
		// complicated to do with WaitGroup, and doesnt wait for the children to exit.
		var wg sync.WaitGroup
		for i := 0; i < cfg.Queries; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				id := randomID()
				log.Debugf("Bootstrapping query (%d/%d) to random ID: %s", i+1, cfg.Queries, id)
				runQuery(ctx, id)
			}()
		}
		wg.Wait()
	}

	if len(merr) > 0 {
		return merr
	}
	return nil
}
