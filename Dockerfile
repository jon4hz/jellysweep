FROM gcr.io/distroless/static

WORKDIR /app

VOLUME /app/data

# workaround to prevent slowness in docker when running with a tty
ENV CI="1"

EXPOSE 3002/tcp

ENTRYPOINT [ "/usr/local/bin/jellysweep", "serve" ]

COPY jellysweep /usr/local/bin/jellysweep
