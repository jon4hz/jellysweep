FROM gcr.io/distroless/static@sha256:15f8637509792e05b97d83451b0b6ccfa4abd548f997e406ac1e72c87fe1fa78

ARG TARGETPLATFORM

WORKDIR /app

VOLUME /app/data

# workaround to prevent slowness in docker when running with a tty
ENV CI="1"

EXPOSE 3002/tcp

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/jellysweep", "healthcheck"]

ENTRYPOINT [ "/usr/local/bin/jellysweep"]
CMD [ "serve" ]

COPY $TARGETPLATFORM/jellysweep /usr/local/bin/jellysweep
