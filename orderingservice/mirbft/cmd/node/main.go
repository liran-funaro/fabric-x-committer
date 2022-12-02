package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/filecoin-project/mir"
	"github.com/filecoin-project/mir/pkg/deploytest"
	"github.com/filecoin-project/mir/pkg/iss"
	"github.com/filecoin-project/mir/pkg/logging"
	"github.com/filecoin-project/mir/pkg/membership"
	"github.com/filecoin-project/mir/pkg/mempool/simplemempool"
	"github.com/filecoin-project/mir/pkg/net/libp2p"
	"github.com/filecoin-project/mir/pkg/systems/smr"
	t "github.com/filecoin-project/mir/pkg/types"
	"github.com/filecoin-project/mir/pkg/util/errstack"
	libp2p_util "github.com/filecoin-project/mir/pkg/util/libp2p"
	cb "github.com/hyperledger/fabric-protos-go/common"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/pkg/errors"
	"gopkg.in/alecthomas/kingpin.v2"
)

const ReqReceiverBasePort = 7050

func main() {
	if err := run(); err != nil {
		errstack.Println(err)
		os.Exit(1)
	}
}

func run() error {
	_ = parseArgs(os.Args)
	args := parseArgs(os.Args)

	var logger logging.Logger
	if args.Verbose {
		logger = logging.ConsoleDebugLogger // Print debug-level info in verbose mode.
	} else {
		logger = logging.ConsoleWarnLogger // Only print errors and warnings by default.
	}

	initialAddrs, err := membership.FromFileName(args.InitMembershipFile)
	if err != nil {
		return errors.Wrap(err, "could not load membership")
	}
	initialMembership, err := membership.DummyMultiAddrs(initialAddrs)
	if err != nil {
		return errors.Wrap(err, "could not create dummy multiaddrs")
	}

	portStr, err := getPortStr(initialMembership[args.OwnId])
	if err != nil {
		return fmt.Errorf("could not parse port from own address: %w", err)
	}

	addrStr := fmt.Sprintf("/ip4/127.0.0.1/tcp/%s", portStr)
	listenAddr, err := multiaddr.NewMultiaddr(addrStr)
	if err != nil {
		return fmt.Errorf("could not create listen address: %w", err)
	}

	ownNumericID, err := strconv.Atoi(string(args.OwnId))
	if err != nil {
		return errors.Wrap(err, "node IDs must be numeric in the sample app")
	}

	h, err := libp2p_util.NewDummyHostWithPrivKey(
		t.NodeAddress(libp2p_util.NewDummyMultiaddr(ownNumericID, listenAddr)),
		libp2p_util.NewDummyHostKey(ownNumericID),
	)
	if err != nil {
		return errors.Wrap(err, "failed to create libp2p host")
	}

	localCrypto := deploytest.NewLocalCryptoSystem("pseudo", membership.GetIDs(initialMembership), logger)

	blockChan := make(chan *cb.Block, 100)

	app := NewOrderingService(initialMembership, blockChan)

	smrParams := smr.Params{
		Mempool: &simplemempool.ModuleParams{
			MaxTransactionsInBatch: 1000,
		},
		Iss: iss.DefaultParams(initialMembership),
		Net: libp2p.DefaultParams(),
	}

	// quicker processing
	smrParams.Iss.MaxProposeDelay = 50 * time.Millisecond

	initialSnapshot, err := app.Snapshot()
	if err != nil {
		return errors.Wrap(err, "could not create initial snapshot")
	}
	genesis := smr.GenesisCheckpoint(initialSnapshot, smrParams)

	// Create a Mir SMR system.
	smrSystem, err := smr.New(
		args.OwnId,
		h,
		genesis,
		localCrypto.Crypto(args.OwnId),
		app,
		smrParams,
		logger,
	)
	if err != nil {
		return errors.Wrap(err, "could not create SMR system")
	}

	node, err := mir.NewNode(args.OwnId, mir.DefaultNodeConfig().WithLogger(logger), smrSystem.Modules(), nil, nil)
	if err != nil {
		return errors.Wrap(err, "could not create node")
	}

	//reqReceiver := requestreceiver.NewRequestReceiver(node, "mempool", logger)
	//if err := reqReceiver.Start(ReqReceiverBasePort + ownNumericID); err != nil {
	//	return fmt.Errorf("could not start request receiver: %w", err)
	//}
	//defer reqReceiver.Stop()

	fmt.Printf("starting smr...\n")
	if err := smrSystem.Start(); err != nil {
		return errors.Wrap(err, "could not start SMR system")
	}

	var wg sync.WaitGroup
	wg.Add(1)

	// Start the node in a separate goroutine
	var nodeErr error
	go func() {
		fmt.Printf("Run node ...\n")
		nodeErr = node.Run(context.Background())
		wg.Done()
	}()

	// fabric ordering service interface
	server := NewServer(fmt.Sprintf("localhost:%d", ReqReceiverBasePort+ownNumericID), node, blockChan)
	defer server.Stop()

	fmt.Printf("ready to go ...\n")
	wg.Wait()

	smrSystem.Stop()
	node.Stop()

	return nodeErr
}

type parsedArgs struct {

	// Numeric ID of this node.
	// The package github.com/hyperledger-labs/mirbft/pkg/types defines this and other types used by the library.
	OwnId t.NodeID

	InitMembershipFile string

	// If set, print verbose output to stdout.
	Verbose bool
}

func parseArgs(args []string) *parsedArgs {
	app := kingpin.New("chat-demo", "Small chat application to demonstrate the usage of the MirBFT library.")
	verbose := app.Flag("verbose", "Verbose mode.").Short('v').Bool()
	// Currently the type of the node ID is defined as uint64 by the /pkg/types package.
	// In case that changes, this line will need to be updated.
	ownId := app.Arg("id", "Numeric ID of this node").Required().String()

	initMembershipFile := app.Flag("init-membership", "File containing the initial system membership.").
		Short('i').Required().String()

	if _, err := app.Parse(args[1:]); err != nil { // Skip args[0], which is the name of the program, not an argument.
		app.FatalUsage("could not parse arguments: %v\n", err)
	}

	return &parsedArgs{
		OwnId:              t.NodeID(*ownId),
		InitMembershipFile: *initMembershipFile,
		Verbose:            *verbose,
	}
}

func getPortStr(address t.NodeAddress) (string, error) {
	_, addrStr, err := manet.DialArgs(address)
	if err != nil {
		return "", err
	}

	_, portStr, err := net.SplitHostPort(addrStr)
	if err != nil {
		return "", err
	}

	return portStr, nil
}
