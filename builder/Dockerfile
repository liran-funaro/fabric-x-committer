
FROM golang:1.18-bullseye

RUN apt-get update && apt-get install -y \
      libgflags-dev \
      libsnappy-dev \
      zlib1g-dev \
      libbz2-dev \
      libzstd-dev \
      liblz4-dev

RUN cd /tmp && git clone https://github.com/facebook/rocksdb.git
RUN cd /tmp/rocksdb && DEBUG_LEVEL=0 make shared_lib && make install

# install just
RUN curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/bin
