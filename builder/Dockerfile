
FROM golang:1.18-bullseye

RUN apt-get update && apt-get install -y librocksdb-dev

# TODO check librocksdb

# install just
RUN curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/bin
