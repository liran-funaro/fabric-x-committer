ARG DB_IMAGE=yugabytedb/yugabyte:latest

FROM ${DB_IMAGE} AS node

ARG ARCHBIN_PATH
ARG TARGETOS
ARG TARGETARCH

ENV CONFIGS_PATH=/root/config
ENV BINS_PATH=/root/bin

COPY ${ARCHBIN_PATH}/${TARGETOS}-${TARGETARCH} $BINS_PATH
COPY ./docker/images/test_node/config $CONFIGS_PATH
COPY ./docker/images/test_node/scripts/run-services.sh /root/
RUN chmod a+x /root/run-services.sh

EXPOSE 5050

CMD ["/root/run-services.sh"]
