FROM debian:jessie

RUN apt-get update && apt-get install --yes ca-certificates kmod

COPY /.build/artifacts/route-controller /usr/bin/route-controller

CMD /usr/bin/route-controller

