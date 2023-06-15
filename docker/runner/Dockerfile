FROM sc_builder

ENV CONFIGS_PATH=/root/config/
ENV BINS_PATH=/root/bin/
ENV PUBKEY_PATH=/root/pubkey/

ADD docker/runner/bin/sigservice \
docker/runner/bin/shardsservice \
docker/runner/bin/coordinator \
docker/runner/bin/coordinator_setup \
docker/runner/bin/sidecar \
$BINS_PATH

ADD docker/runner/run-services.sh \
/root/

EXPOSE 5050

RUN chmod a+x /root/run-services.sh

CMD ["/root/run-services.sh"]
