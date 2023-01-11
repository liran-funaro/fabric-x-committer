#!/bin/bash

listen_port=$1
ops_listen_address=$2
admin_listen_address=$3
orderer_credentials=$4
genesisblock_out=$5
ledger_dir=$6
orderer_config=$7

cp "$orderer_config" /usr/local/orderer.yaml

ORDERER_GENERAL_LOCALMSPID="Orderer" \
ORDERER_GENERAL_LOCALMSPDIR="$orderer_credentials/msp" \
ORDERER_GENERAL_LISTENADDRESS="0.0.0.0" \
ORDERER_GENERAL_LISTENPORT="$listen_port" \
ORDERER_GENERAL_GENESISFILE="$genesisblock_out" \
ORDERER_GENERAL_BOOTSTRAPFILE="$genesisblock_out" \
ORDERER_GENERAL_TLS_ENABLED="true" \
ORDERER_GENERAL_TLS_PRIVATEKEY="$orderer_credentials/tls/server.key" \
ORDERER_GENERAL_TLS_CERTIFICATE="$orderer_credentials/tls/server.crt" \
ORDERER_GENERAL_TLS_ROOTCAS="[$orderer_credentials/tls/ca.crt]" \
ORDERER_GENERAL_CLUSTER_CLIENTCERTIFICATE="$orderer_credentials/tls/server.crt" \
ORDERER_GENERAL_CLUSTER_CLIENTPRIVATEKEY="$orderer_credentials/tls/server.key" \
ORDERER_GENERAL_CLUSTER_ROOTCAS="[$orderer_credentials/tls/ca.crt]" \
ORDERER_OPERATIONS_LISTENADDRESS="$ops_listen_address" \
ORDERER_ADMIN_LISTENADDRESS="$admin_listen_address" \
ORDERER_FILELEDGER_LOCATION="$ledger_dir" \
ORDERER_CONSENSUS_WALDIR="$ledger_dir/etcdraft/wal" \
ORDERER_CONSENSUS_SNAPDIR="$ledger_dir/snapshot" \
"$BINS_PATH"/orderer