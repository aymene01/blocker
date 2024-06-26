package node

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/aymene01/ledgerNet/pb"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/peer"
)

type Node struct {
	version    string
	listenAddr string
	logger     zap.SugaredLogger

	peerLock sync.RWMutex
	peers    map[pb.NodeClient]*pb.Version

	pb.UnimplementedNodeServer
}

func NewNode() *Node {
	logger, _ := getLoggerConfig()
	return &Node{
		peers:   make(map[pb.NodeClient]*pb.Version),
		version: "blocker-1",
		logger:  *logger.Sugar(),
	}
}

func (n *Node) Start(listenAddr string, bootstrapNodes []string) error {
	n.listenAddr = listenAddr

	var (
		opts       = []grpc.ServerOption{}
		grpcServer = grpc.NewServer(opts...)
	)

	ln, err := net.Listen("tcp", listenAddr)

	if err != nil {
		return err
	}

	pb.RegisterNodeServer(grpcServer, n)
	n.logger.Infow("node started ...", "port", listenAddr)

	// bootstrapNode
	if len(bootstrapNodes) > 0 {
		go n.bootstrapNetwork(bootstrapNodes)
	}
	return grpcServer.Serve(ln)
}

func (n *Node) Handshake(ctx context.Context, v *pb.Version) (*pb.Version, error) {
	c, err := makeNodeClient(v.ListenAddr)
	if err != nil {
		return nil, err
	}

	n.addPeer(c, v)

	return n.getVersion(), nil
}

func (n *Node) HandleTransaction(ctx context.Context, tx *pb.Transaction) (*pb.Ack, error) {
	peer, _ := peer.FromContext(ctx)
	fmt.Println("received tx from:", peer)

	return &pb.Ack{}, nil
}

func (n *Node) getVersion() *pb.Version {
	return &pb.Version{
		Version:    "blocker-0.1",
		Height:     0,
		ListenAddr: n.listenAddr,
		PeerList:   n.getPeerList(),
	}
}

func (n *Node) addPeer(c pb.NodeClient, v *pb.Version) {
	n.peerLock.Lock()
	defer n.peerLock.Unlock()

	// Handle the logic where we decide to
	// accept or drop the incomming node

	n.peers[c] = v

	// connect to the list of peers in the received list of peers

	if len(v.PeerList) > 0 {
		go n.bootstrapNetwork(v.PeerList)
	}
	n.logger.Debugw("new peer successfully connected",
		"we", n.listenAddr,
		"remoteNode", v.ListenAddr,
		"height", v.Height,
	)
}

func (n *Node) deletePeer(c pb.NodeClient) {
	n.peerLock.Lock()
	defer n.peerLock.Unlock()

	delete(n.peers, c)
}

func (n *Node) bootstrapNetwork(addrs []string) error {
	for _, addr := range addrs {
		if !n.canConnectWith(addr) {
			continue
		}

		n.logger.Debugw("dialing remote node", "we", n.listenAddr, "remote", addr)
		c, v, err := n.newRemoteClient(addr)
		if err != nil {
			return err
		}

		n.addPeer(c, v)
	}
	return nil
}

func (n *Node) newRemoteClient(addr string) (pb.NodeClient, *pb.Version, error) {
	c, err := makeNodeClient(addr)
	if err != nil {
		return nil, nil, err
	}

	v, err := c.Handshake(context.Background(), n.getVersion())
	if err != nil {
		return nil, nil, err
	}

	return c, v, nil
}

func (n *Node) canConnectWith(addr string) bool {
	if n.listenAddr == addr {
		return false
	}

	connectedPeers := n.getPeerList()

	for _, connectAddr := range connectedPeers {
		if addr == connectAddr {
			return false
		}
	}

	return true
}

func (n *Node) getPeerList() []string {
	n.peerLock.RLock()
	defer n.peerLock.RUnlock()

	peers := []string{}
	for _, version := range n.peers {
		peers = append(peers, version.ListenAddr)
	}

	return peers
}

func makeNodeClient(listenAddr string) (pb.NodeClient, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	c, err := grpc.NewClient(listenAddr, opts...)

	if err != nil {
		return nil, err
	}

	return pb.NewNodeClient(c), nil
}

func getPortNum(listenAddr string) (string, error) {
	values := strings.Split(listenAddr, ":")
	if len(values) != 2 {
		return "", errors.New("invalid listen value")
	}

	return values[1], nil
}

func getLoggerConfig() (*zap.Logger, error) {
	loggerConfig := zap.NewDevelopmentConfig()
	loggerConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	loggerConfig.EncoderConfig.ConsoleSeparator = " "
	loggerConfig.EncoderConfig.TimeKey = ""
	loggerConfig.DisableStacktrace = true
	return loggerConfig.Build()
}
