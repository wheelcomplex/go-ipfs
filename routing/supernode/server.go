package supernode

import (
	"fmt"

	context "github.com/jbenet/go-ipfs/Godeps/_workspace/src/code.google.com/p/go.net/context"
	proto "github.com/jbenet/go-ipfs/Godeps/_workspace/src/code.google.com/p/goprotobuf/proto"
	datastore "github.com/jbenet/go-ipfs/Godeps/_workspace/src/github.com/jbenet/go-datastore"
	peer "github.com/jbenet/go-ipfs/p2p/peer"
	dhtpb "github.com/jbenet/go-ipfs/routing/dht/pb"
	proxy "github.com/jbenet/go-ipfs/routing/supernode/proxy"
	util "github.com/jbenet/go-ipfs/util"
	errors "github.com/jbenet/go-ipfs/util/debugerror"
)

// Server handles routing queries using a database backend
type Server struct {
	local           peer.ID
	routingBackend  datastore.ThreadSafeDatastore
	peerstore       peer.Peerstore
	*proxy.Loopback // so server can be injected into client
}

// NewServer creates a new Supernode routing Server
func NewServer(ds datastore.ThreadSafeDatastore, ps peer.Peerstore, local peer.ID) (*Server, error) {
	s := &Server{local, ds, ps, nil}
	s.Loopback = &proxy.Loopback{
		Handler: s,
		Local:   local,
	}
	return s, nil
}

// HandleLocalRequest implements the proxy.RequestHandler interface. This is
// where requests are received from the outside world.
func (s *Server) HandleRequest(ctx context.Context, p peer.ID, req *dhtpb.Message) *dhtpb.Message {
	_, response := s.handleMessage(ctx, p, req) // ignore response peer. it's local.
	return response
}

func (s *Server) handleMessage(
	ctx context.Context, p peer.ID, req *dhtpb.Message) (peer.ID, *dhtpb.Message) {

	defer log.EventBegin(ctx, "routingMessageReceived", req, p).Done()

	var response = dhtpb.NewMessage(req.GetType(), req.GetKey(), req.GetClusterLevel())
	switch req.GetType() {

	case dhtpb.Message_GET_VALUE:
		rawRecord, err := getRoutingRecord(s.routingBackend, util.Key(req.GetKey()))
		if err != nil {
			return "", nil
		}
		response.Record = rawRecord
		return p, response

	case dhtpb.Message_PUT_VALUE:
		// TODO before merging: verifyRecord(req.GetRecord())
		putRoutingRecord(s.routingBackend, util.Key(req.GetKey()), req.GetRecord())
		return p, req

	case dhtpb.Message_FIND_NODE:
		p := s.peerstore.PeerInfo(peer.ID(req.GetKey()))
		pri := []dhtpb.PeerRoutingInfo{
			dhtpb.PeerRoutingInfo{
				PeerInfo: p,
				// Connectedness: TODO
			},
		}
		response.CloserPeers = dhtpb.PeerRoutingInfosToPBPeers(pri)
		return p.ID, response

	case dhtpb.Message_ADD_PROVIDER:
		storeProvidersToPeerstore(s.peerstore, p, req.GetProviderPeers())

		if err := putRoutingProviders(s.routingBackend, util.Key(req.GetKey()), req.GetProviderPeers()); err != nil {
			return "", nil
		}
		return "", nil

	case dhtpb.Message_GET_PROVIDERS:
		providers, err := getRoutingProviders(s.routingBackend, util.Key(req.GetKey()))
		if err != nil {
			return "", nil
		}
		response.ProviderPeers = providers
		return p, response

	case dhtpb.Message_PING:
		return p, req
	default:
	}
	return "", nil
}

var _ proxy.RequestHandler = &Server{}
var _ proxy.Proxy = &Server{}

func getRoutingRecord(ds datastore.Datastore, k util.Key) (*dhtpb.Record, error) {
	dskey := k.DsKey()
	val, err := ds.Get(dskey)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	recordBytes, ok := val.([]byte)
	if !ok {
		return nil, fmt.Errorf("datastore had non byte-slice value for %v", dskey)
	}
	var record dhtpb.Record
	if err := proto.Unmarshal(recordBytes, &record); err != nil {
		return nil, errors.New("failed to unmarshal dht record from datastore")
	}
	return &record, nil
}

func putRoutingRecord(ds datastore.Datastore, k util.Key, value *dhtpb.Record) error {
	data, err := proto.Marshal(value)
	if err != nil {
		return err
	}
	dskey := k.DsKey()
	// TODO namespace
	if err := ds.Put(dskey, data); err != nil {
		return err
	}
	return nil
}

func putRoutingProviders(ds datastore.Datastore, k util.Key, newRecords []*dhtpb.Message_Peer) error {
	log.Event(context.Background(), "putRoutingProviders", &k)
	oldRecords, err := getRoutingProviders(ds, k)
	if err != nil {
		return err
	}
	mergedRecords := make(map[string]*dhtpb.Message_Peer)
	for _, provider := range oldRecords {
		mergedRecords[provider.GetId()] = provider // add original records
	}
	for _, provider := range newRecords {
		mergedRecords[provider.GetId()] = provider // overwrite old record if new exists
	}
	var protomsg dhtpb.Message
	protomsg.ProviderPeers = make([]*dhtpb.Message_Peer, 0, len(mergedRecords))
	for _, provider := range mergedRecords {
		protomsg.ProviderPeers = append(protomsg.ProviderPeers, provider)
	}
	data, err := proto.Marshal(&protomsg)
	if err != nil {
		return err
	}
	return ds.Put(providerKey(k), data)
}

func storeProvidersToPeerstore(ps peer.Peerstore, p peer.ID, providers []*dhtpb.Message_Peer) {
	for _, provider := range providers {
		providerID := peer.ID(provider.GetId())
		if providerID != p {
			log.Errorf("provider message came from third-party %s", p)
			continue
		}
		for _, maddr := range provider.Addresses() {
			// as a router, we want to store addresses for peers who have provided
			ps.AddAddr(p, maddr, peer.AddressTTL)
		}
	}
}

func getRoutingProviders(ds datastore.Datastore, k util.Key) ([]*dhtpb.Message_Peer, error) {
	e := log.EventBegin(context.Background(), "getProviders", &k)
	defer e.Done()
	var providers []*dhtpb.Message_Peer
	if v, err := ds.Get(providerKey(k)); err == nil {
		if data, ok := v.([]byte); ok {
			var msg dhtpb.Message
			if err := proto.Unmarshal(data, &msg); err != nil {
				return nil, err
			}
			providers = append(providers, msg.GetProviderPeers()...)
		}
	}
	return providers, nil
}

func providerKey(k util.Key) datastore.Key {
	return datastore.KeyWithNamespaces([]string{"routing", "providers", k.String()})
}
