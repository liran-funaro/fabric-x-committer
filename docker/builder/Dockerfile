FROM golang:1.20-bullseye as sc_builder

# install just
RUN curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to /usr/local/bin

# Pre-download the go modules
ADD . /tmp/code/
RUN cd /tmp/code && go mod tidy
RUN rm -rf /tmp/code
