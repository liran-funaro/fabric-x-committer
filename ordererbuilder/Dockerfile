FROM golang:1.18.4-bullseye

## Check out fabric without building
ENV FABRIC_PATH=$GOPATH/src/github.com/hyperledger/fabric
RUN git clone https://github.com/hyperledger/fabric.git $FABRIC_PATH && \
    cd $FABRIC_PATH && \
    git checkout v2.4.7 -b v2.4.7-branch && \
    make native

RUN apt-get update && apt-get install -y tmux

ENV OUT_PATH=/usr/local/out
ENV BINS_PATH=$FABRIC_PATH/build/bin

WORKDIR /usr/local

# Patch to disable signatures
ADD ordererbuilder/orderer_no_sig_check.patch .
# Patch for boosting orderer performance
ADD ordererbuilder/orderer_booster.patch .
# Script to build binaries
ADD ordererbuilder/build_orderer_binaries.sh .
RUN chmod +x ./build_orderer_binaries.sh

CMD ["/bin/sh"]

