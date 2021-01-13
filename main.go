package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	autonat "github.com/libp2p/go-libp2p-autonat"
	connmgr "github.com/libp2p/go-libp2p-connmgr"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	libp2pquic "github.com/libp2p/go-libp2p-quic-transport"
	routing "github.com/libp2p/go-libp2p-routing"

	libp2ptls "github.com/libp2p/go-libp2p-tls"
	"github.com/multiformats/go-multiaddr"
)

func main() {
	port := flag.Int("port", 6666, "port")
	flag.Parse()

	log.Println("启动引导节点", *port)

	//获取程序所在目录
	dir, e := filepath.Abs(filepath.Dir(os.Args[0]))
	if e != nil {
		log.Fatalln(e)
	}

	// 上下文控制libp2p节点的生命周期, 取消它可以停止节点.
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	// 生成或读取私密
	privateKeyPath := filepath.Join(dir, "private.key")
	var privateKey crypto.PrivKey
	var privateKeyBytes []byte
	_, e = os.Stat(privateKeyPath)
	if os.IsNotExist(e) {
		privateKey, _, e = crypto.GenerateKeyPair(
			crypto.Ed25519, // Select your key type. Ed25519 are nice short
			-1,             // Select key length when possible (i.e. RSA).
		)
		if e != nil {
			log.Fatalln(e)
		}
		privateKeyBytes, e = crypto.MarshalPrivateKey(privateKey)
		if e != nil {
			log.Fatalln(e)
		}
		e = ioutil.WriteFile(privateKeyPath, privateKeyBytes, os.ModePerm)
		if e != nil {
			log.Fatalln(e)
		}
	} else {
		privateKeyBytes, e = ioutil.ReadFile(privateKeyPath)
		if e != nil {
			log.Fatalln(e)
		}
		privateKey, e = crypto.UnmarshalPrivateKey(privateKeyBytes)
		if e != nil {
			log.Fatalln(e)
		}
	}

	var idht *dht.IpfsDHT
	h, e := libp2p.New(ctx,
		// Use the keypair we generated
		libp2p.Identity(privateKey),
		// Multiple listen addresses
		libp2p.ListenAddrStrings(
			fmt.Sprint("/ip4/0.0.0.0/tcp/", *port),          // regular tcp connections
			fmt.Sprint("/ip4/0.0.0.0/udp/", *port, "/quic"), // a UDP endpoint for the QUIC transport
		),
		// support TLS connections
		libp2p.Security(libp2ptls.ID, libp2ptls.New),
		// support QUIC - experimental
		libp2p.Transport(libp2pquic.NewTransport),
		// support any other default transports (TCP)
		libp2p.DefaultTransports,
		// Let's prevent our peer from having too many
		// connections by attaching a connection manager.
		libp2p.ConnectionManager(connmgr.NewConnManager(
			100,         // Lowwater
			400,         // HighWater,
			time.Minute, // GracePeriod
		)),
		// Attempt to open ports using uPNP for NATed hosts.
		libp2p.NATPortMap(),
		// Let this host use the DHT to find other hosts
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			idht, e = dht.New(ctx, h)
			return idht, e
		}),
		// Let this host use relays and advertise itself on relays if
		// it finds it is behind NAT. Use libp2p.Relay(options...) to
		// enable active relays and more.
		libp2p.EnableAutoRelay(),
	)
	if e != nil {
		log.Fatalln(e)
	}
	myAddrs, e := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()})
	if e != nil {
		log.Fatalln(e)
	}
	log.Println("我的地址:", myAddrs)

	// 创建自动NAT
	_, e = autonat.New(ctx, h)
	if e != nil {
		log.Fatalln(e)
	}

	// 连接引导节点
	multiAddr, e := multiaddr.NewMultiaddr("/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ")
	if e != nil {
		log.Fatalln(e)
	}
	addrInfo, e := peer.AddrInfoFromP2pAddr(multiAddr)
	if e != nil {
		log.Fatalln(e)
	}
	lc, lcCancel := context.WithTimeout(ctx, time.Second*16)
	defer lcCancel()
	e = h.Connect(lc, *addrInfo)
	if e != nil {
		log.Fatalln(e)
	}

	//显示节点数量
	go func() {
		ticker := time.NewTicker(time.Second * 10)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				log.Println("节点数量", len(h.Peerstore().Peers()))
			}
		}

	}()

	// wait for a SIGINT or SIGTERM signal
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan
	log.Println("收到信号, 关闭程序")
}
