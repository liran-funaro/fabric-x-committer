FROM sc_builder

ENV CONFIGS_PATH=/root/config/
ENV BINS_PATH=/root/bin/
ENV PUBKEY_PATH=/root/pubkey/

ADD docker/runner/out/bin/sigservice \
docker/runner/out/bin/shardsservice \
docker/runner/out/bin/coordinator \
docker/runner/out/bin/coordinator_setup \
docker/runner/out/bin/sidecar \
$BINS_PATH

ADD docker/runner/run-services.sh \
/root/

EXPOSE 5050

RUN chmod a+x /root/run-services.sh

CMD ["/root/run-services.sh"]