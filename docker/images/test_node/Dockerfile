ARG GO_IMAGE=golang:latest
ARG DB_IMAGE=yugabytedb/yugabyte:latest

FROM ${GO_IMAGE} AS sc_builder

WORKDIR /scalable-committer
COPY . .
RUN CGO_ENABLED=0 make build output_dir=/root/build

FROM ${DB_IMAGE} AS node

ENV CONFIGS_PATH=/root/config
ENV BINS_PATH=/root/bin
ENV PUBKEY_PATH=/root/pubkey

ENV METANS_SIG_SCHEME=ECDSA
ENV METANS_SIG_VERIFICATION_KEY_PATH=$PUBKEY_PATH/sc_pubkey.pem

COPY --from=sc_builder /root/build $BINS_PATH
COPY ./docker/images/test_node/config $CONFIGS_PATH
COPY ./docker/images/test_node/scripts/run-services.sh /root/
RUN chmod a+x /root/run-services.sh

EXPOSE 5050

CMD ["/root/run-services.sh"]
