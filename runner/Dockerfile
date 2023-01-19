FROM sc_builder

ENV CONFIGS_PATH=/root/config/
ENV BINS_PATH=/root/bin/
ENV ORDERER_CONFIGS_PATH=/root/orderer-creds/

ADD runner/out/bin/sigservice \
runner/out/bin/shardsservice \
runner/out/bin/coordinator \
runner/out/bin/sidecar \
$BINS_PATH

ADD runner/run-services.sh \
/root/

EXPOSE 5002
EXPOSE 5050

RUN chmod a+x /root/run-services.sh

CMD ["/root/run-services.sh"]