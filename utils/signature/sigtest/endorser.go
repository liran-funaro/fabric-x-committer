/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sigtest

import (
	"math/big"
	"os"
	"path"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-lib-go/bccsp"
	"github.com/hyperledger/fabric-x-common/api/applicationpb"
	"github.com/hyperledger/fabric-x-common/msp"
	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/hyperledger/fabric-x-common/tools/cryptogen"

	"github.com/hyperledger/fabric-x-committer/utils/signature"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

// NsEndorser endorse a transaction's namespace.
// It converts a TX into a ASN1 message, and then uses the message endorser interface to endorse it.
// It also implements the endorser interface, which can endorse raw messages.
type NsEndorser struct {
	endorser
}

var dummyEndorsement = CreateEndorsementsForThresholdRule(make([]byte, 0))[0]

// NewNsEndorserFromKey creates a new NsEndorser according to the key and scheme.
func NewNsEndorserFromKey(scheme signature.Scheme, key []byte) (*NsEndorser, error) {
	var err error
	var e endorser
	switch strings.ToUpper(scheme) {
	case signature.NoScheme, "":
		e = nil
	case signature.Ecdsa:
		signingKey, parseErr := ParseSigningKey(key)
		err = parseErr
		e = &keyEndorser{signer: &ecdsaSigner{signingKey: signingKey}}
	case signature.Bls:
		sk := big.NewInt(0)
		sk.SetBytes(key)
		e = &keyEndorser{signer: &blsSigner{sk}}
	case signature.Eddsa:
		e = &keyEndorser{signer: &eddsaSigner{PrivateKey: key}}
	default:
		return nil, errors.Newf("scheme '%v' not supported", scheme)
	}
	return &NsEndorser{endorser: e}, err
}

// NewNsEndorserFromMsp creates a new NsEndorser using identities loaded from MSP directories.
// This endorser will create an endorsement for each MSP provided.
func NewNsEndorserFromMsp(certType int, mspDirs ...*msp.DirLoadParameters) (*NsEndorser, error) {
	identities, idErr := GetSigningIdentities(mspDirs...)
	if idErr != nil {
		return nil, idErr
	}
	e := &mspEndorser{
		certType:   certType,
		identities: identities,
		mspIDs:     make([][]byte, len(mspDirs)),
		certsBytes: make([][]byte, len(mspDirs)),
	}
	for i, id := range identities {
		e.mspIDs[i] = []byte(id.GetMSPIdentifier())
		serializedIDBytes, err := id.Serialize()
		if err != nil {
			return nil, errors.Wrap(err, "serializing default signing identity")
		}
		serializedID, err := protoutil.UnmarshalSerializedIdentity(serializedIDBytes)
		if err != nil {
			return nil, err
		}
		idBytes := serializedID.IdBytes
		if certType == test.CreatorID {
			idBytes, err = DigestPemContent(idBytes, bccsp.SHA256)
			if err != nil {
				return nil, err
			}
		}
		e.certsBytes[i] = idBytes
	}
	return &NsEndorser{endorser: e}, nil
}

// EndorseTxNs endorses a transaction's namespace.
func (v *NsEndorser) EndorseTxNs(txID string, tx *applicationpb.Tx, nsIdx int) (*applicationpb.Endorsements, error) {
	if nsIdx < 0 || nsIdx >= len(tx.Namespaces) {
		return nil, errors.New("namespace index out of range")
	}
	if v.endorser == nil {
		return dummyEndorsement, nil
	}
	msg, err := tx.Namespaces[nsIdx].ASN1Marshal(txID)
	if err != nil {
		return nil, err
	}
	return v.Endorse(msg)
}

// GetPeersIdentities returns the peers' identities from a crypto path.
func GetPeersIdentities(cryptoPath string) ([]msp.SigningIdentity, error) {
	return GetSigningIdentities(GetPeersMspDirs(cryptoPath)...)
}

// GetPeersMspDirs returns the peers' MSP directory path.
func GetPeersMspDirs(cryptoPath string) []*msp.DirLoadParameters {
	peerOrgPath := path.Join(cryptoPath, cryptogen.PeerOrganizationsDir)
	peerMspDirs := GetMspDirs(peerOrgPath)
	for _, mspItem := range peerMspDirs {
		clientName := "client@" + mspItem.MspName + ".com"
		mspItem.MspDir = path.Join(mspItem.MspDir, "users", clientName, "msp")
	}
	return peerMspDirs
}

// GetConsenterIdentities returns the orderer consenters' identities from a crypto path.
func GetConsenterIdentities(cryptoPath string) ([]msp.SigningIdentity, error) {
	ordererOrgPath := path.Join(cryptoPath, cryptogen.OrdererOrganizationsDir)
	ordererMspDirs := GetMspDirs(ordererOrgPath)
	for _, mspItem := range ordererMspDirs {
		nodeName := "consenter-" + mspItem.MspName[len("orderer-"):]
		mspItem.MspDir = path.Join(mspItem.MspDir, "orderers", nodeName, "msp")
	}
	return GetSigningIdentities(ordererMspDirs...)
}

// GetMspDirs returns an msp dir parameter per organization in the path.
func GetMspDirs(targetPath string) []*msp.DirLoadParameters {
	dir, err := os.ReadDir(targetPath)
	if err != nil {
		return nil
	}
	mspDirs := make([]*msp.DirLoadParameters, 0, len(dir))
	for _, dirEntry := range dir {
		if !dirEntry.IsDir() {
			continue
		}
		orgName := dirEntry.Name()
		mspDirs = append(mspDirs, &msp.DirLoadParameters{
			MspName: orgName,
			MspDir:  path.Join(targetPath, orgName),
		})
	}
	return mspDirs
}

// GetSigningIdentities loads signing identities from the given MSP directories.
func GetSigningIdentities(mspDirs ...*msp.DirLoadParameters) ([]msp.SigningIdentity, error) {
	identities := make([]msp.SigningIdentity, len(mspDirs))
	for i, mspDir := range mspDirs {
		localMsp, err := msp.LoadLocalMspDir(*mspDir)
		if err != nil {
			return nil, err
		}
		identities[i], err = localMsp.GetDefaultSigningIdentity()
		if err != nil {
			return nil, errors.Wrap(err, "loading signing identity")
		}
	}
	return identities, nil
}
