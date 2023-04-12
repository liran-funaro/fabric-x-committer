FROM golang:1.20.3-bullseye

## Check out fabric without building
ENV FABRIC_PATH=$GOPATH/src/github.com/hyperledger/fabric
RUN git clone https://github.com/hyperledger/fabric.git $FABRIC_PATH

RUN apt-get update && apt-get install -y tmux

# install just
RUN curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/bin

ENV OUT_PATH=/usr/local/out
ENV BINS_PATH=$FABRIC_PATH/build/bin

WORKDIR /usr/local

# Patch to fix RequestMaxBytes
ADD ordererbuilder/fix_RequestMaxBytes.patch .
# Patch to disable signatures
ADD ordererbuilder/orderer_no_sig_check.patch .
# Patch for boosting orderer performance
ADD ordererbuilder/orderer_booster.patch .
# Patch to accept MESSAGE as envelope type
ADD ordererbuilder/allow_MESSAGE_type.patch .
# Script to build binaries
ADD ordererbuilder/build_orderer_binaries.sh .
RUN chmod +x ./build_orderer_binaries.sh

CMD ["/bin/sh"]

