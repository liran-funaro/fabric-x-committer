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
ADD orderingservice/fabric/orderer_no_sig_check.patch .
# Script to build binaries
ADD ordererbuilder/rebuild_binaries.sh .
RUN chmod +x ./rebuild_binaries.sh
ADD ordererbuilder/init.sh .
RUN chmod +x ./init.sh
ADD ordererbuilder/run_orderer.sh .
RUN chmod +x ./run_orderer.sh

EXPOSE 7050
EXPOSE 7051
EXPOSE 7052
EXPOSE 7053
EXPOSE 7054
EXPOSE 7055

CMD ["/bin/sh"]

