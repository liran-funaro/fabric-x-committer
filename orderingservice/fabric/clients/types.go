package clients

import ab "github.com/hyperledger/fabric-protos-go/orderer"

// Temporary solution for type checks

type BroadcastClient = ab.AtomicBroadcast_BroadcastClient
type BroadcastServer = ab.AtomicBroadcast_BroadcastServer
type DeliverClient = ab.AtomicBroadcast_DeliverClient
type DeliverServer = ab.AtomicBroadcast_DeliverServer
