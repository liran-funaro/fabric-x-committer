FROM sc_builder

ADD runner/config/sigservice-machine-config-sigservice.yaml \
runner/config/shardsservice-machine-config-shardsservice.yaml \
runner/config/coordinator-machine-config-coordinator.yaml \
/root/config/

ADD runner/bin/sigservice \
runner/bin/shardsservice \
runner/bin/coordinator \
/root/bin/

ADD runner/run-services.sh /root

EXPOSE 5002

RUN chmod a+x /root/run-services.sh

CMD ["/root/run-services.sh"]