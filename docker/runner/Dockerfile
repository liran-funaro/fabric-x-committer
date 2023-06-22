FROM golang:1.20-bullseye as sc_runner

ENV CONFIGS_PATH=/root/config/
ENV BINS_PATH=/root/bin/
ENV PUBKEY_PATH=/root/pubkey/

ADD . scalable-commiter
RUN cd scalable-commiter && make build-local output_dir=$BINS_PATH
RUN rm -rf scalable-commiter

ADD scripts/run-services.sh /root/

EXPOSE 5050

RUN chmod a+x /root/run-services.sh

CMD ["/root/run-services.sh"]
