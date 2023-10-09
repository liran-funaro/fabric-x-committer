FROM golang:1.20-bullseye as sc_builder
ADD . scalable-commiter
RUN cd scalable-commiter && make build output_dir=/root/build/


FROM yugabytedb/yugabyte:2.19.0.0-b190

ENV CONFIGS_PATH=/root/config/
ENV BINS_PATH=/root/bin/
ENV PUBKEY_PATH=/root/pubkey/

COPY --from=sc_builder /root/build $BINS_PATH
ADD docker/runner/config/ $CONFIGS_PATH
ADD docker/runner/scripts/run-services.sh /root/
RUN chmod a+x /root/run-services.sh

EXPOSE 5050

CMD ["/root/run-services.sh"]
