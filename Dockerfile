FROM alpine:latest

WORKDIR /

LABEL org.opencontainers.image.title="URL Shortener"
LABEL org.opencontainers.image.source="https://github.com/SpechtLabs/urlshortener"
LABEL org.opencontainers.image.description="TBD"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL org.opencontainers.image.authors="SpechtLabs <cedi@specht-labs.de>"
LABEL org.opencontainers.image.url="https://staticpages.specht-labs.de"
LABEL org.opencontainers.image.vendor="SpechtLabs"

COPY html/ html/
COPY ./urlshortener /bin/urlshortener

ENTRYPOINT ["/bin/staticpages"]
CMD [ "serve" ]

USER 65532:65532

EXPOSE 8123
#ENTRYPOINT ["/urlshortener --bind-address=:8123"]